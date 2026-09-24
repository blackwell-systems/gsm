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

// TestSugar_LabelAndRelationLowering checks the enum-by-label and two-variable
// relation sugar lowers to the expected numeric/combinator AST, and that a typo in
// a label is caught immediately.
func TestSugar_LabelAndRelationLowering(t *testing.T) {
	r := gsm.NewRegistry("labels")
	state := r.Enum("state", "closed", "open", "locked") // indices 0,1,2
	other := r.Enum("other", "a", "b")

	// IsLabel / SetLabel resolve labels to indices; serialize to prove it.
	r.Rule("stay_open").Require(gsm.IsLabel(state, "open")).RepairWith(gsm.SetLabel(state, "open")).Add()
	// A two-variable relation and a Toggle-style write on enums (index arithmetic).
	r.On("sync").Does(gsm.Copy(other, state)).Add()

	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	got := buf.String()
	// "open" is index 1; SetLabel(state,"open") -> (set 0 (lit 1)); IsLabel -> (eq (var 0) (lit 1)).
	wantInv := "(inv (eq (var 0) (lit 1)) (do (set 0 (lit 1))))"
	wantEv := "(ev (do (set 1 (var 0))))"
	if !bytes.Contains(buf.Bytes(), []byte(wantInv)) {
		t.Fatalf("label sugar did not lower as expected:\nwant substring: %s\ngot:\n%s", wantInv, got)
	}
	if !bytes.Contains(buf.Bytes(), []byte(wantEv)) {
		t.Fatalf("Copy did not lower as expected:\nwant substring: %s\ngot:\n%s", wantEv, got)
	}

	// A typo in a label is a construction-time panic, not a silent wrong index.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on unknown enum label")
			}
		}()
		_ = gsm.IsLabel(state, "nope")
	}()
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
