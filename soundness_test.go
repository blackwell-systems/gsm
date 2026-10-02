package gsm

import (
	"strings"
	"testing"
)

// Regression tests for the commute-shortcut soundness bug (CHANGELOG, Security).
//
// Build used to skip the Compensation Commutativity check for any event pair whose
// "footprints" were disjoint, where the footprint was computed only from the
// invariants an event's writes touch. That shortcut is sound only when each event
// reads nothing but its own footprint (the precondition of disjoint_events_commute
// in normalization-confluence coq/Gsm.v). Build never checked that precondition,
// so an event whose guard reads another event's write was certified convergent
// without being checked.

// payShip is the naive pay/ship machine: ship is guarded on paid, which pay writes.
// Applied in the order pay, ship it ends paid+shipped; in the order ship, pay the
// ship is a no-op and it ends paid only. It does not converge.
func payShip() *Registry {
	r := NewRegistry("pay_ship")
	paid := r.Bool("paid")
	shipped := r.Bool("shipped")
	r.Event("pay").Writes(paid).Apply(func(s State) State { return s.SetBool(paid, true) }).Add()
	r.Event("ship").Writes(shipped).
		Guard(func(s State) bool { return s.GetBool(paid) }).
		Apply(func(s State) State { return s.SetBool(shipped, true) }).Add()
	return r
}

// TestPayShip_NotCertified: Build must not certify the pay/ship machine, and its
// counterexample must name the pair and show the divergence.
func TestPayShip_NotCertified(t *testing.T) {
	m, rep, err := payShip().Build()
	if err == nil {
		t.Fatalf("Build certified the non-convergent pay/ship machine:\n%s", rep)
	}
	if m != nil {
		t.Fatal("Build returned a machine alongside an error")
	}
	if rep == nil || rep.CC || rep.CCFailure == nil {
		t.Fatalf("want a CC failure with a counterexample, got report:\n%s", rep)
	}
	f := rep.CCFailure
	pair := f.Event1 + "," + f.Event2
	if pair != "pay,ship" && pair != "ship,pay" {
		t.Fatalf("counterexample names (%s, %s), want (pay, ship)", f.Event1, f.Event2)
	}
	if f.Result1.packed == f.Result2.packed {
		t.Fatalf("counterexample does not diverge: %s vs %s", f.Result1, f.Result2)
	}
}

// TestPayShip_CompositionalRejects: BuildCompositional rejects pay/ship on the
// footprint check (ship's guard reads paid, outside its declared footprint).
func TestPayShip_CompositionalRejects(t *testing.T) {
	_, _, err := payShip().BuildCompositional()
	if err == nil {
		t.Fatal("BuildCompositional certified the non-convergent pay/ship machine")
	}
	if !strings.Contains(err.Error(), "reads variable \"paid\" outside its declared footprint") {
		t.Fatalf("want a footprint read violation naming paid, got: %v", err)
	}
}

// TestPayShip_Combinators: the same machine written in the combinator vocabulary.
// The event's write set is derived from its transform, so the guard's read of paid
// is outside it, exactly as with closures.
func TestPayShip_Combinators(t *testing.T) {
	r := NewRegistry("pay_ship_ast")
	paid := r.Bool("paid")
	shipped := r.Bool("shipped")
	r.DeclEvent("pay", Do(Set(paid, Lit(1))))
	r.DeclEventGuarded("ship", Eq(V(paid), Lit(1)), Do(Set(shipped, Lit(1))))
	if _, rep, err := r.Build(); err == nil {
		t.Fatalf("Build certified the combinator pay/ship machine:\n%s", rep)
	}
	if _, _, err := r.BuildCompositional(); err == nil {
		t.Fatal("BuildCompositional certified the combinator pay/ship machine")
	}
}

// writeWrite: two events write the same variable, and no invariant watches it.
// set_one then reset ends at 0; reset then set_one ends at 1.
func writeWrite() *Registry {
	r := NewRegistry("write_write")
	x := r.Int("x", 0, 3)
	r.Event("set_one").Writes(x).Apply(func(s State) State { return s.SetInt(x, 1) }).Add()
	r.Event("reset").Writes(x).Apply(func(s State) State { return s.SetInt(x, 0) }).Add()
	return r
}

// TestWriteWrite_NotCertified: the old shortcut ignored the write sets themselves
// when no invariant watched them, so two events overwriting the same variable were
// treated as disjoint. Both builders must reject it.
func TestWriteWrite_NotCertified(t *testing.T) {
	if _, rep, err := writeWrite().Build(); err == nil {
		t.Fatalf("Build certified two events overwriting the same variable:\n%s", rep)
	}
	_, rep, err := writeWrite().BuildCompositional()
	if err == nil {
		t.Fatalf("BuildCompositional certified two events overwriting the same variable:\n%s", rep)
	}
	if rep == nil || rep.CCFailure == nil {
		t.Fatalf("want a CC counterexample from BuildCompositional, got %v", err)
	}
}

// TestCompositionalReport_FootprintViolation pins the report BuildCompositional
// returns when it rejects on the footprint check: the violation is named as the
// cause, and WFC and CC are shown as not evaluated (the build stopped before
// either ran), never as "WFC: FAIL".
func TestCompositionalReport_FootprintViolation(t *testing.T) {
	_, rep, err := payShip().BuildCompositional()
	if err == nil {
		t.Fatal("BuildCompositional certified pay/ship")
	}
	if rep == nil {
		t.Fatal("no report returned with the footprint rejection")
	}
	wantCause := `gsm: event "ship" reads variable "paid" outside its declared footprint`
	if rep.FootprintViolation != wantCause || err.Error() != wantCause {
		t.Fatalf("FootprintViolation = %q, err = %q; want both %q", rep.FootprintViolation, err, wantCause)
	}
	if rep.WFC || rep.CC || rep.FootprintChecked || rep.CCFailure != nil {
		t.Fatalf("rejected report claims a verdict: WFC=%v CC=%v FootprintChecked=%v CCFailure=%v",
			rep.WFC, rep.CC, rep.FootprintChecked, rep.CCFailure)
	}
	want := "Machine: pay_ship\n" +
		"  Variables: 2\n" +
		"  Components: 2\n" +
		"  Events: 2\n" +
		"\n" +
		"  Footprint conformance: FAIL\n" +
		"    " + wantCause + "\n" +
		"  WFC: not evaluated (footprint violation)\n" +
		"  CC (Compensation Commutativity): not evaluated (footprint violation)\n"
	if got := rep.String(); got != want {
		t.Fatalf("report text:\n%s\nwant:\n%s", got, want)
	}
}

// TestCompositionalReport_Pass pins the success report's structure.
func TestCompositionalReport_Pass(t *testing.T) {
	_, rep, err := wideCounters(3).BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	want := "Machine: wide_counters_3\n" +
		"  Variables: 3\n" +
		"  Components: 3\n" +
		"  Events: 3\n" +
		"\n" +
		"  Footprint conformance: PASS (largest component: 4 states)\n" +
		"  WFC: PASS (max repair depth: 1)\n" +
		"  CC (Compensation Commutativity): PASS (3 pairs: 3 disjoint, 0 brute-force)\n" +
		"\n" +
		"  Convergence: GUARANTEED\n" +
		"  Assurance: component tables certified by the verified table oracle; cross-component independence by gsm's footprint check\n"
	if got := rep.String(); got != want {
		t.Fatalf("report text:\n%s\nwant:\n%s", got, want)
	}
}

// TestCompositional_TwoVariableGuard: a guard over two outside variables
// (`paid && inStock`) is masked by every single-variable change from the zero
// background, so footprint verification must also perturb pairs.
func TestCompositional_TwoVariableGuard(t *testing.T) {
	r := NewRegistry("two_var_guard")
	paid := r.Bool("paid")
	inStock := r.Bool("in_stock")
	shipped := r.Bool("shipped")
	r.Event("pay").Writes(paid).Apply(func(s State) State { return s.SetBool(paid, true) }).Add()
	r.Event("stock").Writes(inStock).Apply(func(s State) State { return s.SetBool(inStock, true) }).Add()
	r.Event("ship").Writes(shipped).
		Guard(func(s State) bool { return s.GetBool(paid) && s.GetBool(inStock) }).
		Apply(func(s State) State { return s.SetBool(shipped, true) }).Add()
	if _, _, err := r.Build(); err == nil {
		t.Fatal("Build certified a non-convergent machine")
	}
	_, rep, err := r.BuildCompositional()
	if err == nil {
		t.Fatal("BuildCompositional certified a guard over two outside variables")
	}
	if !strings.Contains(rep.FootprintViolation, `reads variable "paid" and "in_stock"`) {
		t.Fatalf("want a two-variable footprint violation, got %q", rep.FootprintViolation)
	}
}

// TestCompositional_CombinatorReadsExact: combinator events are checked
// syntactically, so a guard over any number of outside variables is caught
// (closures are checked by perturbation, which covers one or two variables).
func TestCompositional_CombinatorReadsExact(t *testing.T) {
	r := NewRegistry("three_var_guard")
	a, b, c := r.Bool("a"), r.Bool("b"), r.Bool("c")
	out := r.Bool("out")
	r.DeclEvent("set_a", Do(Set(a, Lit(1))))
	r.DeclEvent("set_b", Do(Set(b, Lit(1))))
	r.DeclEvent("set_c", Do(Set(c, Lit(1))))
	r.DeclEventGuarded("fire", And(Eq(V(a), Lit(1)), Eq(V(b), Lit(1)), Eq(V(c), Lit(1))), Do(Set(out, Lit(1))))
	_, rep, err := r.BuildCompositional()
	if err == nil {
		t.Fatal("BuildCompositional certified a guard over three outside variables")
	}
	if rep.FootprintViolation != `gsm: event "fire" reads variable "a" outside its declared footprint` {
		t.Fatalf("FootprintViolation = %q", rep.FootprintViolation)
	}
}

// TestCompositional_RejectsBeyond64Bits: State is one uint64, so variables past
// bit 64 are frozen at 0. BuildCompositional used to certify such a machine (an
// increment on the last counter had no effect); it must refuse it.
func TestCompositional_RejectsBeyond64Bits(t *testing.T) {
	if _, _, err := wideCounters(32).BuildCompositional(); err != nil {
		t.Fatalf("64-bit machine rejected: %v", err)
	}
	_, _, err := wideCounters(33).BuildCompositional()
	if err == nil || !strings.Contains(err.Error(), "66 bits") {
		t.Fatalf("want a 66-bit rejection, got %v", err)
	}
}
