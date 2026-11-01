package geocode

import (
	"container/list"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/nimakalantar/calendar-agent/src/platform/fsutil"
)

const cacheVersion = 2

type cacheFile struct {
	Version int              `json:"version"`
	Entries map[string]Coord `json:"entries"`
	Order   []string         `json:"order,omitempty"`
}

// coordCache is a small LRU coordinate cache. Recency order is tracked so the
// least recently used half can be evicted when the cache reaches its limit.
type coordCache struct {
	entries    map[string]Coord
	order      *list.List
	items      map[string]*list.Element
	maxEntries int
	dirty      bool
}

func newCoordCache(maxEntries int) *coordCache {
	return &coordCache{
		entries:    map[string]Coord{},
		order:      list.New(),
		items:      map[string]*list.Element{},
		maxEntries: maxEntries,
	}
}

// newCoordCacheFromEntries restores persisted recency order and trims the cache
// to maxEntries when it has grown too large.
func newCoordCacheFromEntries(entries map[string]Coord, order []string, maxEntries int) *coordCache {
	cache := newCoordCache(maxEntries)
	keys := orderedKeys(entries, order)
	if len(keys) > maxEntries {
		keys = keys[len(keys)-maxEntries:]
		cache.dirty = true
	}
	for _, key := range keys {
		cache.entries[key] = entries[key]
		cache.items[key] = cache.order.PushBack(key)
	}
	return cache
}

// orderedKeys normalizes a persisted order, appending any entries that were
// missing from it so a damaged or older cache never hides coordinates.
func orderedKeys(entries map[string]Coord, order []string) []string {
	seen := make(map[string]bool, len(entries))
	keys := make([]string, 0, len(entries))
	for _, key := range order {
		if _, ok := entries[key]; !ok || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

func (c *coordCache) len() int { return len(c.entries) }

func (c *coordCache) get(key string) (Coord, bool) {
	coord, ok := c.entries[key]
	if ok {
		c.order.MoveToBack(c.items[key])
	}
	return coord, ok
}

func (c *coordCache) put(key string, coord Coord) {
	if len(c.entries) >= c.maxEntries {
		drop := max(1, len(c.entries)/2)
		for i := 0; i < drop; i++ {
			front := c.order.Front()
			if front == nil {
				break
			}
			oldest := front.Value.(string)
			c.order.Remove(front)
			delete(c.entries, oldest)
			delete(c.items, oldest)
		}
	}
	if element, exists := c.items[key]; exists {
		c.order.MoveToBack(element)
	} else {
		c.items[key] = c.order.PushBack(key)
	}
	c.entries[key] = coord
	c.dirty = true
}

// snapshot returns entries in recency order.
func (c *coordCache) snapshot() (map[string]Coord, []string) {
	entries := make(map[string]Coord, c.order.Len())
	order := make([]string, 0, c.order.Len())
	for element := c.order.Front(); element != nil; element = element.Next() {
		key := element.Value.(string)
		if coord, ok := c.entries[key]; ok {
			entries[key] = coord
			order = append(order, key)
		}
	}
	return entries, order
}

func (c *coordCache) markClean() { c.dirty = false }

// readCacheFile loads and parses a persisted cache. A missing file returns the
// underlying os error so callers can distinguish it with os.IsNotExist.
func readCacheFile(path string) (cacheFile, error) {
	var parsed cacheFile
	if err := fsutil.ReadJSON(path, &parsed); err != nil {
		if os.IsNotExist(err) {
			return cacheFile{}, err
		}
		return cacheFile{}, fmt.Errorf("cannot parse geocode cache %s: %w", path, err)
	}
	return parsed, nil
}

// writeCacheFile persists entries and their recency order atomically.
func writeCacheFile(path string, entries map[string]Coord, order []string) error {
	return fsutil.WriteJSONAtomic(path, cacheFile{Version: cacheVersion, Entries: entries, Order: order}, 0o600)
}

// Flush persists the cache if it changed.
func (g *Geocoder) Flush() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cachePath == "" || g.cache == nil || !g.cache.dirty {
		return nil
	}
	entries, order := g.cache.snapshot()
	if err := writeCacheFile(g.cachePath, entries, order); err != nil {
		return err
	}
	g.cache.markClean()
	return nil
}

// trimCache keeps the most recently used maxEntries entries.
func trimCache(entries map[string]Coord, order []string, maxEntries int) (map[string]Coord, []string, int) {
	keys := orderedKeys(entries, order)
	if len(keys) <= maxEntries {
		return entries, keys, 0
	}
	keep := keys[len(keys)-maxEntries:]
	trimmed := make(map[string]Coord, len(keep))
	for _, key := range keep {
		trimmed[key] = entries[key]
	}
	return trimmed, keep, len(keys) - maxEntries
}

// PruneCache trims a persisted cache to at most maxEntries entries.
func PruneCache(path string, maxEntries int) (int, error) {
	if path == "" {
		return 0, fmt.Errorf("geocode cache path is empty")
	}
	if maxEntries <= 0 {
		return 0, fmt.Errorf("maxEntries must be positive")
	}
	parsed, err := readCacheFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	trimmed, order, removed := trimCache(parsed.Entries, parsed.Order, maxEntries)
	if removed == 0 {
		return 0, nil
	}
	if err := writeCacheFile(path, trimmed, order); err != nil {
		return 0, err
	}
	return removed, nil
}
