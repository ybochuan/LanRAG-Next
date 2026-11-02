// Command model-supervisor runs the local llama-server instances that the core
// reaches over loopback. It is deliberately separate from extension-host: a
// crashed model must not be able to affect the governance layer's fail-closed
// decision.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ybochuan/LanRAG-Next/extensions/modelsupervisor"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, func(message string) { log.Print(message) }); err != nil && !errors.Is(err, context.Canceled) {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, report func(string)) error {
	cfg, err := modelsupervisor.LoadConfig(getenv)
	if err != nil {
		return err
	}
	report(cfg.String())
	return modelsupervisor.Run(ctx, cfg, report)
}
