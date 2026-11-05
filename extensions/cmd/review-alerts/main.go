// Command review-alerts lists scoped overdue alerts without creating/migrating a DB.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("review-alerts", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("db", "", "existing independent extension DB")
	tenant := flags.Uint64("tenant", 0, "explicit tenant ID")
	kb := flags.String("kb", "", "explicit knowledge base ID")
	limit := flags.Int("limit", 100, "maximum inbox items (1..1000)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *path == "" || *tenant == 0 || *kb == "" || *limit < 1 || *limit > 1000 {
		return errors.New("db, tenant and KB with limit 1..1000 required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := store.OpenReadOnly(ctx, *path)
	if err != nil {
		return errors.New("cannot open existing independent extension database")
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	// Schema6 adds independent provenance tables and schema7 adds lifecycle guards;
	// Schema9 adds inherited deadlines while retaining the review inbox contract.
	if err = tx.QueryRowContext(ctx, "SELECT MAX(version) FROM ext_schema_migrations").Scan(&version); err != nil || version < 5 || version > 13 {
		return errors.New("extension schema5 to schema10 required; this query never upgrades a database")
	}
	alerts, err := review.ListTx(ctx, tx, outbox.Scope{TenantID: *tenant, KnowledgeBaseID: *kb}, *limit, time.Now().UTC())
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(alerts)
}
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
