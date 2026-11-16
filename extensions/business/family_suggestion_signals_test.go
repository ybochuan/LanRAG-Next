package business

import (
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestTitleIdentityStripsDocumentExtensions(t *testing.T) {
	cases := []struct {
		raw  string
		stem string
		year int
	}{
		{"《中国邮政储蓄银行公司业务上门服务管理办法（2022年修订版）》.docx", "中国邮政储蓄银行公司业务上门服务管理办法", 2022},
		{"中国邮政储蓄银行现金管理业务管理办法（2024年9月修订版）.doc", "中国邮政储蓄银行现金管理业务管理办法", 2024},
		{"中国邮政储蓄银行发票管理办法.doc", "中国邮政储蓄银行发票管理办法", 0},
		{"中国邮政储蓄银行公司结算业务操作规程（2022年版）.doc", "中国邮政储蓄银行公司结算业务操作规程", 2022},
	}
	for _, tc := range cases {
		stem, year := titleIdentity(tc.raw)
		if stem != tc.stem || year != tc.year {
			t.Fatalf("titleIdentity(%q) = %q,%d want %q,%d", tc.raw, stem, year, tc.stem, tc.year)
		}
	}
}

func TestDeclaredDocumentRelationIgnoresFileExtensions(t *testing.T) {
	related, ok := declaredDocumentRelation(
		weknora.AttachmentDocument{Title: "关于《中国邮政储蓄银行公司结算业务操作规程（2022年版）》的补充通知.doc"},
		weknora.AttachmentDocument{Title: "中国邮政储蓄银行公司结算业务操作规程（2022年版）.doc"},
	)
	if !ok || related != "related_to" {
		t.Fatalf("related document relation = %q,%v", related, ok)
	}
	amends, ok := declaredDocumentRelation(
		weknora.AttachmentDocument{Title: "关于《中国邮政储蓄银行现金管理业务管理办法（2020年修订版）》等事项的补充通知.doc"},
		weknora.AttachmentDocument{Title: "中国邮政储蓄银行现金管理业务管理办法（2020年修订版）.doc"},
	)
	if !ok || amends != "amends" {
		t.Fatalf("amend document relation = %q,%v", amends, ok)
	}
}

func TestCandidateSignalsWithProductionStyleExtensions(t *testing.T) {
	peer := scanDocument{doc: weknora.AttachmentDocument{ID: "peer", Title: "中国邮政储蓄银行现金管理业务管理办法（2018年修订版）.doc"}}
	anchor := scanDocument{doc: weknora.AttachmentDocument{ID: "anchor", Title: "中国邮政储蓄银行现金管理业务管理办法（2020年修订版）.doc"}, family: "family"}
	item, ok := candidateSignals("family", "", peer, anchor)
	if !ok || item.Kind != "membership" {
		t.Fatalf("membership candidate = %+v,%v", item, ok)
	}
	peer.family = "family"
	item, ok = candidateSignals("family", "peer", peer, anchor)
	if !ok || item.EdgeType != "supersedes" || item.DocumentID != "anchor" || item.TargetDocumentID != "peer" {
		t.Fatalf("supersedes candidate = %+v,%v", item, ok)
	}
}
