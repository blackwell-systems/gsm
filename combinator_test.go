package gsm_test

import (
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestCombinator_ConfluenceMachine ports the closure-based commuting machine to
// the combinator vocabulary and shows it reads as Go, builds convergent (WFC+CC),
// and (because footprints are derived from the expression tree) passes
// compositional footprint-conformance by construction.
func TestCombinator_ConfluenceMachine(t *testing.T) {
	r := gsm.NewRegistry("commuting-combinators")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)
	flag := r.Bool("flag")

	// a is capped at 3 by compensation; incrementing past the cap clamps.
	r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))

	r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
	r.DeclEvent("inc_b", gsm.Do(gsm.Set(b, gsm.Add(gsm.V(b), gsm.Lit(1)))))
	r.DeclEvent("raise_flag", gsm.Do(gsm.Set(flag, gsm.Lit(1))))

	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("combinator machine failed to build: %v\n%s", err, rep)
	}
	if !rep.WFC || !rep.CC {
		t.Fatalf("expected WFC+CC, got %s", rep)
	}

	// Behaves like the closure version: incrementing a past the cap clamps to 3.
	s := m.NewState()
	for i := 0; i < 5; i++ {
		s = m.Apply(s, "inc_a")
	}
	if s.GetInt(a) != 3 {
		t.Fatalf("expected a clamped to 3, got %d", s.GetInt(a))
	}
	// Independent events are order-independent.
	s1 := m.Apply(m.Apply(m.NewState(), "inc_a"), "inc_b")
	s2 := m.Apply(m.Apply(m.NewState(), "inc_b"), "inc_a")
	if s1.GetInt(a) != s2.GetInt(a) || s1.GetInt(b) != s2.GetInt(b) {
		t.Fatal("independent events not order-independent")
	}
}

// TestCombinator_FootprintConformanceByConstruction: the same machine, when built
// compositionally, passes footprint conformance without any hand-declared
// Watches/Writes -- the footprints came from the expression trees.
func TestCombinator_FootprintConformanceByConstruction(t *testing.T) {
	r := gsm.NewRegistry("wide-combinators")
	const N = 8
	for k := 0; k < N; k++ {
		v := r.Int("v", 0, 7)
		r.DeclInvariant("cap", gsm.Le(gsm.V(v), gsm.Lit(5)), gsm.Do(gsm.Set(v, gsm.Lit(5))))
		r.DeclEvent("inc", gsm.Do(gsm.Set(v, gsm.Add(gsm.V(v), gsm.Lit(1)))))
	}

	_, rep, err := r.BuildCompositional()
	if err != nil {
		t.Fatalf("compositional build of combinator machine failed: %v\n%s", err, rep)
	}
	if !rep.FootprintChecked {
		t.Fatal("expected footprint conformance to hold by construction")
	}
	if rep.Components != N {
		t.Fatalf("expected %d independent components, got %d", N, rep.Components)
	}
}
