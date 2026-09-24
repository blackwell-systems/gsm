package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestSynthesize_FindsRepair: a registry whose invariants + events admit a convergent
// compensation. Synthesize discovers one (no Repair provided), and the resulting Machine
// actually converges.
func TestSynthesize_FindsRepair(t *testing.T) {
	r := gsm.NewRegistry("approval")
	stage := r.Enum("stage", "pending", "approved", "held")
	balance := r.Int("balance", 0, 2)
	r.Invariant("funded_if_approved").Watches(stage, balance).
		Holds(func(s gsm.State) bool { return s.Get(stage) != "approved" || s.GetInt(balance) >= 2 }).
		Add() // NO Repair — that's what we synthesize
	r.Event("approve").Writes(stage).
		Apply(func(s gsm.State) gsm.State { return s.Set(stage, "approved") }).Add()
	r.Event("credit").Writes(stage, balance).
		Apply(func(s gsm.State) gsm.State {
			b := s.GetInt(balance)
			if b < 2 {
				b++
			}
			if s.Get(stage) == "held" && b >= 2 {
				return s.Set(stage, "approved").SetInt(balance, b)
			}
			return s.SetInt(balance, b)
		}).Add()

	syn, err := r.Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if !syn.Convergent {
		t.Fatalf("expected a convergent compensation to exist:\n%s", syn)
	}
	m := syn.Machine()
	if m == nil {
		t.Fatal("Machine() returned nil for a convergent synthesis")
	}
	s0 := m.NewState()
	a := m.Apply(m.Apply(s0, "approve"), "credit")
	b := m.Apply(m.Apply(s0, "credit"), "approve")
	if a.ID() != b.ID() {
		t.Fatalf("synthesized machine not order-independent: %s vs %s", a, b)
	}
	if !m.IsValid(a) {
		t.Fatalf("synthesized machine reached an invalid state: %s", a)
	}
	if len(syn.Repairs()) == 0 {
		t.Fatal("Repairs() empty for a convergent synthesis")
	}
	if !strings.Contains(syn.String(), "CONVERGENT") {
		t.Fatalf("report should say CONVERGENT:\n%s", syn)
	}
}

// TestSynthesize_ProvesImpossible: two events set the same variable to different constants.
// No compensation can reconcile last-writer-wins, so Synthesize exhausts the search and
// reports provable impossibility.
func TestSynthesize_ProvesImpossible(t *testing.T) {
	r := gsm.NewRegistry("conflict")
	x := r.Int("x", 0, 2)
	r.Event("set1").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()
	r.Event("set2").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 2) }).Add()

	syn, err := r.Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if syn.Convergent {
		t.Fatalf("expected impossibility, but found a convergent compensation:\n%s", syn)
	}
	if !syn.Exhaustive {
		t.Fatal("impossibility must be from an exhaustive search")
	}
	if !strings.Contains(syn.String(), "IMPOSSIBLE") {
		t.Fatalf("report should state impossibility:\n%s", syn)
	}
	// The impossibility should come with an actionable witness (the unreconcilable pair).
	if syn.Witness() == "" || !strings.Contains(syn.Witness(), "set1") || !strings.Contains(syn.Witness(), "set2") {
		t.Fatalf("expected a witness naming the conflicting events, got: %q", syn.Witness())
	}
}

// TestSynthesize_Scales: a registry whose repair-assignment space (8 invalid × 8 valid =
// 8^8 ≈ 16.7M) EXCEEDS the old brute-force cap (2^20 ≈ 1M) — the previous implementation
// would have refused it. Backtracking with forward-checking solves it (a per-variable clamp
// repair is convergent) within budget.
func TestSynthesize_Scales(t *testing.T) {
	r := gsm.NewRegistry("clamp")
	x := r.Int("x", 0, 3)
	y := r.Bool("y")
	z := r.Bool("z")
	r.Invariant("x_le_1").Watches(x).
		Holds(func(s gsm.State) bool { return s.GetInt(x) <= 1 }).Add() // no Repair
	r.Event("incx").Writes(x).Apply(func(s gsm.State) gsm.State {
		v := s.GetInt(x)
		if v < 3 {
			v++
		}
		return s.SetInt(x, v)
	}).Add()
	r.Event("flipy").Writes(y).Apply(func(s gsm.State) gsm.State { return s.SetBool(y, !s.GetBool(y)) }).Add()
	r.Event("flipz").Writes(z).Apply(func(s gsm.State) gsm.State { return s.SetBool(z, !s.GetBool(z)) }).Add()

	syn, err := r.Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if !syn.Convergent {
		t.Fatalf("expected a convergent compensation (per-variable clamp):\n%s", syn)
	}
	t.Logf("solved a 8^8 ≈ 16.7M-assignment problem in %d backtracking nodes", syn.Nodes)
	// Minimal-change preference: each invalid state (x∈{2,3}) should repair by changing ONLY
	// x (a clamp), leaving y and z untouched — the sensible repair, not an arbitrary one.
	for _, rp := range syn.Repairs() {
		from, to := rp[0], rp[1]
		if from.GetBool(y) != to.GetBool(y) || from.GetBool(z) != to.GetBool(z) {
			t.Fatalf("non-minimal repair: %s → %s changed y or z (expected x-only clamp)", from, to)
		}
		if to.GetInt(x) > 1 {
			t.Fatalf("repair %s → %s did not restore x≤1", from, to)
		}
	}
	m := syn.Machine()
	// spot-check order-independence of two independent events through the synthesized machine.
	s0 := m.Apply(m.NewState(), "incx") // x=1 (valid); drive x to 2 to exercise repair
	s0 = m.Apply(s0, "incx")            // x would be 2 → repaired
	if !m.IsValid(s0) {
		t.Fatalf("synthesized machine left an invalid state: %s", s0)
	}
}

// TestSynthesize_PreferenceOrdering: when several repairs converge, Prefer steers which one
// synthesis returns. Here both ok1 and ok2 are convergent repairs for `broken`; the default
// (minimal-change, ties by index) picks ok1, and a preference for ok2 flips it.
func TestSynthesize_PreferenceOrdering(t *testing.T) {
	r := gsm.NewRegistry("twochoice")
	st := r.Enum("state", "ok1", "ok2", "broken")
	r.Invariant("not_broken").Watches(st).
		Holds(func(s gsm.State) bool { return s.Get(st) != "broken" }).Add()
	r.Event("noop").Apply(func(s gsm.State) gsm.State { return s }).Add()

	target := func(syn *gsm.Synthesis) string {
		for _, rp := range syn.Repairs() {
			if rp[0].Get(st) == "broken" {
				return rp[1].Get(st)
			}
		}
		return ""
	}

	def, err := r.Synthesize()
	if err != nil || !def.Convergent {
		t.Fatalf("default synthesis failed: %v\n%s", err, def)
	}
	if got := target(def); got != "ok1" {
		t.Fatalf("default repair broken → %s, want ok1 (minimal-change tie by index)", got)
	}

	pref, err := r.SynthesizeWith(gsm.Prefer(func(from, to gsm.State) int {
		if to.Get(st) == "ok2" {
			return 0 // most preferred
		}
		return 1
	}))
	if err != nil || !pref.Convergent {
		t.Fatalf("preference synthesis failed: %v\n%s", err, pref)
	}
	if got := target(pref); got != "ok2" {
		t.Fatalf("preferred repair broken → %s, want ok2", got)
	}
}

// TestSynthesize_Optimal: branch-and-bound returns the provably minimum-cost convergent
// repair. It must minimize the cost (flip with the cost function, not just follow order),
// report proven optimality (Exhaustive), and never cost more than the first-found repair.
func TestSynthesize_Optimal(t *testing.T) {
	build := func() (*gsm.Registry, gsm.Var) {
		r := gsm.NewRegistry("twochoice")
		st := r.Enum("state", "ok1", "ok2", "broken")
		r.Invariant("nb").Watches(st).Holds(func(s gsm.State) bool { return s.Get(st) != "broken" }).Add()
		r.Event("noop").Apply(func(s gsm.State) gsm.State { return s }).Add()
		return r, st
	}
	target := func(syn *gsm.Synthesis, st gsm.Var) string {
		for _, rp := range syn.Repairs() {
			if rp[0].Get(st) == "broken" {
				return rp[1].Get(st)
			}
		}
		return ""
	}

	// Cost favors ok2 → optimal picks ok2.
	r, st := build()
	o, err := r.SynthesizeWith(gsm.Prefer(func(_, to gsm.State) int {
		if to.Get(st) == "ok2" {
			return 1
		}
		return 3
	}), gsm.Optimal())
	if err != nil || !o.Convergent || !o.Exhaustive {
		t.Fatalf("expected a proven-optimal convergent repair: %v\n%s", err, o)
	}
	if target(o, st) != "ok2" || o.Cost != 1 {
		t.Fatalf("optimal repair = %s (cost %d), want ok2 (cost 1)", target(o, st), o.Cost)
	}

	// Flip the cost → optimal must flip to ok1 (proves it minimizes cost, not ordering).
	r2, st2 := build()
	o2, _ := r2.SynthesizeWith(gsm.Prefer(func(_, to gsm.State) int {
		if to.Get(st2) == "ok1" {
			return 1
		}
		return 3
	}), gsm.Optimal())
	if target(o2, st2) != "ok1" || o2.Cost != 1 {
		t.Fatalf("optimal repair = %s (cost %d), want ok1 (cost 1)", target(o2, st2), o2.Cost)
	}

	// Guarantee on a bigger registry (the clamp): optimal cost ≤ first-found cost, proven.
	rc := gsm.NewRegistry("clamp")
	x := rc.Int("x", 0, 3)
	y := rc.Bool("y")
	z := rc.Bool("z")
	rc.Invariant("x_le_1").Watches(x).Holds(func(s gsm.State) bool { return s.GetInt(x) <= 1 }).Add()
	rc.Event("incx").Writes(x).Apply(func(s gsm.State) gsm.State {
		v := s.GetInt(x)
		if v < 3 {
			v++
		}
		return s.SetInt(x, v)
	}).Add()
	rc.Event("flipy").Writes(y).Apply(func(s gsm.State) gsm.State { return s.SetBool(y, !s.GetBool(y)) }).Add()
	rc.Event("flipz").Writes(z).Apply(func(s gsm.State) gsm.State { return s.SetBool(z, !s.GetBool(z)) }).Add()

	def, _ := rc.Synthesize()
	opt, _ := rc.SynthesizeWith(gsm.Optimal())
	if !opt.Exhaustive {
		t.Fatal("clamp optimal search should complete (proven optimal)")
	}
	if opt.Cost > def.Cost {
		t.Fatalf("optimal cost %d exceeds first-found %d — optimal must be ≤", opt.Cost, def.Cost)
	}
	t.Logf("clamp: first-found cost=%d, optimal cost=%d (%d nodes)", def.Cost, opt.Cost, opt.Nodes)
}

// TestBuild_MissingRepairErrors: an invariant without a Repair is fine for Synthesize but
// Build must refuse it with a clear message (not panic).
func TestBuild_MissingRepairErrors(t *testing.T) {
	r := gsm.NewRegistry("no-repair")
	flag := r.Bool("flag")
	r.Invariant("flag_off").Watches(flag).Holds(func(s gsm.State) bool { return !s.GetBool(flag) }).Add()
	r.Event("raise").Writes(flag).Apply(func(s gsm.State) gsm.State { return s.SetBool(flag, true) }).Add()

	if _, _, err := r.Build(); err == nil || !strings.Contains(err.Error(), "Synthesize") {
		t.Fatalf("expected a missing-Repair error pointing to Synthesize, got: %v", err)
	}
}
