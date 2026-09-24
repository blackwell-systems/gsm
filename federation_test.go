package gsm

import (
	"strings"
	"testing"
)

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

// TestFederation_PartialSyncEquivalence proves Corollary 8.10 as a network protocol: a
// distributed system where each node holds only its own component Machine and receives just
// its parent's shared projection (never the full federated state) reaches exactly the same
// per-component normal form as the centralized FedMachine. This is the M3 partial-sync claim.
func TestFederation_PartialSyncEquivalence(t *testing.T) {
	m, mfr, sup, mstate, sstate := buildManufacturerSupplier(t)

	// Centralized reference: apply events through the full FedMachine.
	central := m.NewState()
	central = m.Apply(central, sup, "eexp") // supplier acts first
	central = m.Apply(central, mfr, "epub") // then manufacturer

	// Distributed simulation: each node has ONLY its component Machine + local state.
	mfrMachine := m.Component(mfr)
	supMachine := m.Component(sup)
	mfrLocal := mfrMachine.NewState()
	supLocal := supMachine.NewState()

	// Each node applies its own events locally (Machine.Apply normalizes locally).
	supLocal = supMachine.Apply(supLocal, "eexp")
	mfrLocal = mfrMachine.Apply(mfrLocal, "epub")

	// Partial sync: the manufacturer (source, already finalized as a root) sends only its
	// shared projection down the edge; the supplier merges it. No full state crosses.
	proj, err := m.SharedProjection(mfrLocal, mfr, sup)
	if err != nil {
		t.Fatal(err)
	}
	if proj.From != "manufacturer" || proj.To != "supplier" {
		t.Fatalf("projection routing = %s→%s, want manufacturer→supplier", proj.From, proj.To)
	}
	supLocal, err = supMachine.MergeProjection(supLocal, proj)
	if err != nil {
		t.Fatal(err)
	}

	// Equivalence: distributed per-component states == centralized components.
	if mfrLocal.ID() != m.Of(central, mfr).ID() {
		t.Fatalf("manufacturer diverged: distributed %s vs central %s",
			mfrLocal.Get(mstate), m.Of(central, mfr).Get(mstate))
	}
	if supLocal.ID() != m.Of(central, sup).ID() {
		t.Fatalf("supplier diverged: distributed %s vs central %s",
			supLocal.Get(sstate), m.Of(central, sup).Get(sstate))
	}
	// And it's the expected converged value.
	if supLocal.Get(sstate) != "listed" {
		t.Fatalf("distributed supplier = %s, want listed (authority via projection)", supLocal.Get(sstate))
	}
}

// TestFederation_ProjectionErrors covers the SharedProjection/MergeProjection error paths.
func TestFederation_ProjectionErrors(t *testing.T) {
	m, mfr, sup, _, _ := buildManufacturerSupplier(t)

	// No morphism in the supplier→manufacturer direction.
	if _, err := m.SharedProjection(m.Of(m.NewState(), sup), sup, mfr); err == nil {
		t.Fatal("expected error for a non-existent morphism direction")
	}

	// A projection naming a variable the target machine lacks.
	bad := Projection{From: "manufacturer", To: "supplier", Shared: map[string]uint64{"no_such_var": 1}}
	if _, err := m.Component(sup).MergeProjection(m.Component(sup).NewState(), bad); err == nil {
		t.Fatal("expected error merging a projection with an unknown variable")
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

// TestFederation_ReportAndAccessors covers the explicit Add path, the machine name accessor,
// and the human-readable FedReport rendering.
func TestFederation_ReportAndAccessors(t *testing.T) {
	m, rep, _, _, _ := func() (*FedMachine, *FedReport, *Registry, *Registry, error) {
		mfr := NewRegistry("manufacturer")
		mstate := mfr.Enum("mstate", "draft", "active")
		mfr.Event("epub").Apply(func(s State) State { return s.Set(mstate, "active") }).Add()
		sup := NewRegistry("supplier")
		sstate := sup.Enum("sstate", "idle", "listed")
		image := map[string]string{"draft": "idle", "active": "listed"}
		fed := NewFederation("mfr-sup-2").
			Add(mfr). // explicit Add of an already-referenced component (idempotent)
			Morphism(mfr, sup).Shared(sstate).
			Map(func(srcNF, dst State) State { return dst.Set(sstate, image[srcNF.Get(mstate)]) }).Add()
		mm, rr, err := fed.Build()
		return mm, rr, mfr, sup, err
	}()

	if m.Name() != "mfr-sup-2" {
		t.Fatalf("FedMachine.Name() = %q, want mfr-sup-2", m.Name())
	}
	s := rep.String()
	if !strings.Contains(s, "mfr-sup-2") || !strings.Contains(s, "Morphisms:") {
		t.Fatalf("FedReport.String() missing expected content:\n%s", s)
	}
}

// TestFederation_ApplyNamed exercises name-keyed application (the form event-sourced replay
// uses) including graceful errors for unknown registry and unknown event.
func TestFederation_ApplyNamed(t *testing.T) {
	m, _, sup, _, sstate := buildManufacturerSupplier(t)

	// Registries reports both component names.
	names := m.Registries()
	if len(names) != 2 {
		t.Fatalf("Registries() = %v, want 2 names", names)
	}

	fs, err := m.ApplyNamed(m.NewState(), "manufacturer", "epub")
	if err != nil {
		t.Fatalf("ApplyNamed(manufacturer, epub): %v", err)
	}
	if ss := m.Of(fs, sup).Get(sstate); ss != "listed" {
		t.Fatalf("after named epub, supplier = %s, want listed", ss)
	}

	if _, err := m.ApplyNamed(fs, "nonesuch", "epub"); err == nil {
		t.Fatal("expected error for unknown registry")
	}
	if _, err := m.ApplyNamed(fs, "manufacturer", "nonesuch"); err == nil {
		t.Fatal("expected error for unknown event")
	}
}

// TestFederation_DuplicateNameRejected confirms Build refuses two components sharing a name
// (name-keyed replay would be ambiguous).
func TestFederation_DuplicateNameRejected(t *testing.T) {
	a := NewRegistry("dup")
	ax := a.Bool("ax")
	b := NewRegistry("dup") // same name
	bx := b.Bool("bx")

	fed := NewFederation("dupe").
		Morphism(a, b).Shared(bx).
		Map(func(srcNF, dst State) State { return dst.SetBool(bx, srcNF.GetBool(ax)) }).Add()

	if _, _, err := fed.Build(); err == nil {
		t.Fatal("expected Build to reject duplicate component names, got nil error")
	}
}

// TestFederation_M1Rejected confirms Build refuses a morphism that can drive the target
// invalid (M1 violation, Prop 8.14). Target B requires flag=false; the morphism copies the
// source's boolean into flag, so a source with x=true would make B invalid.
func TestFederation_M1Rejected(t *testing.T) {
	a := NewRegistry("A")
	x := a.Bool("x")

	b := NewRegistry("B")
	flag := b.Bool("flag")
	b.Invariant("flag_off").Watches(flag).
		Holds(func(s State) bool { return !s.GetBool(flag) }).
		Repair(func(s State) State { return s.SetBool(flag, false) }).Add()

	fed := NewFederation("m1-violation").
		Morphism(a, b).Shared(flag).
		Map(func(srcNF, dst State) State { return dst.SetBool(flag, srcNF.GetBool(x)) }).Add()

	_, _, err := fed.Build()
	if err == nil {
		t.Fatal("expected Build to reject an M1-violating morphism, got nil error")
	}
	if !strings.Contains(err.Error(), "M1") {
		t.Fatalf("error should cite M1, got: %v", err)
	}
}

// TestFederation_MultiSourceRejected confirms Build refuses a target with two incoming
// morphisms — the authority argument requires a single source per target (Remark 8.15).
func TestFederation_MultiSourceRejected(t *testing.T) {
	a := NewRegistry("A")
	ax := a.Bool("ax")
	c := NewRegistry("C")
	cx := c.Bool("cx")
	b := NewRegistry("B")
	bx := b.Bool("bx")

	fed := NewFederation("multi-source").
		Morphism(a, b).Shared(bx).
		Map(func(srcNF, dst State) State { return dst.SetBool(bx, srcNF.GetBool(ax)) }).Add().
		Morphism(c, b).Shared(bx).
		Map(func(srcNF, dst State) State { return dst.SetBool(bx, srcNF.GetBool(cx)) }).Add()

	_, _, err := fed.Build()
	if err == nil {
		t.Fatal("expected Build to reject a multi-source target, got nil error")
	}
	if !strings.Contains(err.Error(), "multi-source") {
		t.Fatalf("error should cite multi-source, got: %v", err)
	}
}

// TestFederation_NonSharedWriteRejected confirms Build refuses a Map that mutates a target
// variable outside its declared Shared() set.
func TestFederation_NonSharedWriteRejected(t *testing.T) {
	a := NewRegistry("A")
	x := a.Bool("x")
	b := NewRegistry("B")
	shared := b.Bool("shared")
	local := b.Bool("local")

	fed := NewFederation("bad-footprint").
		Morphism(a, b).Shared(shared).
		Map(func(srcNF, dst State) State {
			// Illegally also writes the local (non-shared) variable.
			return dst.SetBool(shared, srcNF.GetBool(x)).SetBool(local, true)
		}).Add()

	_, _, err := fed.Build()
	if err == nil {
		t.Fatal("expected Build to reject a Map writing a non-shared variable, got nil error")
	}
	if !strings.Contains(err.Error(), "non-shared") {
		t.Fatalf("error should cite non-shared write, got: %v", err)
	}
}
