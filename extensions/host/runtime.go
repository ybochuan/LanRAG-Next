package host

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/ingest"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
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
	var scopes []ingest.Scope
	if cfg.Mode == "worker" {
		client, err := weknora.NewClient(cfg.UpstreamURL, cfg.APIKey, cfg.TenantID)
		if err != nil {
			return errors.New("worker client initialization failed")
		}
		db, err = store.Open(runCtx, cfg.DBPath)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("extension database initialization failed")
		}
		apiStore := taggov.NewHTTPStore(client)
		writer, err = taggov.NewWriter(apiStore, 64)
		if err != nil {
			return errors.New("tag writer initialization failed")
		}
		source, err := ingest.NewHTTPSource(client, 50)
		if err != nil {
			return errors.New("reconciliation source initialization failed")
		}
		reconcile = &ingest.Reconciler{Source: source, Sink: ingest.NewObserver(db), Interval: cfg.ReconcileInterval, Report: func(error) { report("reconciliation_failed") }}
		projector := taggov.NewProjector(db, lifecycle.NewStore(db), taggov.NewMapping(db, apiStore), writer)
		allowed := make([]outbox.Scope, 0, len(cfg.KnowledgeBaseIDs))
		for _, kb := range cfg.KnowledgeBaseIDs {
			scopes = append(scopes, ingest.Scope{TenantID: cfg.TenantID, KnowledgeBaseID: kb})
			allowed = append(allowed, outbox.Scope{TenantID: cfg.TenantID, KnowledgeBaseID: kb})
		}
		worker = &outbox.Worker{Store: outbox.NewStore(db), Handler: projector.Handle, Lease: cfg.Lease, RetryDelay: cfg.RetryDelay, MaxAttempts: cfg.MaxAttempts, PollInterval: cfg.PollInterval, Scopes: allowed, Report: func(error) { report("projection_failed") }}
	}
	server := bridge.NewServerWithServices(bridge.Services{
		BeforeRetrieve: bridge.NewPassThroughRetrieveHandler(),
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(l) }()
	var tasks sync.WaitGroup
	backgroundResult := make(chan error, 2)
	launch := func(fn func(context.Context) error) {
		tasks.Add(1)
		go func() { defer tasks.Done(); backgroundResult <- fn(runCtx) }()
	}
	if reconcile != nil {
		launch(func(ctx context.Context) error { return reconcile.Run(ctx, scopes) })
		launch(worker.Run)
	}
	started = true
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
