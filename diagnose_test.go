package gsm

import (
	"strings"
	"testing"
)

// TestDiagnoseCycle_NegationOrbit builds a two-registry negation loop (A copies to B, B copies the
// negation back to A). Its loop composite is a flip with no fixed point, so Build rejects the cycle
// (naming it) and DiagnoseCycle reports non-convergence with an orbit witness.
func TestDiagnoseCycle_NegationOrbit(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()

	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "->") {
		t.Fatalf("expected a cycle rejection naming the loop, got %v", err)
	}

	d, err := fed.DiagnoseCycle()
	if err != nil {
		t.Fatalf("DiagnoseCycle: %v", err)
	}
	if d == nil {
		t.Fatal("expected a cycle diagnostic, got nil")
	}
	if len(d.Cycle) != 2 {
		t.Fatalf("expected a 2-cycle, got %v", d.Cycle)
	}
	if d.Converges {
		t.Fatalf("negation loop must not converge; got Converges=true (%s)", d.String())
	}
	if len(d.Orbit) == 0 {
		t.Fatal("expected an orbit witness, got none")
	}
	// The composite is a fixed-point-free flip, so no seed settles: every seed is checked
	// and the cycle is obstructed.
	if !d.AllSeeds || d.Seeds != 4 || d.SectionExists || !d.Obstructed() {
		t.Fatalf("negation loop: want all 4 seeds checked and an obstruction, got %+v", d)
	}
	if !strings.Contains(d.String(), "no consistent state") {
		t.Fatalf("String should state the obstruction: %s", d)
	}
}

// TestDiagnoseCycle_SeedOrbitsButSectionExists is the counterexample of
// c15_definitive_claim_false (normalization-confluence coq/CohomologyGeneral.v): A copies its value
// to B, and B copies it back to A through a swap of {0,1} that fixes 2. The loop composite is that
// swap. From the zero seed it orbits 0 -> 1 -> 0, yet the value 2 is a consistent state, so one
// non-settling seed is not an obstruction. DiagnoseCycle must find the section by checking every
// seed (c15_exact_refuter: no section iff no seed reaches a fixed point) and must not report the
// cycle as obstructed.
func TestDiagnoseCycle_SeedOrbitsButSectionExists(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 2)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 2)
	swap := func(x int) int {
		switch x {
		case 0:
			return 1
		case 1:
			return 0
		}
		return x
	}
	fed := NewFederation("swaploop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, swap(s.GetInt(fb))) }).Add()

	if _, _, err := fed.Build(); err == nil {
		t.Fatal("expected Build to reject the cycle")
	}
	d, err := fed.DiagnoseCycle()
	if err != nil {
		t.Fatalf("DiagnoseCycle: %v", err)
	}
	if d == nil {
		t.Fatal("expected a cycle diagnostic, got nil")
	}
	if d.Converges || len(d.Orbit) == 0 {
		t.Fatalf("the zero seed should orbit (0 -> 1 -> 0), got %+v", d)
	}
	if !d.AllSeeds || d.Seeds != 9 {
		t.Fatalf("want all 9 seeds checked, got AllSeeds=%v Seeds=%d", d.AllSeeds, d.Seeds)
	}
	if !d.SectionExists || d.Obstructed() {
		t.Fatalf("value 2 is a consistent state; the cycle must not be reported obstructed: %s", d)
	}
	if d.Section != "B.fb=2 | A.fa=2" {
		t.Fatalf("want the section fa = fb = 2, got %q", d.Section)
	}
	if s := d.String(); !strings.Contains(s, "not an obstruction") || strings.Contains(s, "no consistent state") {
		t.Fatalf("String overclaims or omits the section: %s", s)
	}
}

// TestDiagnoseCycle_SeedBudget: when the seed combinations exceed maxDiagnoseSeeds, the
// diagnostic says only that the zero seed does not settle, and does not claim an obstruction.
func TestDiagnoseCycle_SeedBudget(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	pa := a.Int("pa", 0, 2047) // local padding: 2 * 2048 valid states
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	pb := b.Int("pb", 0, 2047)
	_, _ = pa, pb
	fed := NewFederation("wide").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
	d, err := fed.DiagnoseCycle()
	if err != nil {
		t.Fatalf("DiagnoseCycle: %v", err)
	}
	if d.Converges || d.AllSeeds || d.SectionExists || d.Obstructed() {
		t.Fatalf("over the seed budget: want an unchecked, non-obstructed diagnostic, got %+v", d)
	}
	if !strings.Contains(d.String(), "may still exist") {
		t.Fatalf("String should say other seeds were not checked: %s", d)
	}
}

// TestDiagnoseCycle_AcyclicNil confirms an acyclic network yields no cycle diagnostic.
func TestDiagnoseCycle_AcyclicNil(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("chain").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add()

	d, err := fed.DiagnoseCycle()
	if err != nil {
		t.Fatalf("DiagnoseCycle: %v", err)
	}
	if d != nil {
		t.Fatalf("acyclic network should have no cycle diagnostic, got %s", d.String())
	}
}

// TestDiagnoseCycle_IdentitySettles confirms a loop whose composite is the identity (copy around)
// settles from the zero seed, so the diagnostic reports convergence (the cycle is still named).
func TestDiagnoseCycle_IdentitySettles(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, s.GetInt(fb)) }).Add()

	d, err := fed.DiagnoseCycle()
	if err != nil {
		t.Fatalf("DiagnoseCycle: %v", err)
	}
	if d == nil || !d.Converges || !d.SectionExists || d.Obstructed() {
		t.Fatalf("identity loop should settle from the zero seed, got %v", d)
	}
}
