package modelsupervisor

import (
	"fmt"
	"io"
	"os"
)

// checkRuntimePlatform refuses a runtime binary built for a different operating
// system than the supervisor itself. The failure it prevents is obscure rather
// than harmless: a Windows llama-server started from a Linux supervisor is handed
// a POSIX model path that Windows cannot open, so it exits with "failed to open
// GGUF file (No such file or directory)" — which reads like a missing or corrupt
// model even though the file is right there.
//
// goos is a parameter instead of runtime.GOOS so both sides can be tested on one
// machine; callers pass runtime.GOOS.
func checkRuntimePlatform(path, goos string) error {
	f, err := os.Open(path)
	if err != nil {
		// Unreadable or missing: nothing to classify. exec reports that far more
		// precisely than a header check ever could.
		return nil
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		// Too short to carry a header: exec will report the real problem.
		return nil
	}
	isPE := magic[0] == 'M' && magic[1] == 'Z'
	isELF := string(magic[:]) == "\x7fELF"
	switch {
	case isPE && goos != "windows":
		return fmt.Errorf(
			"runtime %s is a Windows executable but this supervisor is a %s build; "+
				"start the supervisor on Windows (docs/operations/AgentRAG-启动与运维指南.md 4.6)",
			path, goos)
	case isELF && goos == "windows":
		return fmt.Errorf(
			"runtime %s is a Linux executable but this supervisor is a %s build; "+
				"the supervisor must run on the platform the runtime was built for",
			path, goos)
	}
	return nil
}
