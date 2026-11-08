package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func attachmentSemanticFixture(id string) AttachmentSemantic {
	return AttachmentSemantic{
		AttachmentID:       id,
		TenantID:           7,
		KnowledgeBaseID:    "kb-a",
		ParentDocumentID:   "parent-1",
		ParentVersionID:    "v1",
		ParentFileHash:     strings.Repeat("a", 64),
		ParentTitle:        "中国邮政储蓄银行公司业务上门服务管理办法",
		OriginalFilename:   "annex-02.docx",
		DisplayName:        "合法性审查书面声明",
		AttachmentSequence: 2,
		ReferenceTitle:     "合法性审查书面声明",
		ReferenceLocation:  "第三章 结算账户管理",
		ReferenceExcerpt:   "附件2：合法性审查书面声明",
		MappingStatus:      "confirmed",
		MappingConfidence:  1,
		IndexPolicy:        "on_demand",
	}
}

func TestAttachmentSemanticsRoundTripAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDurable(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items := []AttachmentSemantic{attachmentSemanticFixture("att-1"), attachmentSemanticFixture("att-2")}
	items[1].OriginalFilename = "annex-03.docx"
	items[1].DisplayName = "账户管理协议"
	items[1].AttachmentSequence = 3
	if err = db.SaveAttachmentSemantics(ctx, items); err != nil {
		t.Fatal(err)
	}
	got, err := db.AttachmentSemantics(ctx, 7, []string{"att-1", "att-2", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["att-1"].DisplayName != "合法性审查书面声明" || got["att-2"].AttachmentSequence != 3 {
		t.Fatalf("unexpected semantics: %+v", got)
	}
	items[0].DisplayName = "合法性审查声明（修正）"
	if err = db.SaveAttachmentSemantics(ctx, items[:1]); err != nil {
		t.Fatal(err)
	}
	got, err = db.AttachmentSemantics(ctx, 7, []string{"att-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["att-1"].DisplayName != "合法性审查声明（修正）" {
		t.Fatalf("update not applied: %+v", got["att-1"])
	}
	other, err := db.AttachmentSemantics(ctx, 8, []string{"att-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("tenant isolation failed: %+v", other)
	}
}

func TestSaveAttachmentSemanticsRejectsInvalidHash(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDurable(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	item := attachmentSemanticFixture("att-1")
	item.ParentFileHash = "short"
	if err = db.SaveAttachmentSemantics(ctx, []AttachmentSemantic{item}); err == nil {
		t.Fatal("expected invalid parent hash error")
	}
}
