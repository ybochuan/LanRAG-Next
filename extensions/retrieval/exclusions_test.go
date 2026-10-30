package retrieval

import (
	"reflect"
	"testing"
)

func TestExclusionsOnlySubtractFromAuthorizedScope(t *testing.T) {
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	for _, tc := range []struct {
		name       string
		authorized []string
		excluded   []string
		want       []string
		deny       bool
	}{
		{"subtract", []string{"a", "b", "c"}, []string{"b", "outside"}, []string{"a", "c"}, false},
		{"no_exclusions", []string{"a", "b"}, nil, []string{"a", "b"}, false},
		{"all_excluded", []string{"a"}, []string{"a"}, []string{}, true},
		{"no_authorization", nil, nil, []string{}, true},
		{"duplicates_and_empty_ids", []string{"", "a", "a", "b"}, []string{"b", "b"}, []string{"a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := Exclusions{Scope: scope, ExcludedDocumentIDs: tc.excluded}
			got, err := filter.Apply(scope, tc.authorized)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.KnowledgeIDs, tc.want) || got.DenyAll != tc.deny {
				t.Fatalf("got %+v, want IDs=%v deny=%v", got, tc.want, tc.deny)
			}
		})
	}
}

func TestExclusionsRejectDifferentTenantOrKnowledgeBase(t *testing.T) {
	filter := Exclusions{Scope: Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}}
	for _, scope := range []Scope{
		{TenantID: 8, KnowledgeBaseID: "kb-a"},
		{TenantID: 7, KnowledgeBaseID: "kb-b"},
		{},
	} {
		selection, err := filter.Apply(scope, []string{"a"})
		if err == nil || !selection.DenyAll || len(selection.KnowledgeIDs) != 0 {
			t.Fatalf("scope mismatch must deny: %+v, err=%v", selection, err)
		}
	}
}
