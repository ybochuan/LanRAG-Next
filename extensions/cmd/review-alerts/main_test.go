package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestCLIRecognizesOnlyCompatibleReviewSchemas(t *testing.T) {
	for _, version := range []int{5, 6, 7, 8, 9, 10, 11, 12} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ext.db")
			raw, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = raw.Exec("CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)"); e != nil {
				t.Fatal(e)
			}
			files, e := filepath.Glob("../../store/migrations/*.sql")
			if e != nil {
				t.Fatal(e)
			}
			for i, file := range files {
				if i >= version {
					break
				}
				body, e := os.ReadFile(file)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = raw.Exec(string(body)); e != nil {
					t.Fatal(e)
				}
				if _, e = raw.Exec("INSERT INTO ext_schema_migrations VALUES(?)", i+1); e != nil {
					t.Fatal(e)
				}
			}
			if version == 12 {
				if _, e = raw.Exec("INSERT INTO ext_schema_migrations VALUES(12)"); e != nil {
					t.Fatal(e)
				}
			}
			if e = raw.Close(); e != nil {
				t.Fatal(e)
			}
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			e = run(context.Background(), []string{"-db", path, "-tenant", "7", "-kb", "kb"}, &out)
			if (e != nil) != (version == 12) {
				t.Fatalf("schema%d query: %v", version, e)
			}
			if version == 12 && out.Len() != 0 {
				t.Fatal("unknown schema emitted data")
			}
			after, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(before, after) {
				t.Fatal("schema query wrote database")
			}
		})
	}
}

func TestCLIReadsScopedInboxWithoutDatabaseWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	svc := review.NewService(db, func() time.Time { return now })
	for _, scope := range []lifecycle.Scope{{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "ours"}, {TenantID: 8, KnowledgeBaseID: "kb-a", DocumentID: "foreign"}, {TenantID: 7, KnowledgeBaseID: "kb-b", DocumentID: "other-kb"}} {
		if err = lifecycle.NewConfiguration(db).PutAnnotations(ctx, scope, lifecycle.Annotations{ReviewDue: &past}, 0); err != nil {
			t.Fatal(err)
		}
		if err = svc.Check(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = run(ctx, []string{"-db", path, "-tenant", "7", "-kb", "kb-a"}, &output); err != nil {
		t.Fatal(err)
	}
	var alerts []review.Alert
	if err = json.Unmarshal(output.Bytes(), &alerts); err != nil || len(alerts) != 1 || alerts[0].DocumentID != "ours" {
		t.Fatalf("%+v %v", alerts, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only command changed the main DB")
	}
}
func TestCLIRefusesMissingFileOrScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	for _, args := range [][]string{{"-db", path, "-tenant", "7", "-kb", "kb"}, {"-db", path}, {"-tenant", "7", "-kb", "kb"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("unexpected success/output for %v", args)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created database during query")
	}
}
