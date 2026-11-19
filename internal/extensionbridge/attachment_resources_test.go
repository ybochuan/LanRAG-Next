package extensionbridge

import (
	"reflect"
	"strings"
	"testing"
)

func TestAttachmentResourceSnapshotRejectsTampering(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("k", 32))
	want := AttachmentResources{Tenant: 7, KB: "kb", Document: "doc", Hash: "revision", References: []string{"resource://original", "resource://image"}}
	token, err := SignAttachmentResources(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyAttachmentResources(token)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("resource snapshot = %+v %v", got, err)
	}
	if _, err = VerifyAttachmentResources("0" + token[1:]); err == nil {
		t.Fatal("modified resource list accepted")
	}
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("x", 32))
	if _, err = VerifyAttachmentResources(token); err == nil {
		t.Fatal("invalid signing key accepted")
	}
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", "")
	if _, err = SignAttachmentResources(want); err == nil {
		t.Fatal("unsigned resource snapshot accepted")
	}
}
