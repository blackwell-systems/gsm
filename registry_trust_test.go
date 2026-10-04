package gsm

import (
	"math"
	"strings"
	"sync"
	"testing"
)

// accountRegistry is the declared-only probe: deposit is guarded on open and close
// clears open, so (close, deposit) does not commute. notify is independent of all.
func accountRegistry() *Registry {
	r := NewRegistry("acct")
	open := r.Bool("open")
	bal := r.Int("bal", 0, 3)
	note := r.Bool("notified")
	r.Event("close").Writes(open).Apply(func(s State) State { return s.SetBool(open, false) }).Add()
	r.Event("open").Writes(open).Apply(func(s State) State { return s.SetBool(open, true) }).Add()
	r.Event("deposit").Writes(bal).
		Guard(func(s State) bool { return s.GetBool(open) }).
		Apply(func(s State) State { return s.SetInt(bal, s.GetInt(bal)+1) }).Add()
	r.Event("notify").Writes(note).Apply(func(s State) State { return s.SetBool(note, true) }).Add()
	return r
}

// TestIndependent_UndeclaredNonCommutingPairsReported: one Independent call used to
// drop every other pair from checking while the report still said GUARANTEED. Build
// still accepts the machine (the declared pair commutes), but now checks every
// undeclared pair and lists each one that does not commute as a causal-delivery
// obligation, and the convergence line is qualified by it.
func TestIndependent_UndeclaredNonCommutingPairsReported(t *testing.T) {
	r := accountRegistry()
	if _, _, err := r.Build(); err == nil {
		t.Fatal("all-pairs Build should reject (deposit, close)")
	}
	r.Independent("deposit", "notify")
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("declared-only Build: %v\n%s", err, rep)
	}
	if rep.PairsTotal != 1 || rep.PairsUndeclared != 5 {
		t.Fatalf("PairsTotal=%d PairsUndeclared=%d, want 1 and 5", rep.PairsTotal, rep.PairsUndeclared)
	}
	got := map[[2]string]bool{}
	for _, f := range rep.CausalOrderRequired {
		got[[2]string{f.Event1, f.Event2}] = true
		// The witness is real: the two orders differ from the recorded state.
		a := m.Apply(m.Apply(f.State, f.Event1), f.Event2)
		b := m.Apply(m.Apply(f.State, f.Event2), f.Event1)
		if a.ID() == b.ID() || a.ID() != f.Result1.ID() || b.ID() != f.Result2.ID() {
			t.Fatalf("witness for (%s, %s) does not reproduce: %v %v vs %v %v", f.Event1, f.Event2, a, b, f.Result1, f.Result2)
		}
	}
	want := [][2]string{{"close", "open"}, {"close", "deposit"}, {"open", "deposit"}}
	if len(got) != len(want) {
		t.Fatalf("CausalOrderRequired = %v, want %v", rep.CausalOrderRequired, want)
	}
	for _, p := range want {
		if !got[p] {
			t.Fatalf("CausalOrderRequired misses %v: %v", p, rep.CausalOrderRequired)
		}
	}
	s := rep.String()
	for _, frag := range []string{
		"Undeclared pairs: 5 checked, 3 do not commute: each must be causally ordered (not independent)",
		"(close, deposit) from ",
		"Convergence: GUARANTEED under causal delivery of the 3 undeclared pair(s) above",
	} {
		if !strings.Contains(s, frag) {
			t.Fatalf("report lacks %q:\n%s", frag, s)
		}
	}
	if strings.Contains(s, "Convergence: GUARANTEED\n") {
		t.Fatalf("report claims unqualified convergence:\n%s", s)
	}
}

// TestIndependent_AllUndeclaredCommute: when every undeclared pair commutes the
// declarations cost nothing, and the report says so without qualification.
func TestIndependent_AllUndeclaredCommute(t *testing.T) {
	r := NewRegistry("flags")
	a, b, c := r.Bool("a"), r.Bool("b"), r.Bool("c")
	r.DeclEvent("ra", Raise(a))
	r.DeclEvent("rb", Raise(b))
	r.DeclEvent("rc", Raise(c))
	r.Independent("ra", "rb")
	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.PairsUndeclared != 2 || len(rep.CausalOrderRequired) != 0 {
		t.Fatalf("PairsUndeclared=%d CausalOrderRequired=%v", rep.PairsUndeclared, rep.CausalOrderRequired)
	}
	s := rep.String()
	if !strings.Contains(s, "Undeclared pairs: 2 checked, all commute (order free)") ||
		!strings.Contains(s, "Convergence: GUARANTEED\n") {
		t.Fatalf("report:\n%s", s)
	}
}

// TestIndependent_DeclaredPairMustCommute: a declared independent pair that does not
// commute is a build error with a counterexample, not an obligation.
func TestIndependent_DeclaredPairMustCommute(t *testing.T) {
	r := accountRegistry()
	r.Independent("deposit", "close")
	m, rep, err := r.Build()
	if err == nil || m != nil {
		t.Fatal("Build should reject a declared pair that does not commute")
	}
	if rep.CC || rep.CCFailure == nil || rep.CCFailure.Event1 != "close" || rep.CCFailure.Event2 != "deposit" {
		t.Fatalf("CCFailure = %+v", rep.CCFailure)
	}
	if len(rep.CausalOrderRequired) != 0 {
		t.Fatalf("a failed build lists obligations: %v", rep.CausalOrderRequired)
	}
}

// invalidStartRegistry: repairing bad copies a into r, so applying e1 before or after
// the repair of a hand-built bad start used to give different r.
func invalidStartRegistry() (*Registry, Var) {
	r := NewRegistry("m")
	a := r.Bool("a")
	b := r.Bool("b")
	bad := r.Bool("bad")
	rr := r.Bool("r")
	r.Invariant("no_bad").Watches(bad, rr, a).
		Holds(func(s State) bool { return !s.GetBool(bad) }).
		Repair(func(s State) State { return s.SetBool(bad, false).SetBool(rr, s.GetBool(a)) }).Add()
	r.Event("e1").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
	r.Event("e2").Writes(b).Apply(func(s State) State { return s.SetBool(b, true) }).Add()
	return r, bad
}

// TestApply_NormalizesInvalidStart: Apply normalizes an input that violates an
// invariant before applying the event, so both orders agree from a state built by
// hand (restored from storage, say), on table and lazy machines alike.
func TestApply_NormalizesInvalidStart(t *testing.T) {
	r, bad := invalidStartRegistry()
	tm, _, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	r2, bad2 := invalidStartRegistry()
	lm, _, err := r2.BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional: %v", err)
	}
	for name, c := range map[string]struct {
		m   *Machine
		bad Var
	}{"table": {tm, bad}, "lazy": {lm, bad2}} {
		start := c.m.NewState().SetBool(c.bad, true)
		if c.m.IsValid(start) {
			t.Fatalf("%s: start should be invalid", name)
		}
		x := c.m.Apply(c.m.Apply(start, "e1"), "e2")
		y := c.m.Apply(c.m.Apply(start, "e2"), "e1")
		if x.ID() != y.ID() {
			t.Fatalf("%s: orders disagree from an invalid start: %v vs %v", name, x, y)
		}
		n := c.m.Normalize(start)
		if want := c.m.Apply(n, "e1"); c.m.Apply(start, "e1").ID() != want.ID() {
			t.Fatalf("%s: Apply(s, e) != Apply(Normalize(s), e)", name)
		}
	}
}

// TestInt_RangeOverflowRejected: a range with more values than an int can count is
// rejected at the declaration, naming the variable, instead of passing the Go checks
// and failing in the oracle.
func TestInt_RangeOverflowRejected(t *testing.T) {
	for _, c := range []struct{ min, max int }{
		{0, math.MaxInt},
		{math.MinInt, 0},
		{-1, math.MaxInt},
		{math.MinInt, math.MaxInt},
	} {
		func() {
			defer func() {
				p := recover()
				if p == nil || !strings.Contains(p.(string), `int "ts" range`) || !strings.Contains(p.(string), "too wide") {
					t.Fatalf("Int(%d, %d): panic = %v", c.min, c.max, p)
				}
			}()
			NewRegistry("wide").Int("ts", c.min, c.max)
		}()
	}
	// The widest range that fits is accepted at declaration, and Build explains the size.
	r := NewRegistry("wide")
	r.Int("ts", 0, math.MaxInt-1)
	if _, _, err := r.Build(); err == nil || !strings.Contains(err.Error(), "state space too large") {
		t.Fatalf("Build: %v", err)
	}
}

// TestSaturation_Reported: a write clamped into a variable's range is reported per
// rule (closure SetInt, combinator Inc, combinator enum Set), without changing the
// clamping semantics.
func TestSaturation_Reported(t *testing.T) {
	// Closure probe: the overdraft flag never sets because deposit saturates at 3.
	r := NewRegistry("cap")
	bal := r.Int("bal", 0, 3)
	over := r.Bool("overdraft_flag")
	r.Invariant("cap").Watches(bal, over).
		Holds(func(s State) bool { return s.GetInt(bal) <= 3 || s.GetBool(over) }).
		Repair(func(s State) State { return s.SetBool(over, true) }).Add()
	r.Event("deposit").Writes(bal).Apply(func(s State) State { return s.SetInt(bal, s.GetInt(bal)+1) }).Add()
	r.Event("flag").Writes(over).Apply(func(s State) State { return s.SetBool(over, true) }).Add()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(rep.Saturations) != 1 || rep.Saturations[0].Rule != `event "deposit"` || rep.Saturations[0].Var != "bal" ||
		rep.Saturations[0].States != 2 {
		t.Fatalf("Saturations = %+v", rep.Saturations)
	}
	if !strings.Contains(rep.String(), `Saturation: event "deposit" clamps "bal" into its range on 2 state(s)`) {
		t.Fatalf("report:\n%s", rep)
	}
	s := m.NewState()
	for i := 0; i < 6; i++ {
		s = m.Apply(s, "deposit")
	}
	if got := s.GetInt(bal); got != 3 {
		t.Fatalf("clamping semantics changed: bal = %d", got)
	}

	// Combinator rules: Inc on an Int and an enum step past the last label.
	c := NewRegistry("comb")
	n := c.Int("n", 0, 1)
	st := c.Enum("st", "a", "b")
	c.DeclEvent("inc", Inc(n))
	c.DeclEvent("next", Do(Set(st, Add(V(st), Lit(1)))))
	_, crep, err := c.Build()
	if err != nil {
		t.Fatalf("Build comb: %v", err)
	}
	got := map[string]bool{}
	for _, sat := range crep.Saturations {
		got[sat.Rule+"/"+sat.Var] = true
	}
	if !got[`event "inc"/n`] || !got[`event "next"/st`] || len(got) != 2 {
		t.Fatalf("combinator Saturations = %+v", crep.Saturations)
	}

	// A machine that never clamps reports nothing.
	_, frep, err := accountRegistryNoClamp().Build()
	if err != nil || len(frep.Saturations) != 0 {
		t.Fatalf("err=%v Saturations=%v", err, frep.Saturations)
	}
}

func accountRegistryNoClamp() *Registry {
	r := NewRegistry("plain")
	a := r.Bool("a")
	r.Event("set").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
	return r
}

// TestSaturation_ConcurrentBuilds: each build attributes clamps to its own rules,
// even when builds run at once (run with -race).
func TestSaturation_ConcurrentBuilds(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, rep, err := accountRegistryNoClamp().Build()
			if err != nil || len(rep.Saturations) != 0 {
				errs <- "non-clamping build saw saturations"
			}
		}()
		go func() {
			defer wg.Done()
			r := NewRegistry("c")
			n := r.Int("n", 0, 2)
			r.DeclEvent("inc", Inc(n))
			_, rep, err := r.Build()
			if err != nil || len(rep.Saturations) != 1 || rep.Saturations[0].States != 1 {
				errs <- "clamping build lost or miscounted its saturation"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

// TestNotIdempotent_Reported: events whose double application differs from a single
// one are listed as needing exactly-once delivery; set-to-value events are not.
func TestNotIdempotent_Reported(t *testing.T) {
	r := NewRegistry("ctr")
	n := r.Int("n", 0, 3)
	done := r.Bool("done")
	r.DeclEvent("inc", Inc(n))
	r.DeclEvent("finish", Raise(done))
	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(rep.NotIdempotent) != 1 || rep.NotIdempotent[0] != "inc" {
		t.Fatalf("NotIdempotent = %v, want [inc]", rep.NotIdempotent)
	}
	if !strings.Contains(rep.String(), "Delivery: exactly once for inc (applying one twice differs from once)") {
		t.Fatalf("report:\n%s", rep)
	}
}

// TestBuildCoordinated_RecordsCoordinatedInputs: the removed edges are recorded on
// their target component's report as external inputs, and printed in the FedReport.
func TestBuildCoordinated_RecordsCoordinatedInputs(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
	plan := fed.CoordinationPlan()
	if len(plan) != 1 {
		t.Fatalf("plan = %v", plan)
	}
	_, rep, err := fed.BuildCoordinated(plan)
	if err != nil {
		t.Fatalf("BuildCoordinated: %v", err)
	}
	var found int
	for _, c := range rep.Components {
		for _, cp := range c.Coordinated {
			found++
			if c.Name != plan[0].Dst || cp.Src != plan[0].Src || cp.Dst != plan[0].Dst ||
				!sameNameSet(cp.Shared, plan[0].Shared) {
				t.Fatalf("component %s records %v, plan is %v", c.Name, cp, plan[0])
			}
		}
	}
	if found != 1 {
		t.Fatalf("recorded %d coordinated inputs, want 1", found)
	}
	if !strings.Contains(rep.String(), "Coordinated input: "+plan[0].String()+" is set by external coordination") {
		t.Fatalf("FedReport:\n%s", rep)
	}
	// Plain Build of an acyclic federation records nothing.
	_, rep2, err := NewFederation("chain").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep2.Components {
		if len(c.Coordinated) != 0 {
			t.Fatalf("plain Build recorded %v", c.Coordinated)
		}
	}
}

// TestForeignVar_WritesWatches: a Var of another registry passed to Writes or
// Watches is an error naming it on every build path, not an index panic.
func TestForeignVar_WritesWatches(t *testing.T) {
	other := NewRegistry("other")
	_ = other.Bool("o0")
	_ = other.Bool("o1")
	_ = other.Bool("o2")
	foreign := other.Bool("o3")
	sameIndex := other.Bool("o4")

	mk := func(useWatches bool) *Registry {
		r := NewRegistry("r")
		a := r.Bool("a")
		b := r.Bool("b")
		if useWatches {
			r.Invariant("inv").Watches(a, foreign).Holds(func(State) bool { return true }).
				Repair(func(s State) State { return s }).Add()
			r.Event("ea").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
		} else {
			r.Event("ea").Writes(foreign).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
		}
		r.Event("eb").Writes(b).Apply(func(s State) State { return s.SetBool(b, true) }).Add()
		return r
	}
	for _, watches := range []bool{false, true} {
		want := `event "ea" Writes variable "o3", which is not a variable of this registry`
		if watches {
			want = `invariant "inv" Watches variable "o3", which is not a variable of this registry`
		}
		for name, build := range map[string]func(*Registry) error{
			"Build":              func(r *Registry) error { _, _, err := r.Build(); return err },
			"BuildCompositional": func(r *Registry) error { _, _, err := r.BuildCompositional(); return err },
			"Synthesize":         func(r *Registry) error { _, err := r.Synthesize(); return err },
		} {
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("%s (watches=%v) panicked: %v", name, watches, p)
					}
				}()
				if err := build(mk(watches)); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("%s (watches=%v): err = %v, want %q", name, watches, err, want)
				}
			}()
		}
	}
	// A foreign Var whose index is in range here is caught by identity, not range.
	r := NewRegistry("r2")
	_ = r.Bool("x0")
	_ = r.Bool("x1")
	_ = r.Bool("x2")
	_ = r.Bool("x3")
	x4 := r.Bool("x4")
	r.Event("e").Writes(sameIndex).Apply(func(s State) State { return s.SetBool(x4, true) }).Add()
	if _, _, err := r.BuildCompositional(); err == nil || !strings.Contains(err.Error(), `"o4"`) {
		t.Fatalf("BuildCompositional: %v", err)
	}
}
