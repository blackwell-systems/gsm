package gsm

import "testing"

// componentsRegistry is the shape of calc_components_cc1_iff (normalization-confluence
// coq/Calculus.v, section Components) as a gsm registry: two footprint components, x and y.
//
//   - x in {0,1,2} with invariant x != 2, repaired by x := 1 (so N(2) = 1);
//   - fix_x: when x == 2, set x := 0 (writes and reads only x);
//   - set_y: y := 1 (writes and reads only y).
//
// At the invalid state x = 2, fix_x does not absorb its component's repair:
// N(fix_x(2)) = 0 but N(fix_x(N(2))) = N(fix_x(1)) = 1. calc_components_cc1_iff says CC1 then
// fails at that state for the cross-component pair, while calc_components_cc1_valid says it
// holds at every valid state. The zero state (x = 0, y = 0) is valid, as BuildCompositional
// requires.
func componentsRegistry() (*Registry, Var, Var) {
	r := NewRegistry("components")
	x := r.Int("x", 0, 2)
	y := r.Int("y", 0, 1)
	r.DeclInvariant("x_not_2", Ne(V(x), Lit(2)), Do(Set(x, Lit(1))))
	r.DeclEventGuarded("fix_x", Eq(V(x), Lit(2)), Do(Set(x, Lit(0))))
	r.DeclEvent("set_y", Do(Set(y, Lit(1))))
	return r, x, y
}

// TestCompositional_InvalidStateCrossComponentPair is the probe for calc_components_cc1_iff:
// the cross-component pair (fix_x, set_y) diverges from the invalid state x = 2 when events are
// applied to it raw (event, then repair), which is the "CC at arbitrary states" reading the
// theorem refutes. gsm never applies an event to an unnormalized state: Machine.Apply
// normalizes an invalid input first (Apply(s, e) = Apply(Normalize(s), e)), so every order
// starts from a valid state, where calc_components_cc1_valid makes the pair commute. The test
// checks both halves on the lazy machine (BuildCompositional), the table machine (Build) and a
// federation component (FedMachine builds its components with Build).
func TestCompositional_InvalidStateCrossComponentPair(t *testing.T) {
	r, x, y := componentsRegistry()
	m, rep, err := r.BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	if rep.Components != 2 || rep.PairsDisjoint != 1 || rep.PairsBrute != 0 {
		t.Fatalf("want 2 components and the pair skipped as cross-component, got components=%d disjoint=%d brute=%d",
			rep.Components, rep.PairsDisjoint, rep.PairsBrute)
	}

	bad := m.NewState().SetInt(x, 2) // built by hand, violates x != 2
	if m.IsValid(bad) {
		t.Fatal("x = 2 should be invalid")
	}
	fix, sety := m.eventDefs[m.events["fix_x"]], m.eventDefs[m.events["set_y"]]

	// The theorem's counterexample shape: raw application from the invalid state diverges.
	raw12 := m.lazyApply(sety, m.lazyApply(fix, bad))
	raw21 := m.lazyApply(fix, m.lazyApply(sety, bad))
	if raw12.ID() == raw21.ID() {
		t.Fatalf("raw application from x = 2 should diverge (calc_components_cc1_iff), both gave %s", raw12)
	}
	if raw12.GetInt(x) != 0 || raw21.GetInt(x) != 1 {
		t.Fatalf("raw orders: want x = 0 and x = 1, got %s and %s", raw12, raw21)
	}

	// gsm's Apply normalizes the invalid input first, so both orders agree.
	check := func(kind string, apply func(State, string) State, start State) {
		t.Helper()
		ab := apply(apply(start, "fix_x"), "set_y")
		ba := apply(apply(start, "set_y"), "fix_x")
		if ab.ID() != ba.ID() {
			t.Fatalf("%s: Apply from the invalid state diverged: %s vs %s", kind, ab, ba)
		}
		if ab.GetInt(x) != 1 || ab.GetInt(y) != 1 {
			t.Fatalf("%s: want x = 1, y = 1 (normalize first, then both events), got %s", kind, ab)
		}
	}
	check("lazy", m.Apply, bad)

	tm, _, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	check("table", tm.Apply, tm.NewState().SetInt(x, 2))

	// A federation component: FedMachine.Apply goes through the component's Machine.Apply.
	fm, _, err := NewFederation("one").Add(r).Build()
	if err != nil {
		t.Fatalf("federation Build: %v", err)
	}
	fs := fm.NewState()
	fs.states[0] = fs.states[0].SetInt(x, 2)
	fab := fm.Apply(fm.Apply(fs, r, "fix_x"), r, "set_y")
	fba := fm.Apply(fm.Apply(fs, r, "set_y"), r, "fix_x")
	if fm.Of(fab, r).ID() != fm.Of(fba, r).ID() {
		t.Fatalf("FedMachine: Apply from an invalid component state diverged: %s vs %s", fm.Of(fab, r), fm.Of(fba, r))
	}
}
