package lock

import (
	"path/filepath"
	"testing"
)

func TestAcquireIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	unlock, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("expected second acquire to fail")
	}
	unlock()

	unlock2, err := Acquire(path)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	unlock2()
}
