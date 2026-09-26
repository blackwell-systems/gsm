package gsm

import "testing"

// TestCoordinationPlan_NegationLoop: the two-registry negation loop is a non-monotone cycle that
// Build rejects. CoordinationPlan names one morphism edge to coordinate, and BuildCoordinated then
// accepts the federation (the residual, with that edge externally coordinated, is acyclic and
// converges).
func TestCoordinationPlan_NegationLoop(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()

	if _, _, err := fed.Build(); err == nil {
		t.Fatal("expected Build to reject the non-monotone cycle")
	}

	plan := fed.CoordinationPlan()
	if len(plan) != 1 {
		t.Fatalf("expected a 1-edge coordination plan for a single 2-cycle, got %d: %v", len(plan), plan)
	}
	if len(plan[0].Shared) == 0 {
		t.Fatalf("coordination point should name the shared variables it controls: %v", plan[0])
	}

	m, _, err := fed.BuildCoordinated(plan)
	if err != nil {
		t.Fatalf("BuildCoordinated should accept the federation given the coordination, got %v", err)
	}
	if m == nil {
		t.Fatal("BuildCoordinated returned a nil machine")
	}
	// The coordinated residual is acyclic and drives the remaining morphism.
	if len(m.Registries()) != 2 {
		t.Fatalf("coordinated machine has %d components, want 2", len(m.Registries()))
	}
}

// TestCoordinationPlan_AcyclicIsEmpty: an acyclic federation needs no coordination, and
// BuildCoordinated with the empty plan is exactly Build.
func TestCoordinationPlan_AcyclicIsEmpty(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("chain").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add()

	if plan := fed.CoordinationPlan(); plan != nil {
		t.Fatalf("acyclic federation should need no coordination, got %v", plan)
	}
	if _, _, err := fed.BuildCoordinated(nil); err != nil {
		t.Fatalf("BuildCoordinated(nil) on an acyclic federation should build, got %v", err)
	}
}
