//go:build windows

package parser

import "os"

// Windows keeps the existing file Sync + rename behavior. os.File.Sync on an
// ordinary directory handle is not a portable directory flush on Windows.
// Do not advertise the POSIX directory barrier or physical power-loss safety.
func syncArtifactDirectory(_ *os.Root, _ string) error { return nil }
