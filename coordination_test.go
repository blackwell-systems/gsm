package gsm

import (
	"fmt"
	"testing"
)

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

// Example_acceptWithCoordination walks the full loop: a federation that cannot converge, the exact
// adjustment to make, applying it, and then watching it converge. A "primary" and a "mirror" hold a
// bit; the mirror copies the primary, and the primary is forced to disagree with the mirror (an
// antitone constraint). As a cycle this oscillates and Build rejects it. CoordinationPlan names the
// one edge to coordinate (make the primary a single writer); BuildCoordinated accepts the residual,
// and driving it shows the mirror faithfully tracking the primary, valid and stable.
func Example_acceptWithCoordination() {
	primary := NewRegistry("primary")
	pv := primary.Int("v", 0, 1)
	primary.Event("set").Writes(pv).Apply(func(s State) State { return s.SetInt(pv, 1) }).Add()
	mirror := NewRegistry("mirror")
	mv := mirror.Int("v", 0, 1)

	fed := NewFederation("mirrors").
		Morphism(primary, mirror).Shared(mv).
		Map(func(s, d State) State { return d.SetInt(mv, s.GetInt(pv)) }).Add().
		Morphism(mirror, primary).Shared(pv).
		Map(func(s, d State) State { return d.SetInt(pv, 1-s.GetInt(mv)) }).Add()

	// 1. As is, the cycle cannot converge.
	if _, _, err := fed.Build(); err != nil {
		fmt.Println("build: rejected (non-monotone cycle)")
	}
	d, err := fed.DiagnoseCycle()
	if err != nil {
		fmt.Println("diagnose error:", err)
		return
	}
	fmt.Println("diagnose: converges =", d.Converges)

	// 2. The exact adjustment.
	plan := fed.CoordinationPlan()
	fmt.Println("coordinate:", plan)

	// 3. Make it, then 4. watch it converge.
	m, _, err := fed.BuildCoordinated(plan)
	if err != nil {
		fmt.Println("build error:", err)
		return
	}
	s := m.Apply(m.NewState(), primary, "set")
	fmt.Printf("after set: primary=%d mirror=%d valid=%v\n",
		m.Of(s, primary).GetInt(pv), m.Of(s, mirror).GetInt(mv), m.IsValid(s))

	// Output:
	// build: rejected (non-monotone cycle)
	// diagnose: converges = false
	// coordinate: [mirror->primary[v]]
	// after set: primary=1 mirror=1 valid=true
}
