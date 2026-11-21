package main

import (
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/cmd/desktop/agentrag"
)

type installedDesktop struct {
	profile    string
	host       *agentrag.Host
	supervisor *agentrag.ModelSupervisor
	close      func()
}

func (d *installedDesktop) writeCoreAddress(address string) error {
	return os.WriteFile(filepath.Join(d.profile, "core.url"), []byte(address+"\n"), 0o600)
}
