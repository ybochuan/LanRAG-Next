package modelsupervisor

import (
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Resource states. unverified is a first-class answer: a machine without a
// visible NVIDIA device, or one where nvidia-smi is not on PATH, must still be
// able to start a model. Reporting an error there would block a CPU-only setup,
// and silently reporting "ok" would invent capacity that does not exist.
const (
	ResourceOK         = "ok"
	ResourceUnverified = "unverified"
	ResourceError      = "error"
)

// ReserveGB is headroom kept free so that the display stack and the core keep
// working. It is a fixed absolute value on purpose: a percentage of a 24 GB card
// is a different promise than a percentage of an 8 GB card.
const ReserveGB = 1.5

// GPU is one physical device as reported by nvidia-smi.
type GPU struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	TotalMB int    `json:"total_mb"`
	FreeMB  int    `json:"free_mb"`
}

// Resources is the observable capacity of the host.
type Resources struct {
	State     string  `json:"state"`
	GPUs      []GPU   `json:"gpus,omitempty"`
	FreeGB    float64 `json:"free_gb"`
	TotalGB   float64 `json:"total_gb"`
	ReserveGB float64 `json:"reserve_gb"`
	Note      string  `json:"note,omitempty"`
}

// Runner executes an external command and returns its standard output.
type Runner func(name string, args ...string) ([]byte, error)

// DefaultRunner shells out through PATH.
func DefaultRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// QueryResources samples GPU capacity. A missing or failing nvidia-smi yields
// ResourceUnverified with the reason attached, never a silent success.
func QueryResources(run Runner) Resources {
	if run == nil {
		run = DefaultRunner
	}
	out, err := run("nvidia-smi", "--query-gpu=index,name,memory.total,memory.free", "--format=csv,noheader,nounits")
	if err != nil {
		return Resources{State: ResourceUnverified, ReserveGB: ReserveGB, Note: "nvidia-smi unavailable: " + err.Error()}
	}
	gpus, err := ParseNvidiaSMI(out)
	if err != nil {
		return Resources{State: ResourceUnverified, ReserveGB: ReserveGB, Note: err.Error()}
	}
	if len(gpus) == 0 {
		return Resources{State: ResourceUnverified, ReserveGB: ReserveGB, Note: "nvidia-smi reported no devices"}
	}
	resources := Resources{State: ResourceOK, GPUs: gpus, ReserveGB: ReserveGB}
	for _, gpu := range gpus {
		resources.FreeGB += float64(gpu.FreeMB) / 1024
		resources.TotalGB += float64(gpu.TotalMB) / 1024
	}
	resources.FreeGB = round2(resources.FreeGB)
	resources.TotalGB = round2(resources.TotalGB)
	return resources
}

// ParseNvidiaSMI reads the CSV emitted by the query above. It is tolerant of the
// trailing blank line but strict about column count.
func ParseNvidiaSMI(out []byte) ([]GPU, error) {
	var gpus []GPU
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected nvidia-smi row %q", line)
		}
		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid nvidia-smi index in %q", line)
		}
		total, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil {
			return nil, fmt.Errorf("invalid nvidia-smi total memory in %q", line)
		}
		free, err := strconv.Atoi(strings.TrimSpace(fields[3]))
		if err != nil {
			return nil, fmt.Errorf("invalid nvidia-smi free memory in %q", line)
		}
		gpus = append(gpus, GPU{Index: index, Name: strings.TrimSpace(fields[1]), TotalMB: total, FreeMB: free})
	}
	return gpus, nil
}

// EstimateGB projects the resident footprint of a model plus its KV cache. The
// KV term is a deliberate approximation used only to refuse an obviously
// impossible start; it is not a memory guarantee.
func EstimateGB(modelBytes int64, ctxSize int) float64 {
	modelGB := float64(modelBytes) / (1 << 30)
	kvGB := float64(ctxSize) / 1024 * 0.15
	return round2(modelGB + kvGB)
}

// CheckCapacity decides whether a start may proceed. When the host capacity is
// unverified the start is allowed with a reason, because refusing would break
// CPU-only and non-NVIDIA installations; the caller must surface the reason.
func (r Resources) CheckCapacity(estimateGB float64) (bool, string) {
	if r.State != ResourceOK {
		return true, "resource capacity unverified"
	}
	budget := round2(r.FreeGB - ReserveGB)
	if estimateGB > budget {
		return false, fmt.Sprintf("estimated %.2f GB exceeds the %.2f GB budget (free %.2f GB minus %.2f GB reserve)", estimateGB, budget, r.FreeGB, ReserveGB)
	}
	return true, ""
}

// ErrCapacityExceeded wraps a refused precheck so callers can map it to 428.
var ErrCapacityExceeded = errors.New("estimated memory exceeds the available budget")

func round2(v float64) float64 { return math.Round(v*100) / 100 }
