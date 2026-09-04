package storage

import (
	"path/filepath"
	"testing"
)

func TestStateJSONRoundTrip(t *testing.T) {
	root := t.TempDir()
	paths := Paths{DataDir: filepath.Join(root, "data")}
	type state struct {
		Count int `json:"count"`
	}
	if err := SaveStateJSON(paths, "safety.json", state{Count: 2}); err != nil {
		t.Fatal(err)
	}
	var loaded state
	found, err := LoadStateJSON(paths, "safety.json", &loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.Count != 2 {
		t.Fatalf("found=%v loaded=%#v", found, loaded)
	}
}

func TestStateJSONRejectsPathTraversal(t *testing.T) {
	paths := Paths{DataDir: t.TempDir()}
	if err := SaveStateJSON(paths, "../outside.json", map[string]int{"x": 1}); err == nil {
		t.Fatal("expected invalid state file name")
	}
}
