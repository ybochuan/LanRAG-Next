// Command extension-host runs the independent local AgentRAG Extension Host.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ybochuan/LanRAG-Next/extensions/host"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, func(code string) { log.Print(code) }); err != nil && !errors.Is(err, context.Canceled) {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, report func(string)) error {
	cfg, err := host.LoadConfig(getenv)
	if err != nil {
		return err
	}
	return host.Run(ctx, cfg, report)
}
