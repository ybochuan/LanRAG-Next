package host

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/ingest"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/management"
	extmcp "github.com/ybochuan/LanRAG-Next/extensions/mcp"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/parser"
	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/taggov"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Run is the production entry: bind the fixed IPv4 loopback before opening a DB
// or starting work. A second instance cannot start another projection writer.
func Run(ctx context.Context, cfg Config, report func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	l, err := net.Listen("tcp4", bridge.Address)
	if err != nil {
		return errors.New("extension host loopback listener unavailable")
	}
	return Serve(ctx, cfg, l, report)
}

// Serve owns the listener and all component lifetimes. A custom ephemeral port
// supports isolated tests; only an IPv4 127.0.0.1 listener is accepted. Reporter
// receives fixed non-sensitive event codes, may run concurrently, and must return
// promptly. ShutdownTimeout bounds HTTP drain; tasks honor cancellation and are
// joined before closing their Writer/DB, never forcibly abandoning a transaction.
func Serve(ctx context.Context, cfg Config, l net.Listener, report func(string)) (result error) {
	if l == nil {
		return errors.New("host listener required")
	}
	defer l.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) || addr.IP.To4() == nil {
		return errors.New("host requires IPv4 127.0.0.1 listener")
	}
	if report == nil {
		return errors.New("host event reporter required")
	}
	cfg.KnowledgeBaseIDs = append([]string(nil), cfg.KnowledgeBaseIDs...)
	var db *store.DB
	var writer *taggov.Writer
	started := false
	defer func() {
		if writer != nil {
			writer.Close()
		}
		if db != nil {
			if err := db.Close(); err != nil {
				result = errors.Join(result, errors.New("extension database close failed"))
			}
		}
		if started {
			report("host_stopped")
		}
	}()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var reconcile *ingest.Reconciler
	var worker *outbox.Worker
	var machine *lifecycle.Machine
	var reviewService *review.Service
	var sourceRegistry *lineage.Registry
	var sourceRuns *provenance.Runs
	var sourceLedger *provenance.Ledger
	var businessService *business.Service
	var upstream *weknora.Client
	var scopes []ingest.Scope
	if cfg.Mode == "worker" {
		client, err := weknora.NewClient(cfg.UpstreamURL, cfg.APIKey, cfg.TenantID)
		if err != nil {
			return errors.New("worker client initialization failed")
		}
		upstream = client
		db, err = store.OpenDurable(runCtx, cfg.DBPath)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return databaseInitializationError(err)
		}
		reader, err := lineage.NewHTTPReader(client, cfg.KnowledgeBaseIDs)
		if err != nil {
			return errors.New("source reader initialization failed")
		}
		sourceRegistry = lineage.NewRegistry(db, reader)
		businessService = business.New(db, sourceRegistry, time.Now).WithRemote(business.NewNativePublisher(client))
		// Production proofs use real native components. A nil sealed provider is
		// used only for source freshness; run facts provide the copy authority.
		sourceAuthority := lineage.NewAuthority(sourceRegistry, nil)
		sourceRuns = provenance.NewRuns(db, lineage.NewComponentVerifier(reader, sourceRegistry), sourceAuthority, []byte(cfg.ManagementKey))
		sourceLedger = provenance.New(db, lineage.NewAuthority(sourceRegistry, sourceRuns))
		apiStore := taggov.NewHTTPStore(client)
		writer, err = taggov.NewWriter(apiStore, 64)
		if err != nil {
			return errors.New("tag writer initialization failed")
		}
		source, err := ingest.NewHTTPSource(client, 50)
		if err != nil {
			return errors.New("reconciliation source initialization failed")
		}
		machine = lifecycle.NewMachine(db, time.Now)
		reconcile = &ingest.Reconciler{Source: source, Sink: ingest.NewLifecycleObserver(db, time.Now), Interval: cfg.ReconcileInterval, Report: func(error) { report("reconciliation_failed") }}
		projector := taggov.NewProjector(db, lifecycle.NewStore(db), taggov.NewMapping(db, apiStore), writer)
		reviewService = review.NewService(db, time.Now)
		alertHandler := reviewService.Handler(func(_ context.Context, _ review.Alert) error { report(review.SignalCode); return nil })
		allowed := make([]outbox.Scope, 0, len(cfg.KnowledgeBaseIDs))
		for _, kb := range cfg.KnowledgeBaseIDs {
			scopes = append(scopes, ingest.Scope{TenantID: cfg.TenantID, KnowledgeBaseID: kb})
			allowed = append(allowed, outbox.Scope{TenantID: cfg.TenantID, KnowledgeBaseID: kb})
		}
		worker = &outbox.Worker{Store: outbox.NewStore(db), Handler: projectionDispatcher(projector.Handle, alertHandler), Lease: cfg.Lease, RetryDelay: cfg.RetryDelay, MaxAttempts: cfg.MaxAttempts, PollInterval: cfg.PollInterval, Scopes: allowed, Report: func(error) { report("projection_failed") }}
	}
	var native *parser.NativeHandler
	var err error
	if cfg.Parser != nil {
		parserCfg := *cfg.Parser
		parserCfg.Report = report
		if cfg.Mode == "worker" {
			client, e := weknora.NewClient(cfg.UpstreamURL, cfg.APIKey, cfg.TenantID)
			if e != nil {
				return e
			}
			parserCfg.Attachments = attachment.NewImporter(client, cfg.KnowledgeBaseIDs)
		}
		if db == nil {
			db, err = store.OpenDurable(runCtx, filepath.Join(parserCfg.ArtifactRoot, ".parser-state.sqlite"))
			if err != nil {
				return errors.New("parser database initialization failed")
			}
		}
		native, err = parser.NewPersistentNativeHandler(parserCfg, db)
		if err != nil {
			return errors.New("native parser initialization failed")
		}
		defer native.Close()
	}
	services := bridge.Services{}
	var localReady atomic.Bool
	defer localReady.Store(false)
	services.Ready = func(ctx context.Context) bool {
		if !localReady.Load() || db == nil {
			return false
		}
		return db.View(ctx, func(tx *sql.Tx) error { var one int; return tx.QueryRowContext(ctx, "SELECT 1").Scan(&one) }) == nil
	}
	if cfg.Mode == "worker" {
		policy, err := retrieval.NewPolicy(db, cfg.TenantID, cfg.KnowledgeBaseIDs)
		if err != nil {
			return errors.New("retrieval policy initialization failed")
		}
		policy.WithSources(sourceRegistry)
		attachmentSource, err := weknora.NewClient(cfg.UpstreamURL, cfg.APIKey, cfg.TenantID)
		if err != nil {
			return err
		}
		policy.WithAttachments(attachmentSource)
		services.MCP = &extmcp.Server{DB: db, Client: attachmentSource, Policy: policy, Key: cfg.ManagementKey, Tenant: cfg.TenantID, KBs: cfg.KnowledgeBaseIDs, NativeDBPath: cfg.NativeDBPath}
		services.AfterRetrieve = &bridge.AttachmentHandler{Key: cfg.ManagementKey, Policy: policy}
		services.BeforeRetrieve, err = bridge.NewRetrieveHandler(cfg.TenantID, cfg.KnowledgeBaseIDs, policy)
		if err != nil {
			return errors.New("retrieval handler initialization failed")
		}
	}
	if native != nil {
		services.Parser = native
		services.ParserReadTimeout = cfg.Parser.BulkTimeout
	}
	lifecycleScopes := make([]lifecycle.PolicyScope, 0, len(scopes))
	for _, scope := range scopes {
		lifecycleScopes = append(lifecycleScopes, lifecycle.PolicyScope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID})
	}
	if machine != nil {
		if err := machine.Sweep(runCtx, lifecycleScopes); err != nil {
			return errors.New("initial lifecycle scan failed; Host not ready")
		}
	}
	server := bridge.NewServerWithServices(services)
	{
		original := server.Handler
		manager := management.New(db, cfg.ManagementKey, lifecycleScopes).WithSources(sourceRegistry).WithBusiness(businessService)
		if upstream != nil {
			manager.WithNativeSQLite(cfg.NativeDBPath, upstream.DocumentTitles)
		}
		proofScopes := make([]provenance.Scope, 0, len(scopes))
		for _, scope := range scopes {
			proofScopes = append(proofScopes, provenance.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID})
		}
		proofs := provenance.NewHTTPServer(sourceRuns, sourceLedger, cfg.ManagementKey, proofScopes)
		server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/provenance/") || r.URL.Path == "/bridge/before-generate" {
				proofs.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/management/lifecycle" {
				manager.ServeHTTP(w, r)
				return
			}
			original.ServeHTTP(w, r)
		})
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(l) }()
	var tasks sync.WaitGroup
	backgroundResult := make(chan error, 4)
	launch := func(fn func(context.Context) error) {
		tasks.Add(1)
		go func() { defer tasks.Done(); backgroundResult <- fn(runCtx) }()
	}
	if reconcile != nil {
		launch(func(ctx context.Context) error { return reconcile.Run(ctx, scopes) })
		launch(worker.Run)
		launch(func(ctx context.Context) error {
			return runDependencyDaily(ctx, db, sourceRegistry, machine, lifecycleScopes, report)
		})
		launch(func(ctx context.Context) error {
			return reviewService.RunDaily(ctx, lifecycleScopes, func(error) { report("review_scan_failed") })
		})
	}
	started = true
	localReady.Store(cfg.Mode == "worker")
	if cfg.Mode == "worker" {
		report("host_workers_started")
	} else {
		report("host_probe_started")
	}
	serverReturned := false
	select {
	case <-ctx.Done():
	case <-serveResult:
		serverReturned = true
		if ctx.Err() == nil {
			result = errors.New("extension HTTP server stopped unexpectedly")
		}
	case <-backgroundResult:
		if ctx.Err() == nil {
			result = errors.New("extension background service stopped unexpectedly")
		}
	}
	localReady.Store(false)
	cancel()
	drainCtx, stopDrain := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer stopDrain()
	if err := server.Shutdown(drainCtx); err != nil {
		_ = server.Close()
		result = errors.Join(result, errors.New("extension HTTP shutdown deadline exceeded"))
	}
	if !serverReturned {
		<-serveResult
	}
	tasks.Wait()
	return result
}
