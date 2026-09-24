package gsm_test

import (
	"bytes"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestSugar_LowersToPrimitives is the load-bearing test for the layered design: a
// machine written with the friendly surface must serialize to byte-identical AST as
// the same machine written with the raw combinator primitives. If they ever diverge,
// the sugar has grown semantics the analyzable core (and the verified oracle) does
// not see, which is exactly what this layering forbids.
func TestSugar_LowersToPrimitives(t *testing.T) {
	build := func(friendly bool) []byte {
		r := gsm.NewRegistry("m")
		a := r.Int("a", 0, 5)
		b := r.Int("b", 0, 5)
		flag := r.Bool("flag")
		if friendly {
			r.Rule("a_cap").Require(gsm.AtMost(a, 3)).RepairWith(gsm.SetTo(a, 3)).Add()
			r.On("inc_a").Does(gsm.Inc(a)).Add()
			r.On("inc_b").Does(gsm.Inc(b)).Add()
			r.On("raise_flag").Does(gsm.Raise(flag)).Add()
		} else {
			r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))
			r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
			r.DeclEvent("inc_b", gsm.Do(gsm.Set(b, gsm.Add(gsm.V(b), gsm.Lit(1)))))
			r.DeclEvent("raise_flag", gsm.Do(gsm.Set(flag, gsm.Lit(1))))
		}
		var buf bytes.Buffer
		if err := r.WriteMachineAST(&buf); err != nil {
			t.Fatalf("WriteMachineAST (friendly=%v): %v", friendly, err)
		}
		return buf.Bytes()
	}

	sugar := build(true)
	primitive := build(false)
	if !bytes.Equal(sugar, primitive) {
		t.Fatalf("sugar did not lower to identical primitives:\n--- sugar ---\n%s\n--- primitive ---\n%s", sugar, primitive)
	}
}

// TestSugar_BuildsConvergent confirms the friendly surface still builds a convergent
// machine (WFC+CC) and behaves: a is capped at 3, independent events commute.
func TestSugar_BuildsConvergent(t *testing.T) {
	r := gsm.NewRegistry("sugar-commuting")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)

	r.Rule("a_cap").Require(gsm.AtMost(a, 3)).RepairWith(gsm.SetTo(a, 3)).Add()
	r.On("inc_a").Does(gsm.Inc(a)).Add()
	r.On("inc_b").Does(gsm.Inc(b)).Add()

	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	if !rep.WFC || !rep.CC {
		t.Fatalf("expected WFC+CC, got %s", rep)
	}
	s := m.NewState()
	for i := 0; i < 5; i++ {
		s = m.Apply(s, "inc_a")
	}
	if s.GetInt(a) != 3 {
		t.Fatalf("expected a capped at 3, got %d", s.GetInt(a))
	}
}
