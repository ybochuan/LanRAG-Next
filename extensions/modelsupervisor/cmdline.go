package modelsupervisor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// BinaryName is the llama-server executable the supervisor manages. The binary
// is fixed rather than caller-supplied: a caller-supplied executable path turns
// the start endpoint into arbitrary command execution.
const BinaryName = "llama-server.exe"

// managedFlags are the flags whose value the supervisor owns, either because
// Argv writes them or because a structured field already decides them. A caller
// may not restate one through extra_args: doing so would let it rebind the
// listener (--host 0.0.0.0), swap the model, override the task mode, or give
// -c/-t a different value from the one the console displays while the
// supervisor still reports success.
//
// The rule is "everything we manage is protected", not "a few we remembered":
// an omit-by-oversight list is exactly how -c and -t used to slip through.
var managedFlags = map[string]bool{
	"-c": true, "--ctx-size": true,
	"-t": true, "--threads": true,
	"--host": true, "--port": true, "--model": true, "--alias": true,
	"--api-key": true, "--api-key-file": true,
	"--embedding": true, "--embeddings": true,
	"--rerank": true, "--reranking": true,
	"--jinja":    true,
	"--no-webui": true, "--no-ui": true,
	// Layer offload is resolved from use_gpu/gpu_layers and written by Argv.
	"-ngl": true, "--gpu-layers": true, "--n-gpu-layers": true,
}

// ResolveUnder joins a bare file name onto a configured root and refuses
// anything that escapes it. Names carrying separators, drive letters or dot
// segments are rejected outright; existing files are re-checked after symlink
// resolution so a link cannot point outside the root either.
func ResolveUnder(root, name string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(name) == "" {
		return "", ErrPathEscape
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return "", ErrPathEscape
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", ErrPathEscape
	}
	candidate := filepath.Join(absRoot, name)
	if !within(absRoot, candidate) {
		return "", ErrPathEscape
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// Not present yet: the lexical containment check above still holds and
		// existence is reported separately with a clearer message.
		return candidate, nil
	}
	if !within(realRoot, realCandidate) {
		return "", ErrPathEscape
	}
	return realCandidate, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Argv builds the exact command line for one instance. The returned slice never
// contains a caller-controlled --host, --port, --model, --alias or --api-key.
func (s Spec) Argv(binary, modelPath, apiKey string) ([]string, error) {
	if !s.TaskType.Valid() {
		return nil, ErrUnknownTask
	}
	if err := validateExtraArgs(s.ExtraArgs); err != nil {
		return nil, err
	}
	argv := []string{
		binary,
		"--model", modelPath,
	}
	if flag := s.TaskType.flag(); flag != "" {
		argv = append(argv, flag)
	}
	if s.TaskType == TaskChat {
		// --jinja makes llama-server render the chat template carried by the
		// GGUF. Without it a chat model is served through llama.cpp's built-in
		// default template, which silently mangles role and tool formatting.
		argv = append(argv, "--jinja")
	}
	argv = append(argv,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(s.Port),
		"--alias", s.ID,
		"--no-webui",
	)
	if apiKey != "" {
		argv = append(argv, "--api-key", apiKey)
	}
	argv = append(argv, "-ngl", strconv.Itoa(s.GPULayerCount()))
	if s.CtxSize > 0 {
		argv = append(argv, "-c", strconv.Itoa(s.CtxSize))
	}
	if s.Threads > 0 {
		argv = append(argv, "-t", strconv.Itoa(s.Threads))
	}
	return append(argv, s.ExtraArgs...), nil
}

// validateExtraArgs enforces two things: no token may restate a managed flag,
// and the shape must be well formed — a token that is neither a flag nor the
// value of the immediately preceding flag is rejected rather than passed
// through as a stray positional argument.
func validateExtraArgs(args []string) error {
	expectingValue := false
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, "\x00\n\r") {
			return ErrInvalidArg
		}
		if strings.HasPrefix(arg, "-") {
			name := arg
			if i := strings.IndexByte(arg, '='); i >= 0 {
				name = arg[:i]
			}
			if managedFlags[strings.ToLower(name)] {
				return ErrReservedArg
			}
			expectingValue = !strings.Contains(arg, "=")
			continue
		}
		if !expectingValue {
			return ErrInvalidArg
		}
		expectingValue = false
	}
	return nil
}

// Redacted renders the command line for diagnostics. The API key is replaced so
// that an error path cannot leak it into a log or an HTTP response.
func Redacted(argv []string) []string {
	out := make([]string, len(argv))
	copy(out, argv)
	for i := 0; i+1 < len(out); i++ {
		if strings.EqualFold(out[i], "--api-key") {
			out[i+1] = "***"
		}
	}
	return out
}
