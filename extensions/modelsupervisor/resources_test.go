package modelsupervisor

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNvidiaSMIReadsAllDevices(t *testing.T) {
	out := []byte("0, NVIDIA GeForce RTX 4090, 24564, 23000\n1, NVIDIA GeForce RTX 3060, 12288, 9000\n\n")
	gpus, err := ParseNvidiaSMI(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("devices = %d", len(gpus))
	}
	if gpus[0].Index != 0 || gpus[0].TotalMB != 24564 || gpus[0].FreeMB != 23000 {
		t.Fatalf("first device = %+v", gpus[0])
	}
	if gpus[1].Name != "NVIDIA GeForce RTX 3060" {
		t.Fatalf("second name = %q", gpus[1].Name)
	}
}

func TestParseNvidiaSMIRejectsMalformedRows(t *testing.T) {
	if _, err := ParseNvidiaSMI([]byte("0, GPU\n")); err == nil {
		t.Fatal("expected an error for a two-column row")
	}
	if _, err := ParseNvidiaSMI([]byte("x, GPU, 100, 50\n")); err == nil {
		t.Fatal("expected an error for a non-numeric index")
	}
}

func TestQueryResourcesDegradesToUnverified(t *testing.T) {
	cases := []struct {
		name string
		run  Runner
	}{
		{"nvidia-smi missing", func(string, ...string) ([]byte, error) { return nil, errors.New("exec: not found") }},
		{"no devices", func(string, ...string) ([]byte, error) { return []byte("\n"), nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resources := QueryResources(tc.run)
			if resources.State != ResourceUnverified {
				t.Fatalf("state = %q", resources.State)
			}
			if resources.Note == "" {
				t.Fatal("an unverified result must explain itself")
			}
			// Capacity is unknown, so a start must be allowed rather than blocked.
			ok, reason := resources.CheckCapacity(999)
			if !ok {
				t.Fatalf("unverified capacity must not block a start, reason = %q", reason)
			}
		})
	}
}

func TestQueryResourcesSumsCapacity(t *testing.T) {
	run := func(string, ...string) ([]byte, error) {
		return []byte("0, RTX 4090, 24576, 20480\n"), nil
	}
	resources := QueryResources(run)
	if resources.State != ResourceOK {
		t.Fatalf("state = %q", resources.State)
	}
	if resources.FreeGB != 20 || resources.TotalGB != 24 {
		t.Fatalf("free/total = %.2f/%.2f", resources.FreeGB, resources.TotalGB)
	}
	if resources.ReserveGB != ReserveGB {
		t.Fatalf("reserve = %.2f", resources.ReserveGB)
	}
}

func TestEstimateGBAddsModelAndContext(t *testing.T) {
	// 4 GiB model, 4096 context: 4.00 + 0.60.
	got := EstimateGB(4<<30, 4096)
	if got != 4.6 {
		t.Fatalf("estimate = %.2f", got)
	}
	if EstimateGB(0, 0) != 0 {
		t.Fatalf("empty estimate = %.2f", EstimateGB(0, 0))
	}
}

func TestCheckCapacityRefusesOversizedRequests(t *testing.T) {
	resources := Resources{State: ResourceOK, FreeGB: 20, TotalGB: 24, ReserveGB: ReserveGB}
	ok, _ := resources.CheckCapacity(18)
	if !ok {
		t.Fatal("18 GB fits in a 20 GB budget minus the reserve")
	}
	ok, reason := resources.CheckCapacity(19)
	if ok {
		t.Fatal("19 GB exceeds the 18.50 GB budget")
	}
	if !strings.Contains(reason, "18.50") {
		t.Fatalf("reason = %q, want it to name the budget", reason)
	}
}

func TestCheckCapacityAllowsUnknownState(t *testing.T) {
	resources := Resources{State: ResourceError, ReserveGB: ReserveGB}
	ok, reason := resources.CheckCapacity(100)
	if !ok {
		t.Fatal("an errored capacity reading must not block")
	}
	if reason != "resource capacity unverified" {
		t.Fatalf("reason = %q", reason)
	}
}
