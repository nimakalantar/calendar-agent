package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesParentAndSetsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	if err := WriteFileAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWriteFileAtomicReplacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "second" {
		t.Fatalf("content = %q", data)
	}
}

func TestReadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value.json")
	if err := WriteJSONAtomic(path, map[string]int{"a": 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	var value map[string]int
	if err := ReadJSON(path, &value); err != nil {
		t.Fatal(err)
	}
	if value["a"] != 1 {
		t.Fatalf("value = %v", value)
	}
}

func TestReadJSONMissingAndCorrupt(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	if err := ReadJSON(missing, &map[string]int{}); !os.IsNotExist(err) {
		t.Fatalf("missing error = %v, want not-exist", err)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReadJSON(corrupt, &map[string]int{}); err == nil {
		t.Fatal("expected corrupt JSON error")
	}
}

func TestWriteJSONAtomicRejectsUnmarshalableValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value.json")
	if err := WriteJSONAtomic(path, make(chan int), 0o600); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestWriteFileAtomicReportsDirectoryFailure(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(parent, "child.txt"), []byte("x"), 0o600); err == nil {
		t.Fatal("expected directory error")
	}
}
