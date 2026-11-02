//go:build !windows

package parser

import "os"

// syncArtifactDirectory flushes directory entries after file creation/rename.
// Filesystems may reject directory sync: propagate that failure before SQL/ACK.
// This barrier does not prove the durability of storage hardware or ancestors
// provisioned outside this operation, including the artifact root itself.
func syncArtifactDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
