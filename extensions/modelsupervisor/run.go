package modelsupervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/modelsupervisor/winjob"
)

// sweep re-probes running instances on a fixed interval until ctx is cancelled.
// It is deliberately read-only: a health observation never changes lifecycle
// status, because one slow or failed probe must not tear down an instance that
// is merely busy, and because the state machine follows the process, not a
// timer. What it does buy is that /list stops going stale — the health it
// reports was measured at a known time.
func sweep(ctx context.Context, manager *Manager, prober *Prober, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A sweep must never outlive its own tick by much, but it must also
			// outlive the slowest task probe.
			sweepCtx, cancel := context.WithTimeout(ctx, sweepBudget)
			manager.Observe(sweepCtx, prober)
			cancel()
		}
	}
}

// Run serves the supervisor until ctx is cancelled. It owns the listener, the
// process manager and the job object, and it tears all three down in order.
func Run(ctx context.Context, cfg Config, report func(string)) error {
	if report == nil {
		report = func(string) {}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	job, err := winjob.New()
	if err != nil {
		if !errors.Is(err, winjob.ErrUnsupported) {
			return fmt.Errorf("create job object: %w", err)
		}
		// Without a job object a crash of this process leaves children behind.
		// That is a degraded mode, not a reason to refuse to run on a machine
		// where the models are still useful.
		report("model supervisor: job objects unavailable on this platform, orphan cleanup is degraded")
		job = nil
	}
	defer func() {
		if job != nil {
			_ = job.Close()
		}
	}()

	manager, err := NewManager(Options{
		Store:       NewStore(cfg.StatePath),
		BinaryDir:   cfg.BinaryDir,
		ModelDir:    cfg.ModelDir,
		APIKey:      cfg.APIKey,
		Job:         job,
		StopTimeout: cfg.StopTimeout,
		LogLines:    cfg.LogLines,
	})
	if err != nil {
		return err
	}
	if err := manager.Load(); err != nil {
		return err
	}
	defer func() { _ = manager.Close() }()

	prober := &Prober{WSLDistro: cfg.WSLDistro}
	server, err := NewServer(manager, prober, cfg.Token)
	if err != nil {
		return err
	}
	server.AllowedOrigins = cfg.AllowedOrigins
	server.Catalog = func() (Catalog, error) { return ScanCatalog(cfg.ModelDir, cfg.BinaryDir) }

	if cfg.SweepInterval > 0 {
		go sweep(ctx, manager, prober, cfg.SweepInterval)
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	report(fmt.Sprintf("model supervisor listening on http://%s", cfg.Addr))
	serveErr := httpServer.Serve(listener)
	<-shutdownDone
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
