package geocode

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCoordCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newCoordCache(4)
	for _, key := range []string{"a", "b", "c", "d"} {
		cache.put(key, Coord{Lat: 1})
	}
	// Refresh "a" so the eviction of the oldest half keeps it.
	if _, ok := cache.get("a"); !ok {
		t.Fatal("a should be present")
	}
	cache.put("e", Coord{Lat: 2})

	for _, key := range []string{"a", "d", "e"} {
		if _, ok := cache.get(key); !ok {
			t.Fatalf("%s should survive eviction", key)
		}
	}
	for _, key := range []string{"b", "c"} {
		if _, ok := cache.get(key); ok {
			t.Fatalf("%s should have been evicted", key)
		}
	}
}

func TestCoordCacheHandlesSingleEntryLimit(t *testing.T) {
	cache := newCoordCache(1)
	cache.put("a", Coord{Lat: 1})
	cache.put("b", Coord{Lat: 2})
	if cache.len() != 1 {
		t.Fatalf("cache size = %d, want 1", cache.len())
	}
	if _, ok := cache.get("b"); !ok {
		t.Fatal("newest entry should remain")
	}
}

func TestFlushPersistsRecencyAndPruneKeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geocode.json")
	g := New("test", path)
	g.cachePut("a", Coord{Lat: 1})
	g.cachePut("b", Coord{Lat: 2})
	g.cachePut("c", Coord{Lat: 3})
	g.cacheGet("a")
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}

	parsed, err := readCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b", "c", "a"}
	if !reflect.DeepEqual(parsed.Order, want) {
		t.Fatalf("order = %v, want %v", parsed.Order, want)
	}

	removed, err := PruneCache(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	parsed, err = readCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Order, []string{"c", "a"}) {
		t.Fatalf("pruned order = %v", parsed.Order)
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("pruned entries = %v", parsed.Entries)
	}
}
