package gsm_test

import (
	"fmt"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestBuildCompositional_ScalesBeyondGlobalEnumeration builds a machine whose
// GLOBAL state space is far past Build's enumeration ceiling (10 independent
// 3-bit counters = 30 bits ~= 1e9 states), but whose footprint components are
// each tiny (one counter, 8 states). Build must reject it; BuildCompositional
// certifies it per component.
func TestBuildCompositional_ScalesBeyondGlobalEnumeration(t *testing.T) {
	const N = 10
	r := gsm.NewRegistry("wide")
	for k := 0; k < N; k++ {
		v := r.Int(fmt.Sprintf("a%d", k), 0, 7) // 3 bits each; 30 bits total
		r.Invariant(fmt.Sprintf("cap%d", k)).Watches(v).
			Holds(func(s gsm.State) bool { return s.GetInt(v) <= 5 }).
			Repair(func(s gsm.State) gsm.State { return s.SetInt(v, 5) }).Add()
		r.Event(fmt.Sprintf("inc%d", k)).Writes(v).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
	}

	// Global enumeration cannot certify this machine.
	if _, _, err := r.Build(); err == nil {
		t.Fatal("expected Build to reject a 30-bit state space")
	}

	// Footprint-local certification can.
	_, rep, err := r.BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	if !rep.WFC || !rep.CC {
		t.Fatalf("expected WFC+CC, got WFC=%v CC=%v", rep.WFC, rep.CC)
	}
	if rep.Components != N {
		t.Fatalf("expected %d components, got %d", N, rep.Components)
	}
	if rep.MaxComponentStates != 8 {
		t.Fatalf("expected largest component = 8 states, got %d", rep.MaxComponentStates)
	}
	// All pairs are cross-component, so all CC certified by disjointness.
	if rep.PairsBrute != 0 || rep.PairsDisjoint == 0 {
		t.Fatalf("expected all-disjoint CC, got disjoint=%d brute=%d", rep.PairsDisjoint, rep.PairsBrute)
	}
}

// TestBuildCompositional_Runtime checks the lazy machine computes correctly:
// compensation clamps, valid states stay valid, and independent events are
// order-independent.
func TestBuildCompositional_Runtime(t *testing.T) {
	r := gsm.NewRegistry("wide")
	a := r.Int("a", 0, 7)
	b := r.Int("b", 0, 7)
	r.Invariant("acap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 5) }).Add()
	r.Invariant("bcap").Watches(b).
		Holds(func(s gsm.State) bool { return s.GetInt(b) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(b, 5) }).Add()
	r.Event("inca").Writes(a).Apply(func(s gsm.State) gsm.State { return s.SetInt(a, s.GetInt(a)+1) }).Add()
	r.Event("incb").Writes(b).Apply(func(s gsm.State) gsm.State { return s.SetInt(b, s.GetInt(b)+1) }).Add()

	m, _, err := r.BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional: %v", err)
	}

	if !m.IsValid(m.NewState()) {
		t.Fatal("zero state should be valid")
	}

	// Incrementing a past the cap clamps to 5 (compensation fires at runtime).
	s := m.NewState()
	for i := 0; i < 7; i++ {
		s = m.Apply(s, "inca")
	}
	if s.GetInt(a) != 5 {
		t.Fatalf("expected a clamped to 5, got %d", s.GetInt(a))
	}

	// Order independence of independent events (compare via public getters, since
	// State holds a slice and is not directly comparable).
	s1 := m.Apply(m.Apply(m.NewState(), "inca"), "incb")
	s2 := m.Apply(m.Apply(m.NewState(), "incb"), "inca")
	if s1.GetInt(a) != s2.GetInt(a) || s1.GetInt(b) != s2.GetInt(b) {
		t.Fatalf("independent events not order-independent: a=%d/%d b=%d/%d",
			s1.GetInt(a), s2.GetInt(a), s1.GetInt(b), s2.GetInt(b))
	}
	if s1.GetInt(a) != 1 || s1.GetInt(b) != 1 {
		t.Fatalf("expected a=1 b=1, got a=%d b=%d", s1.GetInt(a), s1.GetInt(b))
	}
}

// TestBuildCompositional_DetectsNonTermination: a repair that does not make
// progress must fail the WFC check locally.
func TestBuildCompositional_DetectsNonTermination(t *testing.T) {
	r := gsm.NewRegistry("bad")
	a := r.Int("a", 0, 7)
	// Repair that never restores validity (increments instead of clamping).
	r.Invariant("acap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, s.GetInt(a)+1) }).Add()
	r.Event("inca").Writes(a).Apply(func(s gsm.State) gsm.State { return s.SetInt(a, s.GetInt(a)+1) }).Add()

	if _, _, err := r.BuildCompositional(); err == nil {
		t.Fatal("expected WFC failure for a non-terminating repair")
	}
}
