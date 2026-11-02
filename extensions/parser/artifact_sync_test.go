package parser

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func artifactFixtureRef() artifactRef {
	return artifactRef{ScopeIdentity: ScopeIdentity{TenantID: 7, KnowledgeBaseID: "kb-a"}, DocumentID: "doc-a", SourceFileID: "f-a", MinerUVersion: "4.0.1"}
}

func TestArtifactDirectoryBarriersAndRetry(t *testing.T) {
	root := t.TempDir()
	var order []string
	a := &artifactStore{path: root, syncDirectory: func(root *os.Root, name string) error {
		if strings.HasPrefix(name, ".pending-") {
			for _, file := range []string{"middle.json", "manifest.json"} {
				if _, err := root.Stat(name + "/" + file); err != nil {
					t.Fatal("staging sync before both files", err)
				}
			}
			if _, err := root.Stat("doc-a"); !os.IsNotExist(err) {
				t.Fatal("document published before staging directory barrier")
			}
			order = append(order, "stage")
		} else {
			if _, err := root.Stat("doc-a/middle.json"); err != nil {
				t.Fatal("publication barrier before rename", err)
			}
			order = append(order, name)
		}
		return nil
	}}
	archive := middleZIP(t, "middle_json.json")
	if err := a.persist(artifactFixtureRef(), archive); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"stage", "."}) {
		t.Fatalf("publication barriers=%v", order)
	}
	order = nil
	if err := a.persist(artifactFixtureRef(), archive); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"doc-a", "."}) {
		t.Fatalf("retry skipped existing directory barriers: %v", order)
	}
}

func TestArtifactDirectoryBarrierFailureIsRetriable(t *testing.T) {
	for _, failure := range []string{"stage", "root"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			injected := errors.New("injected directory sync failure")
			a := &artifactStore{path: root, syncDirectory: func(_ *os.Root, name string) error {
				if (failure == "stage" && strings.HasPrefix(name, ".pending-")) || (failure == "root" && name == ".") {
					return injected
				}
				return nil
			}}
			archive := middleZIP(t, "middle_json.json")
			if err := a.persist(artifactFixtureRef(), archive); !errors.Is(err, injected) {
				t.Fatalf("directory failure swallowed: %v", err)
			}
			_, err := os.Stat(filepath.Join(root, "doc-a", "middle.json"))
			if failure == "stage" && !os.IsNotExist(err) {
				t.Fatal("staging failure published a document")
			}
			if failure == "root" && err != nil {
				t.Fatal("rename window was not reached", err)
			}
			a.syncDirectory = nil
			if err := a.persist(artifactFixtureRef(), archive); err != nil {
				t.Fatal("retry could not repair barriers", err)
			}
		})
	}
}
