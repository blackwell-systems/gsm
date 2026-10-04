package gsm

import (
	"errors"
	"strings"
	"testing"
)

// The C2 check (repaired CC) ported from normalization-confluence coq/FederationEvents.v.

// slotTarget builds the target of c2_counterexample: state ((slot_a, slot_b), audit) with
// slot_a shared. swap exchanges the two slots; audit records slot_a xor slot_b. The two commute
// on the target alone (xor is symmetric).
func slotTarget(name string) (*Registry, Var, Var, Var) {
	dst := NewRegistry(name)
	a := dst.Bool("slot_a")
	b := dst.Bool("slot_b")
	audit := dst.Bool("audit")
	dst.Event("swap").Writes(a, b).
		Apply(func(s State) State { return s.SetBool(a, s.GetBool(b)).SetBool(b, s.GetBool(a)) }).Add()
	dst.Event("audit").Writes(audit).
		Apply(func(s State) State { return s.SetBool(audit, s.GetBool(a) != s.GetBool(b)) }).Add()
	return dst, a, b, audit
}

// closedSource is a source whose invariant keeps its own slot_a closed, so the only image the
// morphism can produce is slot_a = false (C1 then holds trivially: the shared part never moves).
func closedSource(name string) (*Registry, Var) {
	src := NewRegistry(name)
	a := src.Bool("slot_a")
	src.Invariant("closed").Watches(a).
		Holds(func(s State) bool { return !s.GetBool(a) }).
		Repair(func(s State) State { return s.SetBool(a, false) }).Add()
	return src, a
}

// c2Federation is c2_counterexample: Common, C1 and the target's own CC hold, C2 fails, and the
// federation delivers audit,swap and swap,audit to different audit bits.
func c2Federation() (*Federation, *Registry, Var, Var, Var) {
	src, sa := closedSource("src")
	dst, a, b, audit := slotTarget("dst")
	f := NewFederation("slots")
	f.Morphism(src, dst).Shared(a).
		Map(func(s, d State) State { return d.SetBool(a, s.GetBool(sa)) }).Add()
	return f, dst, a, b, audit
}

// TestC2_Counterexample is the regression for c2_counterexample: Build must reject it with a
// *SameTargetOrderError (not a *CrossOrderError: C1 holds), naming both events, the target, the
// source witness, the target state and the two diverging results.
func TestC2_Counterexample(t *testing.T) {
	f, dst, _, _, audit := c2Federation()

	// The target's own CC holds: the defect is invisible to the component check.
	if _, _, err := dst.Build(); err != nil {
		t.Fatalf("target alone should build (local CC holds): %v", err)
	}

	_, _, err := f.Build()
	if err == nil {
		t.Fatal("Build accepted c2_counterexample")
	}
	var ce *CrossOrderError
	if errors.As(err, &ce) {
		t.Fatalf("C1 holds here (single image); want *SameTargetOrderError, got %v", err)
	}
	var se *SameTargetOrderError
	if !errors.As(err, &se) {
		t.Fatalf("want *SameTargetOrderError, got %T: %v", err, err)
	}
	if se.Federation != "slots" || se.Target != "dst" || se.Morphism != "morphism src→dst" {
		t.Fatalf("wrong failure: %+v", se)
	}
	if pair := se.First + "," + se.Second; pair != "swap,audit" && pair != "audit,swap" {
		t.Fatalf("wrong event pair %q", pair)
	}
	if se.FirstThenSecond.ID() == se.SecondThenFirst.ID() {
		t.Fatalf("reported results do not diverge: %+v", se)
	}
	if se.FirstThenSecond.GetBool(audit) == se.SecondThenFirst.GetBool(audit) {
		t.Fatalf("the orders should differ in the audit bit: %s vs %s", se.FirstThenSecond, se.SecondThenFirst)
	}
	msg := err.Error()
	for _, want := range []string{`"swap"`, `"audit"`, `"dst"`, "src→dst", "C2", "may diverge",
		se.State.String(), se.Source, se.FirstThenSecond.String(), se.SecondThenFirst.String()} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}

	// Certify runs the same build, so no certificate is issued either.
	if _, cerr := f.Certify(); !errors.As(cerr, &se) {
		t.Fatalf("Certify: want *SameTargetOrderError, got %v", cerr)
	}
}

// TestC2_CounterexampleDivergesAtRuntime replays the Coq witness: from target state
// ((false, true), false) with the source closed, audit,swap ends with audit = true and
// swap,audit with audit = false, composing the target machine with the repair the way
// FedMachine.Apply does. The reported witness replays to the reported results too.
func TestC2_CounterexampleDivergesAtRuntime(t *testing.T) {
	f, dst, a, b, audit := c2Federation()
	_, _, err := f.Build()
	var se *SameTargetOrderError
	if !errors.As(err, &se) {
		t.Fatalf("want *SameTargetOrderError, got %v", err)
	}
	dm, _, berr := dst.Build()
	if berr != nil {
		t.Fatal(berr)
	}
	ow := func(s State) State { return s.SetBool(a, false) } // the only image
	run := func(s State, evs ...string) State {
		for _, ev := range evs {
			s = ow(dm.Apply(s, ev))
		}
		return s
	}

	s0 := dm.NewState().SetBool(a, false).SetBool(b, true).SetBool(audit, false)
	as, sa := run(s0, "audit", "swap"), run(s0, "swap", "audit")
	if !as.GetBool(audit) || as.GetBool(a) || as.GetBool(b) {
		t.Fatalf("audit,swap: want ((false, false), true), got %s", as)
	}
	if sa.GetBool(audit) || sa.GetBool(a) || sa.GetBool(b) {
		t.Fatalf("swap,audit: want ((false, false), false), got %s", sa)
	}

	w := State{packed: se.State.packed, vars: dm.vars}
	if got := run(w, se.First, se.Second); got.ID() != se.FirstThenSecond.ID() {
		t.Fatalf("FirstThenSecond does not replay: got %s, report %s", got, se.FirstThenSecond)
	}
	if got := run(w, se.Second, se.First); got.ID() != se.SecondThenFirst.ID() {
		t.Fatalf("SecondThenFirst does not replay: got %s, report %s", got, se.SecondThenFirst)
	}
}

// TestC2_UndeclaredPairNotChecked: C2 covers the pairs the target's own CC covers. With no
// pair declared independent (OnlyDeclaredPairs), neither the component nor the federation
// promises anything about the order of swap and audit, so the federation builds.
func TestC2_UndeclaredPairNotChecked(t *testing.T) {
	src, sa := closedSource("src")
	dst, a, _, _ := slotTarget("dst")
	dst.OnlyDeclaredPairs()
	f := NewFederation("slots")
	f.Morphism(src, dst).Shared(a).
		Map(func(s, d State) State { return d.SetBool(a, s.GetBool(sa)) }).Add()
	if _, _, err := f.Build(); err != nil {
		t.Fatalf("undeclared pair should not be checked: %v", err)
	}

	// Declaring the pair independent brings it back under C2.
	src2, sa2 := closedSource("src")
	dst2, a2, _, _ := slotTarget("dst")
	dst2.Independent("swap", "audit")
	g := NewFederation("slots")
	g.Morphism(src2, dst2).Shared(a2).
		Map(func(s, d State) State { return d.SetBool(a2, s.GetBool(sa2)) }).Add()
	var se *SameTargetOrderError
	if _, _, err := g.Build(); !errors.As(err, &se) {
		t.Fatalf("declared pair: want *SameTargetOrderError, got %v", err)
	}
}

// TestC2_Resolver: the same defect on a multi-source target is caught through its resolver,
// and the witness names every source.
func TestC2_Resolver(t *testing.T) {
	s1, a1 := closedSource("left")
	s2, a2 := closedSource("right")
	dst, a, _, _ := slotTarget("dst")
	f := NewFederation("slots")
	f.Morphism(s1, dst).Shared(a).Map(func(s, d State) State { return d }).Add()
	f.Morphism(s2, dst).Shared(a).Map(func(s, d State) State { return d }).Add()
	f.Resolve(dst, func(d State, src map[string]State) State {
		return d.SetBool(a, src["left"].GetBool(a1) || src["right"].GetBool(a2))
	})
	_, _, err := f.Build()
	var se *SameTargetOrderError
	if !errors.As(err, &se) {
		t.Fatalf("want *SameTargetOrderError, got %v", err)
	}
	if se.Morphism != `resolver for "dst"` || !strings.Contains(se.Source, "left:") || !strings.Contains(se.Source, "right:") {
		t.Fatalf("resolver witness should name the resolver and every source: %+v", se)
	}
}

// supplyFederation is supply_instance: the manufacturer's recall drives the supplier's
// listed_ok flag; the supplier's sell reads only local state and unlist WRITES the shared flag,
// which the repair then overwrites. C1 and C2 hold, although the stronger "commute without the
// final repair" form fails (unlist does not commute with re-projection on its own).
func supplyFederation() (*Federation, *Registry, *Registry, Var, Var) {
	mfr := NewRegistry("manufacturer")
	recalled := mfr.Bool("recalled")
	mfr.Event("recall").Writes(recalled).Apply(func(s State) State { return s.SetBool(recalled, true) }).Add()

	sup := NewRegistry("supplier")
	listed := sup.Bool("listed_ok")
	sold := sup.Int("sold", 0, 3)
	sup.Event("sell").Writes(sold).
		Apply(func(s State) State { return s.SetInt(sold, min(s.GetInt(sold)+1, 3)) }).Add()
	sup.Event("unlist").Writes(listed).Apply(func(s State) State { return s.SetBool(listed, false) }).Add()

	f := NewFederation("supply")
	f.Morphism(mfr, sup).Shared(listed).
		Map(func(src, dst State) State { return dst.SetBool(listed, !src.GetBool(recalled)) }).Add()
	return f, mfr, sup, listed, sold
}

// TestC2_SupplyInstance: supply_instance builds, its report lists C1 and C2, and every order
// of its events (supply_converges) reaches the same federated state.
func TestC2_SupplyInstance(t *testing.T) {
	f, mfr, sup, _, _ := supplyFederation()
	m, rep, err := f.Build()
	if err != nil {
		t.Fatalf("supply_instance rejected: %v", err)
	}
	joined := strings.Join(rep.Checks, "\n")
	for _, want := range []string{"cross-registry CC (C1)", "repaired CC (C2)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Checks missing %q:\n%s", want, joined)
		}
	}

	type ev struct {
		r    *Registry
		name string
	}
	evs := []ev{{mfr, "recall"}, {sup, "sell"}, {sup, "unlist"}, {sup, "sell"}}
	starts := []FedState{m.Normalize(m.NewState()), m.Apply(m.Normalize(m.NewState()), sup, "sell")}
	for _, s0 := range starts {
		var want string
		var perm func(k int)
		perm = func(k int) {
			if k == len(evs) {
				s := s0
				for _, e := range evs {
					s = m.Apply(s, e.r, e.name)
				}
				got := m.Of(s, mfr).String() + " " + m.Of(s, sup).String()
				if want == "" {
					want = got
				} else if got != want {
					t.Fatalf("orders diverge from %v: %s vs %s", s0, got, want)
				}
				return
			}
			for i := k; i < len(evs); i++ {
				evs[k], evs[i] = evs[i], evs[k]
				perm(k + 1)
				evs[k], evs[i] = evs[i], evs[k]
			}
		}
		perm(0)
	}
}
