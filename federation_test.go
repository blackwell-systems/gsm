package gsm

import "testing"

// buildManufacturerSupplier constructs the federation from the paper's §8.5 example:
// a manufacturer (authoritative source) and a supplier (target) linked by a total
// morphism that maps manufacturer status to supplier listing status.
func buildManufacturerSupplier(t *testing.T) (*FedMachine, *Registry, *Registry, Var, Var) {
	t.Helper()

	// Manufacturer RM: {draft, active, suspended}, all locally valid. epub: draft → active.
	mfr := NewRegistry("manufacturer")
	mstate := mfr.Enum("mstate", "draft", "active", "suspended")
	mfr.Event("epub").Writes(mstate).
		Guard(func(s State) bool { return s.Get(mstate) == "draft" }).
		Apply(func(s State) State { return s.Set(mstate, "active") }).Add()

	// Supplier RS: {idle, listed, stale, err}; err is invalid and repairs to idle.
	// eexp sets the listing to stale.
	sup := NewRegistry("supplier")
	sstate := sup.Enum("sstate", "idle", "listed", "stale", "err")
	sup.Invariant("no_err").Watches(sstate).
		Holds(func(s State) bool { return s.Get(sstate) != "err" }).
		Repair(func(s State) State { return s.Set(sstate, "idle") }).Add()
	sup.Event("eexp").Writes(sstate).
		Apply(func(s State) State { return s.Set(sstate, "stale") }).Add()

	// Total morphism ϕ: manufacturer status fixes the entire supplier state (shared = {sstate}).
	// draft↦idle, active↦listed, suspended↦stale. All images are valid, so M1 holds.
	image := map[string]string{"draft": "idle", "active": "listed", "suspended": "stale"}
	fed := NewFederation("mfr-sup").
		Morphism(mfr, sup).
		Shared(sstate).
		Map(func(srcNF, dst State) State { return dst.Set(sstate, image[srcNF.Get(mstate)]) }).
		Add()

	m, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("federation does not converge: %v\n%s", err, rep)
	}
	return m, mfr, sup, mstate, sstate
}

// TestFederation_PaperTrace reproduces the two convergence traces in §8.5 step by step and
// confirms both event orderings land on the same federally valid state (active, listed) —
// the supplier event's effect is overwritten because the manufacturer is authoritative.
func TestFederation_PaperTrace(t *testing.T) {
	m, mfr, sup, mstate, sstate := buildManufacturerSupplier(t)

	get := func(fs FedState) (string, string) {
		return m.Of(fs, mfr).Get(mstate), m.Of(fs, sup).Get(sstate)
	}

	start := m.NewState() // (draft, idle) — both at their zero value
	if ms, ss := get(start); ms != "draft" || ss != "idle" {
		t.Fatalf("start = (%s, %s), want (draft, idle)", ms, ss)
	}

	// Order 1 (epub first): (draft,idle) --epub--> (active,listed) --eexp--> (active,listed).
	o1 := m.Apply(start, mfr, "epub")
	if ms, ss := get(o1); ms != "active" || ss != "listed" {
		t.Fatalf("order1 after epub = (%s, %s), want (active, listed) — morphism repair should list", ms, ss)
	}
	o1 = m.Apply(o1, sup, "eexp")
	if ms, ss := get(o1); ms != "active" || ss != "listed" {
		t.Fatalf("order1 after eexp = (%s, %s), want (active, listed) — supplier event overwritten by authority", ms, ss)
	}

	// Order 2 (eexp first): (draft,idle) --eexp--> (draft,idle) --epub--> (active,listed).
	o2 := m.Apply(start, sup, "eexp")
	if ms, ss := get(o2); ms != "draft" || ss != "idle" {
		t.Fatalf("order2 after eexp = (%s, %s), want (draft, idle) — supplier event overwritten to idle", ms, ss)
	}
	o2 = m.Apply(o2, mfr, "epub")
	if ms, ss := get(o2); ms != "active" || ss != "listed" {
		t.Fatalf("order2 after epub = (%s, %s), want (active, listed)", ms, ss)
	}

	// Convergence: both orderings reach the same federally valid state.
	if o1.states[0].ID() != o2.states[0].ID() || o1.states[1].ID() != o2.states[1].ID() {
		m1s, s1s := get(o1)
		m2s, s2s := get(o2)
		t.Fatalf("orderings diverged: order1=(%s,%s) order2=(%s,%s)", m1s, s1s, m2s, s2s)
	}
	if !m.IsValid(o1) {
		t.Fatalf("converged state is not federally valid")
	}
}

// TestFederation_Authority checks the authority argument directly: whatever the supplier
// does locally, morphism repair re-derives its shared state from the manufacturer's normal
// form. A supplier event can never win against the source.
func TestFederation_Authority(t *testing.T) {
	m, mfr, sup, mstate, sstate := buildManufacturerSupplier(t)

	// Drive the manufacturer to active, then hammer the supplier with its event repeatedly.
	fs := m.Apply(m.NewState(), mfr, "epub")
	for i := 0; i < 5; i++ {
		fs = m.Apply(fs, sup, "eexp")
		if ss := m.Of(fs, sup).Get(sstate); ss != "listed" {
			t.Fatalf("iteration %d: supplier = %s, want listed (manufacturer is authoritative)", i, ss)
		}
	}
	if ms := m.Of(fs, mfr).Get(mstate); ms != "active" {
		t.Fatalf("manufacturer drifted to %s, want active (source is unaffected by target events)", ms)
	}
}

// TestFederation_CycleRejected confirms Build refuses a cyclic morphism network — the
// paper proves cyclic federations cannot converge (§8.4, Prop 8.13).
func TestFederation_CycleRejected(t *testing.T) {
	a := NewRegistry("A")
	av := a.Bool("shared_a")
	b := NewRegistry("B")
	bv := b.Bool("shared_b")

	fed := NewFederation("cyclic").
		Morphism(a, b).Shared(bv).
		Map(func(srcNF, dst State) State { return dst.SetBool(bv, srcNF.GetBool(av)) }).Add().
		Morphism(b, a).Shared(av).
		Map(func(srcNF, dst State) State { return dst.SetBool(av, srcNF.GetBool(bv)) }).Add()

	if _, _, err := fed.Build(); err == nil {
		t.Fatal("expected Build to reject a cyclic morphism network, got nil error")
	}
}
