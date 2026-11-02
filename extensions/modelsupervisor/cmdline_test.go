package modelsupervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgvPinsSupervisorOwnedFlags(t *testing.T) {
	spec := validSpec()
	spec.ExtraArgs = []string{"--flash-attn", "--batch-size", "512"}
	argv, err := spec.Argv(`C:\data\bin\llama-server.exe`, `C:\data\models\m.gguf`, "secret-key")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--host 127.0.0.1",
		"--port 8081",
		"--alias embedding",
		"--embedding",
		"--no-webui",
		"-ngl 99",
		"-c 4096",
		"-t 8",
		"--flash-attn",
		"--batch-size 512",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv missing %q: %v", want, argv)
		}
	}
	if strings.Count(joined, "--host") != 1 || strings.Count(joined, "--port") != 1 {
		t.Fatalf("supervisor flag duplicated: %v", argv)
	}
}

func TestArgvUsesTaskFlagPerType(t *testing.T) {
	rerank := Spec{ID: "rerank", Model: "r.gguf", TaskType: TaskRerank, Port: 8082, CtxSize: 4096, Threads: 8}
	argv, err := rerank.Argv("bin", "model", "")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--rerank") || strings.Contains(joined, "--embedding") {
		t.Fatalf("rerank task flag wrong: %v", argv)
	}
	if strings.Contains(joined, "--api-key") {
		t.Fatalf("empty key must not emit --api-key: %v", argv)
	}
}

// Chat is llama-server's default mode, so it must carry no mode flag at all — a
// bogus flag would make the process exit before it ever binds the port — and it
// must ask for the GGUF's own chat template.
func TestArgvChatOmitsModeFlagAndEnablesJinja(t *testing.T) {
	chat := Spec{ID: "chat", Model: "c.gguf", TaskType: TaskChat, Port: 8083, CtxSize: 4096, Threads: 8}
	argv, err := chat.Argv("bin", "model", "")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	joined := strings.Join(argv, " ")
	for _, unwanted := range []string{"--embedding", "--embeddings", "--rerank", "--reranking"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("chat must not carry a retrieval mode flag (%s): %v", unwanted, argv)
		}
	}
	if !strings.Contains(joined, "--jinja") {
		t.Fatalf("chat must render the GGUF chat template: %v", argv)
	}
	// The empty flag must not leave a blank argv element behind.
	for _, arg := range argv {
		if arg == "" {
			t.Fatalf("argv contains an empty element: %q", argv)
		}
	}
}

func TestArgvDisablesGPUWhenNotRequested(t *testing.T) {
	spec := validSpec()
	no := false
	spec.UseGPU = &no
	argv, err := spec.Argv("bin", "model", "")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-ngl 0") {
		t.Fatalf("cpu-only spec must pin -ngl 0: %v", argv)
	}
}

func TestArgvUsesExplicitGPULayers(t *testing.T) {
	spec := validSpec()
	partial := 20
	spec.GPULayers = &partial
	argv, err := spec.Argv("bin", "model", "")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-ngl 20") {
		t.Fatalf("explicit gpu_layers must reach the command line: %v", argv)
	}
	// The value must be a single token pair, not a flag with a stray neighbour.
	for i, arg := range argv {
		if arg == "-ngl" && (i+1 >= len(argv) || argv[i+1] != "20") {
			t.Fatalf("-ngl must be immediately followed by its count: %v", argv)
		}
	}
}

func TestArgvRejectsCallerSuppliedGPULayers(t *testing.T) {
	for _, extra := range [][]string{
		{"-ngl", "0"},
		{"--gpu-layers", "0"},
		{"--n-gpu-layers", "0"},
		{"--n-gpu-layers=0"},
	} {
		spec := validSpec()
		spec.ExtraArgs = extra
		if _, err := spec.Argv("bin", "model", ""); !errors.Is(err, ErrReservedArg) {
			t.Fatalf("extra_args %v => %v, want %v", extra, err, ErrReservedArg)
		}
	}
}

// A6: a caller must not be able to rebind the listener or swap the model.
func TestArgvRejectsReservedExtraArgs(t *testing.T) {
	for _, extra := range [][]string{
		{"--host", "0.0.0.0"},
		{"--host=0.0.0.0"},
		{"--port", "9999"},
		{"--model", `..\..\windows\system32\cmd.exe`},
		{"--alias", "other"},
		{"--api-key", "attacker"},
		{"--api-key-file", "secrets.txt"},
		{"--rerank"},
		{"--embeddings"},
		{"--no-ui"},
		{"--HOST", "0.0.0.0"},
		// ctx_size and threads are structured fields, so restating them through
		// extra_args made the command line disagree with what the console showed.
		{"-c", "8192"},
		{"--ctx-size", "8192"},
		{"-c=8192"},
		{"-t", "1"},
		{"--threads", "1"},
		{"-ngl", "4"},
	} {
		spec := validSpec()
		spec.ExtraArgs = extra
		if _, err := spec.Argv("bin", "model", "key"); !errors.Is(err, ErrReservedArg) {
			t.Fatalf("%v: got %v, want ErrReservedArg", extra, err)
		}
	}
}

func TestValidateExtraArgsRejectsMalformedEntries(t *testing.T) {
	for _, extra := range [][]string{
		{""},
		{"512"},
		{"--batch-size=512", "extra-positional"},
		{"--threads\n--host"},
		{"--flash-attn\x00"},
	} {
		if err := validateExtraArgs(extra); !errors.Is(err, ErrInvalidArg) {
			t.Fatalf("%q: got %v, want ErrInvalidArg", extra, err)
		}
	}
	if err := validateExtraArgs([]string{"--flash-attn", "--batch-size", "512", "--cache-type-k=q8_0"}); err != nil {
		t.Fatalf("harmless flags rejected: %v", err)
	}
}

// A5: a caller-supplied path must not escape its configured root.
func TestResolveUnderRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		`..\..\windows\system32\cmd.exe`,
		"../../windows/system32/cmd.exe",
		`C:\windows\system32\cmd.exe`,
		"sub/model.gguf",
		`sub\model.gguf`,
		"..",
		".",
		"",
	} {
		if _, err := ResolveUnder(root, name); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("%q: got %v, want ErrPathEscape", name, err)
		}
	}
	if _, err := ResolveUnder("", "model.gguf"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("empty root must be rejected, got %v", err)
	}
}

func TestResolveUnderAcceptsBareNameInsideRoot(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "Qwen3-Embedding-4B-Q6_K.gguf")
	if err := os.WriteFile(model, []byte("gguf"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	got, err := ResolveUnder(root, "Qwen3-Embedding-4B-Q6_K.gguf")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, "Qwen3-Embedding-4B-Q6_K.gguf") {
		t.Fatalf("resolved=%q", got)
	}
	missing, err := ResolveUnder(root, "absent.gguf")
	if err != nil {
		t.Fatalf("absent file should resolve lexically: %v", err)
	}
	if !strings.HasSuffix(missing, "absent.gguf") {
		t.Fatalf("missing=%q", missing)
	}
}

func TestRedactedHidesAPIKey(t *testing.T) {
	spec := validSpec()
	argv, err := spec.Argv("bin", "model", "super-secret")
	if err != nil {
		t.Fatalf("argv: %v", err)
	}
	safe := strings.Join(Redacted(argv), " ")
	if strings.Contains(safe, "super-secret") || !strings.Contains(safe, "***") {
		t.Fatalf("redaction failed: %s", safe)
	}
}
