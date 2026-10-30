package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommandRejectsBadConfigAndHonorsPreCanceledContext(t *testing.T) {
	getenv := func(key string) string {
		if key == "AGENTRAG_MODE" {
			return "worker"
		}
		if key == "AGENTRAG_WEKNORA_API_KEY" {
			return "test-secret"
		}
		return ""
	}
	if err := run(context.Background(), getenv, func(string) {}); err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("command validation=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, func(string) string { return "" }, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled command=%v", err)
	}
}
