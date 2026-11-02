package host

import (
	"errors"
	"os"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// databaseInitializationError exposes actionable categories, never raw SQL,
// paths, table names or configuration values from an underlying error.
func databaseInitializationError(err error) error {
	hint := "check directory permissions, SQLite file integrity and filesystem locking"
	switch {
	case errors.Is(err, store.ErrManualSchema):
		hint = "untracked schema does not exactly match migrations; original database preserved"
	case strings.Contains(err.Error(), "foreign table"):
		hint = "native/mixed database refused; use independent data/agentrag_ext.db, not data/weknora.db"
	case strings.Contains(strings.ToLower(err.Error()), "locked"), strings.Contains(strings.ToLower(err.Error()), "busy"):
		hint = "SQLite is busy; stop other writers and retry; on WSL consider a database on the Linux filesystem"
	case errors.Is(err, os.ErrPermission):
		hint = "database directory or file is not writable"
	case strings.Contains(err.Error(), "unsupported extension schema"):
		hint = "database schema is newer than this Host; use a matching Host version"
	}
	return errors.New("extension database initialization failed: " + hint)
}
