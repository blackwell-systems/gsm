package gsm

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// withinDeadline runs fn and fails the test if it does not return in d, so a
// regression to a hang fails instead of stalling the suite.
func withinDeadline(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not return within %v", d)
	}
}

// tripleGuard is the audit's machine: "fire" declares Writes(x) but its closure
// guard reads a, b and c jointly. No single or pair change from the zero background
// flips the guard, so the perturbation test cannot see the dependence.
func tripleGuard() *Registry {
	r := NewRegistry("triple_guard")
	a, b, c := r.Bool("a"), r.Bool("b"), r.Bool("c")
	x := r.Bool("x")
	r.Event("setA").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
	r.Event("setB").Writes(b).Apply(func(s State) State { return s.SetBool(b, true) }).Add()
	r.Event("setC").Writes(c).Apply(func(s State) State { return s.SetBool(c, true) }).Add()
	r.Event("fire").Writes(x).
		Guard(func(s State) bool { return !(s.GetBool(a) && s.GetBool(b) && s.GetBool(c)) }).
		Apply(func(s State) State { return s.SetBool(x, true) }).Add()
	return r
}

// TestCompositional_ClosuresNeedOptIn: by default BuildCompositional refuses closure
// rules, whose footprint it can only test, and says what to do instead.
func TestCompositional_ClosuresNeedOptIn(t *testing.T) {
	m, rep, err := tripleGuard().BuildCompositional()
	if err == nil || m != nil {
		t.Fatalf("BuildCompositional certified closure rules without TrustClosureFootprints:\n%s", rep)
	}
	for _, want := range []string{`event "setA" is a Go closure`, "TrustClosureFootprints", "combinators", "Build"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if _, _, berr := tripleGuard().Build(); berr == nil {
		t.Fatal("Build certified the triple-guard machine; the test no longer shows the gap")
	}
}

// TestCompositional_ClosureInvariantNeedsOptIn: one closure rule is enough, here an
// invariant among combinator events.
func TestCompositional_ClosureInvariantNeedsOptIn(t *testing.T) {
	r := NewRegistry("mixed")
	a := r.Int("a", 0, 3)
	r.DeclEvent("inc", Do(Set(a, Add(V(a), Lit(1)))))
	r.Invariant("cap").Watches(a).
		Holds(func(s State) bool { return s.GetInt(a) <= 2 }).
		Repair(func(s State) State { return s.SetInt(a, 2) }).Add()
	_, _, err := r.BuildCompositional()
	if err == nil || !strings.Contains(err.Error(), `invariant "cap" is a Go closure`) {
		t.Fatalf("want the closure invariant named, got %v", err)
	}
	if _, rep, err := r.BuildCompositional(TrustClosureFootprints()); err != nil || rep.Assurance != AssuranceOracleComponentsTested {
		t.Fatalf("with the opt-in: err %v, report:\n%s", err, rep)
	}
}

// TestCompositional_OptInReportsTestedAssurance: with the opt-in the triple-guard
// machine is accepted (the perturbation gap is real), and the report says the
// footprint check for closures is a test, not an exact check.
func TestCompositional_OptInReportsTestedAssurance(t *testing.T) {
	m, rep, err := tripleGuard().BuildCompositional(TrustClosureFootprints())
	if err != nil || m == nil {
		t.Fatalf("BuildCompositional(TrustClosureFootprints()): %v", err)
	}
	if rep.Assurance != AssuranceOracleComponentsTested {
		t.Fatalf("Assurance = %v, want %v", rep.Assurance, AssuranceOracleComponentsTested)
	}
	if !strings.Contains(rep.String(), "Assurance: component tables certified by the verified table oracle; "+
		"cross-component independence by gsm's footprint check, which for closure rules is a perturbation test, not exact") {
		t.Fatalf("report does not state the closure footprint check is a test:\n%s", rep)
	}
}

// TestCompositional_CombinatorsNeedNoOptIn: combinator rules are checked exactly, so
// the default path accepts them and reports the exact assurance.
func TestCompositional_CombinatorsNeedNoOptIn(t *testing.T) {
	r := NewRegistry("combinators")
	for k := 0; k < 4; k++ {
		v := r.Int(fmt.Sprintf("v%d", k), 0, 7)
		r.DeclInvariant(fmt.Sprintf("cap%d", k), Le(V(v), Lit(5)), Do(Set(v, Lit(5))))
		r.DeclEvent(fmt.Sprintf("inc%d", k), Do(Set(v, Add(V(v), Lit(1)))))
	}
	m, rep, err := r.BuildCompositional()
	if err != nil || m == nil {
		t.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	if rep.Assurance != AssuranceOracleComponents {
		t.Fatalf("Assurance = %v, want %v", rep.Assurance, AssuranceOracleComponents)
	}
}

// TestLazyNormalize_RepairCycleIsBounded: two closure invariants on x that fight
// only when a, b and c are all set. Every one- and two-variable perturbation from
// the zero background leaves them satisfied, so BuildCompositional (with the
// opt-in) accepts the machine. At runtime the repairs cycle; Apply must panic with
// an explanation once the verified repair bound is exceeded, not loop forever.
func TestLazyNormalize_RepairCycleIsBounded(t *testing.T) {
	r := NewRegistry("cycle")
	a, b, c := r.Bool("a"), r.Bool("b"), r.Bool("c")
	x := r.Bool("x")
	y := r.Int("y", 0, 3) // a separate component whose repair chain has depth 1
	all := func(s State) bool { return s.GetBool(a) && s.GetBool(b) && s.GetBool(c) }
	r.Invariant("x_on").Watches(x).
		Holds(func(s State) bool { return !all(s) || s.GetBool(x) }).
		Repair(func(s State) State { return s.SetBool(x, true) }).Add()
	r.Invariant("x_off").Watches(x).
		Holds(func(s State) bool { return !all(s) || !s.GetBool(x) }).
		Repair(func(s State) State { return s.SetBool(x, false) }).Add()
	r.Invariant("y_cap").Watches(y).
		Holds(func(s State) bool { return s.GetInt(y) <= 2 }).
		Repair(func(s State) State { return s.SetInt(y, 2) }).Add()
	r.Event("setA").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
	r.Event("setB").Writes(b).Apply(func(s State) State { return s.SetBool(b, true) }).Add()
	r.Event("setC").Writes(c).Apply(func(s State) State { return s.SetBool(c, true) }).Add()
	r.Event("incY").Writes(y).Apply(func(s State) State { return s.SetInt(y, s.GetInt(y)+1) }).Add()

	m, rep, err := r.BuildCompositional(TrustClosureFootprints())
	if err != nil {
		t.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	if m.repairBound != 1 {
		t.Fatalf("repairBound = %d, want 1 (the y component's chain; x's invariants never fired in verification)", m.repairBound)
	}

	// The verified repairs still run within the bound.
	s := m.NewState()
	for i := 0; i < 4; i++ {
		s = m.Apply(s, "incY")
	}
	if s.GetInt(y) != 2 {
		t.Fatalf("y = %d after four incY, want 2", s.GetInt(y))
	}

	var msg string
	withinDeadline(t, 10*time.Second, func() {
		defer func() { msg = fmt.Sprint(recover()) }()
		s := m.Apply(m.Apply(m.NewState(), "setA"), "setB")
		m.Apply(s, "setC")
	})
	for _, want := range []string{"did not reach a valid state within 1 steps", "may cycle", `last repaired: invariant "x_`} {
		if !strings.Contains(msg, want) {
			t.Errorf("panic %q does not mention %q", msg, want)
		}
	}
}

// TestCompositional_OverflowedDomainFailsFast: Int(0, math.MaxInt) overflows its
// domain to a negative int. BuildCompositional used to loop for 2^63 perturbations;
// it must return an error naming the variable at once.
func TestCompositional_OverflowedDomainFailsFast(t *testing.T) {
	r := NewRegistry("wide")
	declared := func() (ok bool) {
		defer func() { ok = recover() == nil }()
		r.Int("ts", 0, math.MaxInt)
		return true
	}()
	if !declared {
		t.Skip("Int rejects the overflowing range at declaration")
	}
	flag := r.Bool("flag")
	r.Event("set").Writes(flag).Apply(func(s State) State { return s.SetBool(flag, true) }).Add()
	var err error
	withinDeadline(t, 5*time.Second, func() { _, _, err = r.BuildCompositional(TrustClosureFootprints()) })
	if err == nil || !strings.Contains(err.Error(), `variable "ts" has an invalid domain`) {
		t.Fatalf("want an invalid-domain error naming ts, got %v", err)
	}
}

// TestCompositional_PerturbationCostFailsFast: two wide variables, each small enough
// to be a component, make the pairwise perturbation of a closure elsewhere about
// 2^36 calls. BuildCompositional must refuse up front, not run for hours.
func TestCompositional_PerturbationCostFailsFast(t *testing.T) {
	r := NewRegistry("wide_pairs")
	flag := r.Bool("flag")
	u := r.Int("u", 0, 1<<18-1)
	v := r.Int("v", 0, 1<<18-1)
	r.Event("set").Writes(flag).Apply(func(s State) State { return s.SetBool(flag, true) }).Add()
	r.DeclEvent("incU", Do(Set(u, Add(V(u), Lit(1)))))
	r.DeclEvent("incV", Do(Set(v, Add(V(v), Lit(1)))))
	var err error
	withinDeadline(t, 5*time.Second, func() { _, _, err = r.BuildCompositional(TrustClosureFootprints()) })
	if err == nil || !strings.Contains(err.Error(), `closure event "set" would take about`) {
		t.Fatalf("want a perturbation-cost error for \"set\", got %v", err)
	}
}
