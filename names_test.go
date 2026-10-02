package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// dupEventRegistry declares two events named "a". Independent("a", "b") resolves
// "a" to the first declaration (which commutes with b), but a built machine's
// Apply resolves "a" to the last (which does not), so the pair CC checks and the
// pair the runtime applies are different events.
func dupEventRegistry() *gsm.Registry {
	r := gsm.NewRegistry("dup")
	x := r.Int("x", 0, 3)
	y := r.Bool("y")
	r.On("a").Does(gsm.SetTo(y, 1)).Add()                                 // first "a": commutes with b
	r.On("b").Does(gsm.Inc(x)).Add()                                      // b: x += 1
	r.On("a").Does(gsm.Do(gsm.Set(x, gsm.Add(gsm.V(x), gsm.V(x))))).Add() // last "a": x *= 2
	r.Independent("a", "b")
	return r
}

// TestDuplicateEventName_CheckAndRuntimeDisagree is the bug: with two events named
// "a", the CC check covers the first and Apply runs the last, so a machine that
// diverges by event order was certified convergent. Build must reject it.
func TestDuplicateEventName_CheckAndRuntimeDisagree(t *testing.T) {
	m, _, err := dupEventRegistry().Build()
	if err == nil {
		s := m.NewState()
		ab := m.Apply(m.Apply(s, "a"), "b")
		ba := m.Apply(m.Apply(s, "b"), "a")
		t.Fatalf("Build accepted a registry with two events named %q; the declared pair (a, b) "+
			"was checked for the first \"a\" but Apply runs the last, and the order matters: "+
			"a;b = %s, b;a = %s", "a", ab, ba)
	}
	if !strings.Contains(err.Error(), `registry "dup": duplicate event name "a"`) {
		t.Fatalf("Build error does not name the duplicate: %v", err)
	}
}
