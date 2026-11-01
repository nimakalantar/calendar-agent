package gcalendar

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
)

var testOwnership = calendaragent.Ownership{Property: "managedBy", Value: "calendar-agent"}

func testConfig() Config {
	return Config{
		Ownership: testOwnership,
		Schema: Schema{
			ManagedBy: "managedBy", Key: "planKey", SourceKey: "sourceEventId",
			Kind: "journeyKind", Link: "searchUri", LinkHash: "searchUriHash", ContentHash: "contentHash",
		},
	}
}

type fakeMetrics struct {
	mu   sync.Mutex
	incs map[string]float64
	sets map[string]float64
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{incs: map[string]float64{}, sets: map[string]float64{}}
}

func (m *fakeMetrics) Inc(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incs[metricKey(name, labels)]++
}

func (m *fakeMetrics) Set(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets[metricKey(name, labels)] = value
}

func (m *fakeMetrics) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	lines := make([]string, 0, len(m.incs)+len(m.sets))
	for key, value := range m.incs {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, value := range m.sets {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, labels[key]))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}
