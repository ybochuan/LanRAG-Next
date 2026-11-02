package host

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// TestNativeE2EHostProcess is opt-in and runs the real production Serve runtime
// on an owned loopback port. It never changes the production listener setting.
func TestNativeE2EHostProcess(t *testing.T) {
	ready := os.Getenv("AGENTRAG_M1_E2E_HOST_READY")
	if ready == "" {
		t.Skip("owned native E2E process only")
	}
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:"+os.Getenv("AGENTRAG_M1_E2E_HOST_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	b, _ := json.Marshal(map[string]int{"port": l.Addr().(*net.TCPAddr).Port})
	if err := os.WriteFile(ready, b, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := Serve(ctx, cfg, l, func(code string) { log.Print(code) }); err != nil {
		t.Fatal(err)
	}
}
