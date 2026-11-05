// Command outbox-retry requeues one dead projection in an existing extension database.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func main() {
	path := flag.String("db", "", "existing extension SQLite database path")
	tenant := flag.Uint64("tenant", 0, "tenant ID")
	kb := flag.String("kb", "", "knowledge base ID")
	id := flag.String("id", "", "dead outbox ID")
	flag.Parse()
	if *path == "" || *tenant == 0 || strings.TrimSpace(*kb) == "" || strings.TrimSpace(*id) == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := retry(*path, outbox.Scope{TenantID: *tenant, KnowledgeBaseID: *kb}, *id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintln(os.Stderr, "no dead projection matches the given scope and ID")
		} else {
			fmt.Fprintln(os.Stderr, "extension outbox retry failed")
		}
		os.Exit(1)
	}
	fmt.Println("dead projection requeued")
}

func retry(path string, scope outbox.Scope, id string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("existing extension database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer db.Close()
	return outbox.NewStore(db).RetryDead(ctx, scope, id, time.Now().UTC())
}
