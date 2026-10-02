package gsm

import (
	"strings"
	"testing"
)

// MergeProjection must leave s unchanged when a later value is bad (kills merge-partial),
// and report the same bad variable on every call (kills merge-sort).
func TestReview2_MergeProjectionAtomicAndDeterministic(t *testing.T) {
	r := NewRegistry("rev2_merge")
	a := r.Int("a", 0, 2)
	r.Int("b", 0, 2)
	r.Int("c", 0, 2)
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	s := m.NewState()
	out, err := m.MergeProjection(s, Projection{Shared: map[string]uint64{"a": 2, "b": 3}})
	if err == nil || out.ID() != s.ID() || out.GetInt(a) != 0 {
		t.Fatalf("partial merge: out=%s err=%v", out, err)
	}
	for i := 0; i < 50; i++ {
		_, e := m.MergeProjection(s, Projection{Shared: map[string]uint64{"b": 3, "c": 3}})
		if !strings.Contains(e.Error(), `"b"`) {
			t.Fatalf("run %d reported %v, want b first", i, e)
		}
	}
}
