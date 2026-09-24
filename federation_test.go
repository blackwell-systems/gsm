package gsm

import (
	"fmt"
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

// idMap is an identity Map for edges into a resolved target (superseded by the resolver).
func idMap(srcNF, dst State) State { return dst }

// buildAccessControl builds a multi-source (DAG) federation: independent HR and Security
// registries both feed a Door, whose access is granted only if HR says employed AND Security
// says cleared — an AND merge no single authority could express. The resolver makes it
// converge; Build verifies the merge exhaustively.
func buildAccessControl(t *testing.T) (m *FedMachine, hr, sec, door *Registry, access Var) {
	t.Helper()
	hr = NewRegistry("hr")
	employed := hr.Bool("employed")
	hr.Event("hire").Writes(employed).Apply(func(s State) State { return s.SetBool(employed, true) }).Add()
	hr.Event("terminate").Writes(employed).Apply(func(s State) State { return s.SetBool(employed, false) }).Add()

	sec = NewRegistry("security")
	cleared := sec.Bool("cleared")
	sec.Event("grant_clearance").Writes(cleared).Apply(func(s State) State { return s.SetBool(cleared, true) }).Add()
	sec.Event("revoke").Writes(cleared).Apply(func(s State) State { return s.SetBool(cleared, false) }).Add()

	door = NewRegistry("door")
	access = door.Enum("access", "denied", "granted")

	fed := NewFederation("access").
		Morphism(hr, door).Shared(access).Map(idMap).Add().
		Morphism(sec, door).Shared(access).Map(idMap).Add().
		Resolve(door, func(dst State, src map[string]State) State {
			if src["hr"].GetBool(employed) && src["security"].GetBool(cleared) {
				return dst.Set(access, "granted")
			}
			return dst.Set(access, "denied")
		})

	mm, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("access-control federation failed to build: %v\n%s", err, rep)
	}
	return mm, hr, sec, door, access
}

// TestFederation_MultiSourceResolver is the M4 headline: a DAG target with two independent
// sources converges via a declared resolver. Access is granted only when BOTH sources agree,
// and the result is order-independent.
func TestFederation_MultiSourceResolver(t *testing.T) {
	m, hr, sec, door, access := buildAccessControl(t)
	get := func(fs FedState) string { return m.Of(fs, door).Get(access) }

	s := m.NewState()
	if get(m.Normalize(s)) != "denied" {
		t.Fatalf("initial access = %s, want denied", get(m.Normalize(s)))
	}
	s = m.Apply(s, hr, "hire")
	if get(s) != "denied" {
		t.Fatalf("employed-only access = %s, want denied (needs both)", get(s))
	}
	s = m.Apply(s, sec, "grant_clearance")
	if get(s) != "granted" {
		t.Fatalf("both-conditions access = %s, want granted", get(s))
	}
	s = m.Apply(s, sec, "revoke")
	if get(s) != "denied" {
		t.Fatalf("post-revoke access = %s, want denied (merge re-evaluates)", get(s))
	}

	// Order independence across the two independent sources.
	a := m.Apply(m.Apply(m.NewState(), hr, "hire"), sec, "grant_clearance")
	b := m.Apply(m.Apply(m.NewState(), sec, "grant_clearance"), hr, "hire")
	if m.Of(a, door).ID() != m.Of(b, door).ID() {
		t.Fatalf("multi-source order dependence: %s vs %s", get(a), get(b))
	}
	if get(a) != "granted" || !m.IsValid(a) {
		t.Fatalf("converged multi-source state wrong: access=%s valid=%v", get(a), m.IsValid(a))
	}
}

// buildBadDoor wires HR+Security into a Door with a local `logged` flag and a
// "granted requires logged" invariant. The resolver is built by `mk`, which receives the
// live Var handles so each rejection test can express a specific fault.
func buildBadDoor(name string, mk func(access, logged, employed, cleared Var) Resolver) *Federation {
	hr := NewRegistry("hr")
	employed := hr.Bool("employed")
	hr.Event("hire").Writes(employed).Apply(func(s State) State { return s.SetBool(employed, true) }).Add()
	sec := NewRegistry("security")
	cleared := sec.Bool("cleared")
	sec.Event("clear").Writes(cleared).Apply(func(s State) State { return s.SetBool(cleared, true) }).Add()

	door := NewRegistry("door")
	access := door.Enum("access", "denied", "granted")
	logged := door.Bool("logged")
	door.Invariant("granted_needs_log").Watches(access, logged).
		Holds(func(s State) bool { return s.Get(access) != "granted" || s.GetBool(logged) }).
		Repair(func(s State) State { return s.Set(access, "denied") }).Add()

	return NewFederation(name).
		Morphism(hr, door).Shared(access).Map(idMap).Add().
		Morphism(sec, door).Shared(access).Map(idMap).Add().
		Resolve(door, mk(access, logged, employed, cleared))
}

// TestFederation_ResolverValidityRejected: a resolver that can produce an invalid target
// (grants without the required log) is rejected at build.
func TestFederation_ResolverValidityRejected(t *testing.T) {
	fed := buildBadDoor("bad-validity", func(access, logged, employed, cleared Var) Resolver {
		return func(dst State, src map[string]State) State {
			if src["hr"].GetBool(employed) && src["security"].GetBool(cleared) {
				return dst.Set(access, "granted") // ignores `logged` → can be invalid
			}
			return dst.Set(access, "denied")
		}
	})
	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "invalid target") {
		t.Fatalf("expected M1-for-merge rejection, got: %v", err)
	}
}

// TestFederation_ResolverLocalDependenceRejected: a resolver whose shared output depends on
// the target's local state (not just the sources) is rejected.
func TestFederation_ResolverLocalDependenceRejected(t *testing.T) {
	fed := buildBadDoor("bad-local", func(access, logged, employed, cleared Var) Resolver {
		return func(dst State, src map[string]State) State {
			if dst.GetBool(logged) { // reads the target's local state — forbidden
				return dst.Set(access, "granted")
			}
			return dst.Set(access, "denied")
		}
	})
	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "local state") {
		t.Fatalf("expected source-determinacy rejection, got: %v", err)
	}
}

// TestFederation_ResolverNonSharedWriteRejected: a resolver that writes a non-shared target
// variable is rejected.
func TestFederation_ResolverNonSharedWriteRejected(t *testing.T) {
	fed := buildBadDoor("bad-write", func(access, logged, employed, cleared Var) Resolver {
		return func(dst State, src map[string]State) State {
			return dst.Set(access, "denied").SetBool(logged, true) // writes non-shared `logged`
		}
	})
	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "non-shared") {
		t.Fatalf("expected non-shared-write rejection, got: %v", err)
	}
}

// TestFederation_EmbedComposition demonstrates compositionality: a pricing→catalog
// subsystem is defined and verified in isolation, then embedded into a larger system and
// connected to an order registry. Upgrading pricing propagates through the embedded
// subsystem and across the boundary — the composed federation is the flat convergent machine.
func TestFederation_EmbedComposition(t *testing.T) {
	// A reusable sub-federation, verified on its own first.
	pricing := NewRegistry("pricing")
	tier := pricing.Enum("tier", "free", "pro")
	pricing.Event("upgrade").Writes(tier).
		Guard(func(s State) bool { return s.Get(tier) == "free" }).
		Apply(func(s State) State { return s.Set(tier, "pro") }).Add()
	catalog := NewRegistry("catalog")
	badge := catalog.Enum("badge", "none", "star")
	badgeOf := map[string]string{"free": "none", "pro": "star"}
	sub := NewFederation("pricing-sub").
		Morphism(pricing, catalog).Shared(badge).
		Map(func(srcNF, d State) State { return d.Set(badge, badgeOf[srcNF.Get(tier)]) }).Add()
	if _, _, err := sub.Build(); err != nil {
		t.Fatalf("sub-federation must verify in isolation: %v", err)
	}

	// Embed the verified subsystem into a larger system and connect it to an order registry.
	order := NewRegistry("order")
	perk := order.Enum("perk", "basic", "premium")
	perkOf := map[string]string{"none": "basic", "star": "premium"}
	outer := NewFederation("system").
		Embed(sub).
		Morphism(catalog, order).Shared(perk).
		Map(func(srcNF, d State) State { return d.Set(perk, perkOf[srcNF.Get(badge)]) }).Add()
	m, rep, err := outer.Build()
	if err != nil {
		t.Fatalf("embedded federation failed to build: %v\n%s", err, rep)
	}
	if len(m.Registries()) != 3 {
		t.Fatalf("composed federation has %d components, want 3", len(m.Registries()))
	}

	// Pricing upgrade propagates pricing → catalog (embedded) → order (boundary).
	s := m.Apply(m.NewState(), pricing, "upgrade")
	if got := m.Of(s, catalog).Get(badge); got != "star" {
		t.Fatalf("catalog badge = %s, want star (propagated inside the embedded subsystem)", got)
	}
	if got := m.Of(s, order).Get(perk); got != "premium" {
		t.Fatalf("order perk = %s, want premium (propagated across the embed boundary)", got)
	}
	if !m.IsValid(s) {
		t.Fatal("composed state is not federally valid")
	}
}

// TestFederation_TenRegistryChain shows a federation scales to many registries: a 10-deep
// chain r0→r1→…→r9 where each morphism copies its parent's flag. Turning the root on
// propagates through all ten levels in a single ρ_Fed (topological order finalizes each
// parent before its edge fires). No product state space is ever built — the ten components
// stay independent.
func TestFederation_TenRegistryChain(t *testing.T) {
	const N = 10
	regs := make([]*Registry, N)
	on := make([]Var, N)
	for i := 0; i < N; i++ {
		r := NewRegistry(fmt.Sprintf("r%02d", i))
		on[i] = r.Bool("on")
		regs[i] = r
	}
	// Only the root has an event; the rest receive state purely through morphisms.
	regs[0].Event("turn_on").Writes(on[0]).
		Apply(func(s State) State { return s.SetBool(on[0], true) }).Add()

	fed := NewFederation("chain").Add(regs[0])
	for i := 1; i < N; i++ {
		parent, child := regs[i-1], regs[i]
		pv, cv := on[i-1], on[i]
		fed.Morphism(parent, child).Shared(cv).
			Map(func(srcNF, dst State) State { return dst.SetBool(cv, srcNF.GetBool(pv)) }).Add()
	}

	m, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("10-registry chain failed to build: %v\n%s", err, rep)
	}
	if len(rep.Components) != N {
		t.Fatalf("report has %d components, want %d", len(rep.Components), N)
	}

	// Turn on the root; one Apply propagates the flag through all ten registries.
	s := m.Apply(m.NewState(), regs[0], "turn_on")
	for i := 0; i < N; i++ {
		if !m.Of(s, regs[i]).GetBool(on[i]) {
			t.Fatalf("registry r%02d did not receive propagation", i)
		}
	}
	if !m.IsValid(s) {
		t.Fatal("10-registry state is not federally valid")
	}
}

// TestFederation_BranchingTree shows federations aren't limited to chains: a root fans out to
// three children, and one child fans out to two grandchildren (6 registries, still a tree).
func TestFederation_BranchingTree(t *testing.T) {
	root := NewRegistry("root")
	rv := root.Enum("v", "off", "on")
	root.Event("flip").Apply(func(s State) State { return s.Set(rv, "on") }).Add()

	mkChild := func(name string) (*Registry, Var) {
		c := NewRegistry(name)
		return c, c.Enum("v", "off", "on")
	}
	a, av := mkChild("a")
	b, bv := mkChild("b")
	c, cv := mkChild("c")
	d, dv := mkChild("d") // grandchild of b
	e, ev := mkChild("e") // grandchild of b

	copyV := func(src, dst Var) func(State, State) State {
		return func(srcNF, dstS State) State { return dstS.Set(dst, srcNF.Get(src)) }
	}
	fed := NewFederation("tree").
		Morphism(root, a).Shared(av).Map(copyV(rv, av)).Add().
		Morphism(root, b).Shared(bv).Map(copyV(rv, bv)).Add().
		Morphism(root, c).Shared(cv).Map(copyV(rv, cv)).Add().
		Morphism(b, d).Shared(dv).Map(copyV(bv, dv)).Add().
		Morphism(b, e).Shared(ev).Map(copyV(bv, ev)).Add()

	m, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("branching tree failed to build: %v\n%s", err, rep)
	}

	s := m.Apply(m.NewState(), root, "flip")
	for _, pair := range []struct {
		r *Registry
		v Var
	}{{a, av}, {b, bv}, {c, cv}, {d, dv}, {e, ev}} {
		if m.Of(s, pair.r).Get(pair.v) != "on" {
			t.Fatalf("registry %q did not receive propagation (incl. 2 levels deep)", pair.r.name)
		}
	}
}

// buildMonotoneMesh builds a CYCLIC federation: a 3-cycle A→B→C→A where each node's shared
// `s` takes the max of its predecessor's request and shared value. max is monotone, so the
// highest request propagates all the way around the cycle — convergence without acyclicity
// (the Monotone Convergence Despite Cycles theorem).
func buildMonotoneMesh(t *testing.T) (m *FedMachine, regs [3]*Registry, sV [3]Var) {
	t.Helper()
	names := [3]string{"A", "B", "C"}
	var reqV [3]Var
	for i := 0; i < 3; i++ {
		r := NewRegistry(names[i])
		reqV[i] = r.Int("req", 0, 3)
		sV[i] = r.Int("s", 0, 3)
		val, rq := i+1, reqV[i]
		r.Event(fmt.Sprintf("req%d", val)).Writes(rq).
			Apply(func(st State) State { return st.SetInt(rq, val) }).Add()
		regs[i] = r
	}
	fed := NewFederation("mesh").AllowMonotoneCycles()
	for i := 0; i < 3; i++ {
		pred := (i + 2) % 3 // A←C, B←A, C←B  (edges C→A, A→B, B→C form a cycle)
		src, dst := regs[pred], regs[i]
		pReq, pS, dS := reqV[pred], sV[pred], sV[i]
		fed.Morphism(src, dst).Shared(dS).
			Map(func(srcNF, d State) State {
				mx := srcNF.GetInt(pReq)
				if v := srcNF.GetInt(pS); v > mx {
					mx = v
				}
				return d.SetInt(dS, mx)
			}).Add()
	}
	mm, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("monotone mesh failed to build: %v\n%s", err, rep)
	}
	return mm, regs, sV
}

// TestFederation_MonotoneMeshConverges is the M4+ headline: a CYCLIC network converges, and
// converges order-independently, because its repair is monotone. The max request (3) reaches
// every node around the cycle.
func TestFederation_MonotoneMeshConverges(t *testing.T) {
	m, regs, sV := buildMonotoneMesh(t)

	s := m.NewState()
	s = m.Apply(s, regs[0], "req1")
	s = m.Apply(s, regs[1], "req2")
	s = m.Apply(s, regs[2], "req3")
	for i := 0; i < 3; i++ {
		if got := m.Of(s, regs[i]).GetInt(sV[i]); got != 3 {
			t.Fatalf("node %s: s=%d, want 3 (max request propagates around the cycle)", regs[i].name, got)
		}
	}
	if !m.IsValid(s) {
		t.Fatal("mesh fixed point is not federally valid")
	}

	// Order independence: applying the same events in a different order reaches the same state.
	s2 := m.NewState()
	s2 = m.Apply(s2, regs[2], "req3")
	s2 = m.Apply(s2, regs[0], "req1")
	s2 = m.Apply(s2, regs[1], "req2")
	for i := 0; i < 3; i++ {
		if m.Of(s, regs[i]).ID() != m.Of(s2, regs[i]).ID() {
			t.Fatalf("cyclic mesh is order-dependent at node %s", regs[i].name)
		}
	}
}

// TestFederation_NonMonotoneCycleRejected confirms a cyclic network with non-monotone repair
// (the negation counterexample) is rejected even under AllowMonotoneCycles.
func TestFederation_NonMonotoneCycleRejected(t *testing.T) {
	a := NewRegistry("A")
	as := a.Bool("s")
	b := NewRegistry("B")
	bs := b.Bool("s")
	fed := NewFederation("flip").AllowMonotoneCycles().
		Morphism(a, b).Shared(bs).Map(func(src, d State) State { return d.SetBool(bs, !src.GetBool(as)) }).Add().
		Morphism(b, a).Shared(as).Map(func(src, d State) State { return d.SetBool(as, !src.GetBool(bs)) }).Add()

	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "monotone") {
		t.Fatalf("expected non-monotone cycle rejection, got: %v", err)
	}
}

// TestFederation_CycleRejectedWithoutOptIn confirms cycles are still rejected by default
// (even monotone ones) unless AllowMonotoneCycles is set.
func TestFederation_CycleRejectedWithoutOptIn(t *testing.T) {
	a := NewRegistry("A")
	av := a.Int("v", 0, 3)
	b := NewRegistry("B")
	bv := b.Int("v", 0, 3)
	// Monotone 2-cycle, but no AllowMonotoneCycles() — must be rejected as a cycle.
	fed := NewFederation("no-optin").
		Morphism(a, b).Shared(bv).Map(func(src, d State) State { return d.SetInt(bv, src.GetInt(av)) }).Add().
		Morphism(b, a).Shared(av).Map(func(src, d State) State { return d.SetInt(av, src.GetInt(bv)) }).Add()

	if _, _, err := fed.Build(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle rejection without opt-in, got: %v", err)
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
	mfr := NewRegistry("manufacturer")
	mstate := mfr.Enum("mstate", "draft", "active")
	mfr.Event("epub").Apply(func(s State) State { return s.Set(mstate, "active") }).Add()
	sup := NewRegistry("supplier")
	sstate := sup.Enum("sstate", "idle", "listed")
	image := map[string]string{"draft": "idle", "active": "listed"}

	m, rep, err := NewFederation("mfr-sup-2").
		Add(mfr). // explicit Add of an already-referenced component (idempotent)
		Morphism(mfr, sup).Shared(sstate).
		Map(func(srcNF, dst State) State { return dst.Set(sstate, image[srcNF.Get(mstate)]) }).Add().
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

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
