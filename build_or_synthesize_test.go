package gsm_test

import (
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestBuildOrSynthesize_BuildsAsWritten: a registry whose repair already converges builds directly,
// and no synthesis is reported.
func TestBuildOrSynthesize_BuildsAsWritten(t *testing.T) {
	r := gsm.NewRegistry("cap")
	n := r.Int("n", 0, 3)
	r.Invariant("le2").Watches(n).
		Holds(func(s gsm.State) bool { return s.GetInt(n) <= 2 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(n, 2) }).Add()
	r.Event("inc").Writes(n).Apply(func(s gsm.State) gsm.State {
		b := s.GetInt(n)
		if b < 3 {
			b++
		}
		return s.SetInt(n, b)
	}).Add()

	m, syn, err := r.BuildOrSynthesize()
	if err != nil {
		t.Fatalf("BuildOrSynthesize on a convergent registry should succeed, got %v", err)
	}
	if m == nil {
		t.Fatal("expected a machine")
	}
	if syn != nil {
		t.Fatalf("no synthesis should be reported when Build succeeds as written, got:\n%s", syn)
	}
}

// TestBuildOrSynthesize_FallsBackToSynthesis: a registry with an invariant but no repair fails Build;
// BuildOrSynthesize synthesizes a convergent compensation, returns the machine and a Synthesis
// describing what it added, and the machine is order-independent and stays valid.
func TestBuildOrSynthesize_FallsBackToSynthesis(t *testing.T) {
	r := gsm.NewRegistry("approval")
	stage := r.Enum("stage", "pending", "approved", "held")
	balance := r.Int("balance", 0, 2)
	r.Invariant("funded_if_approved").Watches(stage, balance).
		Holds(func(s gsm.State) bool { return s.Get(stage) != "approved" || s.GetInt(balance) >= 2 }).
		Add() // no Repair: Build will fail, synthesis supplies one
	r.Event("approve").Writes(stage).
		Apply(func(s gsm.State) gsm.State { return s.Set(stage, "approved") }).Add()
	r.Event("credit").Writes(stage, balance).
		Apply(func(s gsm.State) gsm.State {
			b := s.GetInt(balance)
			if b < 2 {
				b++
			}
			return s.SetInt(balance, b)
		}).Add()

	// Build alone fails (the invariant has no repair, so the rules do not converge as written).
	if _, _, err := r.Build(); err == nil {
		t.Fatal("expected Build to fail for a registry with an unrepaired invariant")
	}

	m, syn, err := r.BuildOrSynthesize()
	if err != nil {
		t.Fatalf("BuildOrSynthesize should synthesize a convergent compensation, got %v", err)
	}
	if syn == nil || !syn.Convergent {
		t.Fatalf("expected a convergent synthesis to be reported, got %v", syn)
	}
	if len(syn.Repairs()) == 0 {
		t.Fatal("synthesis should report the repairs it added (what changed)")
	}
	if m == nil {
		t.Fatal("expected the synthesized machine")
	}
	// The synthesized machine converges: applying the same events in either order agrees, and stays valid.
	s0 := m.NewState()
	ab := m.Apply(m.Apply(s0, "approve"), "credit")
	ba := m.Apply(m.Apply(s0, "credit"), "approve")
	if ab.ID() != ba.ID() {
		t.Fatalf("synthesized machine is not order-independent: %s vs %s", ab, ba)
	}
	if !m.IsValid(ab) {
		t.Fatalf("synthesized machine reached an invalid state: %s", ab)
	}
}
