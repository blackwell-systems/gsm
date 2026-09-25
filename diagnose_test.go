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
	if d == nil || !d.Converges {
		t.Fatalf("identity loop should settle from the zero seed, got %v", d)
	}
}
