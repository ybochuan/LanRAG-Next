package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
)

func TestGraphManagementIsScopedReadonlyAndCanOmitTitles(t *testing.T) {
	s := fixture(t)
	if err := s.db.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO ext_lineage_node VALUES('node','7','kb','document_version','doc','2026-11-05T00:00:00Z')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.WithNativeSQLite("", func(_ context.Context, kb string, ids []string) (map[string]string, error) {
		if kb != "kb" || len(ids) != 1 || ids[0] != "doc" {
			t.Fatal("unscoped title lookup")
		}
		return nil, errors.New("unavailable")
	})
	w := call(s, `{"action":"business_lineage_graph","tenant_id":7,"knowledge_base_id":"kb","can_write":false,"graph_mode":"version-ancestry"}`, s.key)
	var out struct{ Data lineage.GraphSnapshot }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || len(out.Data.Nodes) != 1 || !out.Data.TitlesUnavailable {
		t.Fatalf("readonly graph: %d %s", w.Code, w.Body)
	}
	w = call(s, `{"action":"business_lineage_graph","tenant_id":8,"knowledge_base_id":"kb"}`, s.key)
	if w.Code != 403 {
		t.Fatal("foreign graph scope", w.Code)
	}
}
