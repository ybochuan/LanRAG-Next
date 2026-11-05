package modelsupervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRuntime(t *testing.T, magic string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), BinaryName)
	if err := os.WriteFile(path, []byte(magic), 0o755); err != nil {
		t.Fatalf("write runtime fixture: %v", err)
	}
	return path
}

func TestCheckRuntimePlatformRejectsWindowsRuntimeOnLinux(t *testing.T) {
	path := writeRuntime(t, "MZ\x90\x00not really a PE")
	err := checkRuntimePlatform(path, "linux")
	if err == nil {
		t.Fatal("a Windows runtime must be refused by a Linux supervisor")
	}
	// The message has to name the real problem: a path Windows cannot open, not a
	// missing model.
	if !strings.Contains(err.Error(), "start the supervisor on Windows") {
		t.Fatalf("error does not explain the fix: %v", err)
	}
}

func TestCheckRuntimePlatformRejectsLinuxRuntimeOnWindows(t *testing.T) {
	path := writeRuntime(t, "\x7fELFnot really an ELF")
	if err := checkRuntimePlatform(path, "windows"); err == nil {
		t.Fatal("a Linux runtime must be refused by a Windows supervisor")
	}
}

func TestCheckRuntimePlatformAcceptsMatchingRuntime(t *testing.T) {
	cases := []struct {
		name  string
		goos  string
		magic string
	}{
		{"pe on windows", "windows", "MZ\x90\x00not really a PE"},
		{"elf on linux", "linux", "\x7fELFnot really an ELF"},
		{"text is left to exec", "linux", "#!/bin/sh\necho hi\n"},
		{"empty is left to exec", "windows", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkRuntimePlatform(writeRuntime(t, tc.magic), tc.goos); err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}

func TestCheckRuntimePlatformLeavesAMissingRuntimeToExec(t *testing.T) {
	// Launch() must not turn "no binary installed yet" into a header error: exec
	// says exactly which path is missing.
	err := checkRuntimePlatform(filepath.Join(t.TempDir(), "absent"), "linux")
	if err != nil {
		t.Fatalf("a missing runtime must not be classified here: %v", err)
	}
}
