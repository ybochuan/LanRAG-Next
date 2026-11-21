// The installed entry point supplies the bundled dictionaries before the
// desktop's native text tokenizer initializes.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir := filepath.Dir(executable)
	cmd := exec.Command(filepath.Join(dir, "LanRAG Desktop.exe"), os.Args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JIEBA_DICT_DIR=resources/jieba")
	// Let exec supply a valid null stdout when Windows launches the GUI without a console.
	// Core writes to stdout before its configured log file.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if exited, ok := err.(*exec.ExitError); ok {
			os.Exit(exited.ExitCode())
		}
		os.Exit(1)
	}
}
