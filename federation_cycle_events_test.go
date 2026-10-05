package gsm

import (
	"errors"
	"strings"
	"testing"
)

// Event order on AllowMonotoneCycles networks, ported from normalization-confluence
// coq/FederationEventsCyclesCheck.v. The theorem cyc_check_gc_lfp says gsm's C1 and C2, read on a
// cycle (locals compared after the final overwrite, image set over every valid source state),
// imply that every interleaving of independent events converges. These tests pin the three
// instances the file uses: the latch it rejects, the instance it accepts, and the example showing
// C2 cannot be dropped.

// alarmCycle is the two-registry cycle of FederationEventsCycles.v (cyc_instance): each registry
// has a local alarm and a shared flag fed by the other, flag := other.alarm || other.flag, so
// A -> B -> A. extraA adds events to A before the morphisms are declared.
func alarmCycle(extraA func(A *Registry, alarm, flag Var)) (*Federation, *Registry, *Registry, [4]Var) {
	A := NewRegistry("A")
	la := A.Bool("alarm")
	sa := A.Bool("flag")
	B := NewRegistry("B")
	lb := B.Bool("alarm")
	sb := B.Bool("flag")
	extraA(A, la, sa)
	f := NewFederation("alarms").AllowMonotoneCycles()
	f.Morphism(B, A).Shared(sa).
		Map(func(s, d State) State { return d.SetBool(sa, s.GetBool(lb) || s.GetBool(sb)) }).Add()
	f.Morphism(A, B).Shared(sb).
		Map(func(s, d State) State { return d.SetBool(sb, s.GetBool(la) || s.GetBool(sa)) }).Add()
	return f, A, B, [4]Var{la, sa, lb, sb}
}

// TestCycleEvents_LatchRejected is check_rejects_latch: LatchA copies A's shared flag (fed by B)
// into A's local alarm. The repair is monotone, each registry's own CC holds, and C2 holds (no
// declared pairs), but C1 fails: the local outcome of LatchA depends on which image the flag
// holds. cyc_counterexample shows LatchA;RaiseB and RaiseB;LatchA diverge, so Build must reject it
// with a *CrossOrderError on A.
func TestCycleEvents_LatchRejected(t *testing.T) {
	f, A, B, v := alarmCycle(func(A *Registry, la, sa Var) {
		A.Event("latch").Writes(la).
			Apply(func(s State) State { return s.SetBool(la, s.GetBool(la) || s.GetBool(sa)) }).Add()
	})
	B.Event("raise").Writes(v[2]).Apply(func(s State) State { return s.SetBool(v[2], true) }).Add()
	am, _, aerr := A.Build()
	bm, _, berr := B.Build()
	if aerr != nil || berr != nil {
		t.Fatalf("A and B alone should build (each one's own CC holds): %v, %v", aerr, berr)
	}

	_, _, err := f.Build()
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CrossOrderError (C1 fails at LatchA), got %T: %v", err, err)
	}
	if ce.Target != "A" || ce.Event != "latch" || ce.Morphism != "morphism B→A" {
		t.Fatalf("wrong failure: %+v", ce)
	}
	// The two sides differ only in A's local alarm (the overwrite fixes the flag on both sides).
	if ce.SourceFirst.GetBool(v[1]) != ce.EventFirst.GetBool(v[1]) ||
		ce.SourceFirst.GetBool(v[0]) == ce.EventFirst.GetBool(v[0]) {
		t.Fatalf("want the locals to differ and the shared flag to agree: %s vs %s", ce.SourceFirst, ce.EventFirst)
	}

	// The divergence is real (cyc_counterexample): replay both orders from the all-false state with
	// the component machines and this cycle's least fixed point (both flags = A.alarm || B.alarm).
	nf := func(a, b State) (State, State) {
		up := a.GetBool(v[0]) || b.GetBool(v[2])
		return a.SetBool(v[1], up), b.SetBool(v[3], up)
	}
	a0, b0 := nf(am.NewState(), bm.NewState())
	a1, b1 := nf(am.Apply(a0, "latch"), b0)
	a1, _ = nf(a1, bm.Apply(b1, "raise"))
	a2, b2 := nf(a0, bm.Apply(b0, "raise"))
	a2, _ = nf(am.Apply(a2, "latch"), b2)
	if a1.GetBool(v[0]) || !a2.GetBool(v[0]) {
		t.Fatalf("latch,raise should leave A's alarm down and raise,latch raise it: %s vs %s", a1, a2)
	}
}

// TestCycleEvents_InstanceBuilds is cyc_check_instance: raise/clear on both registries, plus PingA,
// which raises A's alarm and also writes A's shared flag (allowed: the reset to bottom erases
// shared writes). RaiseA and PingA are declared independent. C1 and C2 hold, so Build accepts, and
// every pair of federated-independent events commutes at every reachable state (GC).
func TestCycleEvents_InstanceBuilds(t *testing.T) {
	f, A, B, v := alarmCycle(func(A *Registry, la, sa Var) {
		A.Event("raise").Writes(la).Apply(func(s State) State { return s.SetBool(la, true) }).Add()
		A.Event("clear").Writes(la).Apply(func(s State) State { return s.SetBool(la, false) }).Add()
		A.Event("ping").Writes(la, sa).Apply(func(s State) State { return s.SetBool(la, true).SetBool(sa, true) }).Add()
		A.OnlyDeclaredPairs().Independent("raise", "ping")
	})
	B.Event("raise").Writes(v[2]).Apply(func(s State) State { return s.SetBool(v[2], true) }).Add()
	B.Event("clear").Writes(v[2]).Apply(func(s State) State { return s.SetBool(v[2], false) }).Add()
	B.OnlyDeclaredPairs()

	m, rep, err := f.Build()
	if err != nil {
		t.Fatalf("cyc_check_instance rejected: %v", err)
	}
	joined := strings.Join(rep.Checks, "\n")
	for _, want := range []string{"cross-registry CC (C1)", "repaired CC (C2)", "event order on the cycle", "cyc_check_gc_lfp"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Checks missing %q:\n%s", want, joined)
		}
	}

	// grun [PingA; ClearA] from the all-false state is all-false: PingA's shared write is erased.
	s0 := m.Normalize(m.NewState())
	s := m.Apply(m.Apply(s0, A, "ping"), A, "clear")
	for _, r := range []*Registry{A, B} {
		if m.Of(s, r).ID() != m.Of(s0, r).ID() {
			t.Fatalf("ping,clear should return to the all-false state, got %s at %s", m.Of(s, r), r.name)
		}
	}

	// GC at every reachable state: federated-independent pairs commute after re-normalization.
	type ev struct {
		r    *Registry
		name string
	}
	evs := []ev{{A, "raise"}, {A, "clear"}, {A, "ping"}, {B, "raise"}, {B, "clear"}}
	indep := func(x, y ev) bool {
		if x.r != y.r {
			return true
		}
		return (x.name == "raise" && y.name == "ping") || (x.name == "ping" && y.name == "raise")
	}
	key := func(fs FedState) string { return m.Of(fs, A).String() + "|" + m.Of(fs, B).String() }
	seen := map[string]bool{key(s0): true}
	queue := []FedState{s0}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, x := range evs {
			for _, y := range evs {
				if !indep(x, y) {
					continue
				}
				xy := m.Apply(m.Apply(cur, x.r, x.name), y.r, y.name)
				yx := m.Apply(m.Apply(cur, y.r, y.name), x.r, x.name)
				if key(xy) != key(yx) {
					t.Fatalf("from %s: %s.%s,%s.%s gives %s but the other order gives %s",
						key(cur), x.r.name, x.name, y.r.name, y.name, key(xy), key(yx))
				}
			}
			next := m.Apply(cur, x.r, x.name)
			if k := key(next); !seen[k] {
				seen[k] = true
				queue = append(queue, next)
			}
		}
	}
}

// swapAuditCycle is c1_localcc_insufficient. A has a local a and a shared cb; B has locals sb, au
// and shared sa, ca. Repair: A.cb := B.au || B.ca; B.sa := false; B.ca := A.a || A.cb (A -> B ->
// A). Swap exchanges B's sb and sa; Audit records sa xor sb. They commute on B's full state, and
// C1 holds (the shared sa is always false), but between them the reset erases what Swap moved into
// sa, so C2 fails and Audit;Swap and Swap;Audit diverge.
func swapAuditCycle(declarePair bool) (*Federation, *Registry, *Registry, [6]Var) {
	A := NewRegistry("A")
	a := A.Bool("a")
	cb := A.Bool("cb")
	B := NewRegistry("B")
	sb := B.Bool("sb")
	au := B.Bool("au")
	sa := B.Bool("sa")
	ca := B.Bool("ca")
	B.Event("swap").Writes(sb, sa).
		Apply(func(s State) State { return s.SetBool(sb, s.GetBool(sa)).SetBool(sa, s.GetBool(sb)) }).Add()
	B.Event("audit").Writes(au).
		Apply(func(s State) State { return s.SetBool(au, s.GetBool(sa) != s.GetBool(sb)) }).Add()
	if !declarePair {
		B.OnlyDeclaredPairs()
	}
	f := NewFederation("swapaudit").AllowMonotoneCycles()
	f.Morphism(A, B).Shared(sa, ca).
		Map(func(s, d State) State { return d.SetBool(sa, false).SetBool(ca, s.GetBool(a) || s.GetBool(cb)) }).Add()
	f.Morphism(B, A).Shared(cb).
		Map(func(s, d State) State { return d.SetBool(cb, s.GetBool(au) || s.GetBool(ca)) }).Add()
	return f, A, B, [6]Var{a, cb, sb, au, sa, ca}
}

// TestCycleEvents_C2CannotBeDropped: with swap and audit covered by B's CC (the default, every
// pair), Build must reject the cycle with a *SameTargetOrderError on B, not a *CrossOrderError
// (C1 holds). Undeclaring the pair makes the federation build, and replaying the Coq witness on
// that machine shows the two orders do diverge.
func TestCycleEvents_C2CannotBeDropped(t *testing.T) {
	f, _, B, _ := swapAuditCycle(true)
	if _, _, err := B.Build(); err != nil {
		t.Fatalf("B alone should build (swap and audit commute on B's full state): %v", err)
	}
	_, _, err := f.Build()
	var ce *CrossOrderError
	if errors.As(err, &ce) {
		t.Fatalf("C1 holds here; want *SameTargetOrderError, got %v", err)
	}
	var se *SameTargetOrderError
	if !errors.As(err, &se) {
		t.Fatalf("want *SameTargetOrderError, got %T: %v", err, err)
	}
	if se.Target != "B" || se.Morphism != "morphism A→B" {
		t.Fatalf("wrong failure: %+v", se)
	}
	if pair := se.First + "," + se.Second; pair != "swap,audit" && pair != "audit,swap" {
		t.Fatalf("wrong event pair %q", pair)
	}

	// Undeclared, the federation makes no promise about the pair and builds; the orders diverge.
	g, A2, B2, v := swapAuditCycle(false)
	m, _, err := g.Build()
	if err != nil {
		t.Fatalf("with the pair undeclared the cycle should build: %v", err)
	}
	s := m.NewState()
	s.states[m.mustIndex(B2)] = m.Of(s, B2).SetBool(v[2], true) // s4: B.sb = true
	s4 := m.Normalize(s)
	as := m.Apply(m.Apply(s4, B2, "audit"), B2, "swap")
	sa := m.Apply(m.Apply(s4, B2, "swap"), B2, "audit")
	if !m.Of(as, B2).GetBool(v[3]) || !m.Of(as, A2).GetBool(v[1]) {
		t.Fatalf("audit,swap: want B.au and A.cb set, got A %s B %s", m.Of(as, A2), m.Of(as, B2))
	}
	if m.Of(sa, B2).GetBool(v[3]) || m.Of(sa, A2).GetBool(v[1]) {
		t.Fatalf("swap,audit: want all false, got A %s B %s", m.Of(sa, A2), m.Of(sa, B2))
	}
}

// TestCycleEvents_MultiSourceJointImage: on a cycle through a multi-source target, the report
// says C1 and C2 ran against the joint image set of all incoming edges.
func TestCycleEvents_MultiSourceJointImage(t *testing.T) {
	A := NewRegistry("A")
	la := A.Bool("alarm")
	sa := A.Bool("flag")
	B := NewRegistry("B")
	lb := B.Bool("alarm")
	sb := B.Bool("flag")
	C := NewRegistry("C")
	lc := C.Bool("alarm")
	A.Event("raise").Writes(la).Apply(func(s State) State { return s.SetBool(la, true) }).Add()
	f := NewFederation("joint").AllowMonotoneCycles()
	f.Morphism(B, A).Shared(sa).Map(func(s, d State) State { return d.SetBool(sa, s.GetBool(lb) || s.GetBool(sb)) }).Add()
	f.Morphism(C, A).Shared(sa).Map(func(s, d State) State { return d.SetBool(sa, s.GetBool(lc)) }).Add()
	f.Resolve(A, func(d State, src map[string]State) State {
		return d.SetBool(sa, src["B"].GetBool(lb) || src["B"].GetBool(sb) || src["C"].GetBool(lc))
	})
	f.Morphism(A, B).Shared(sb).Map(func(s, d State) State { return d.SetBool(sb, s.GetBool(la) || s.GetBool(sa)) }).Add()
	_, rep, err := f.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if joined := strings.Join(rep.Checks, "\n"); !strings.Contains(joined, "1 multi-source target(s)") ||
		!strings.Contains(joined, "joint image set") {
		t.Fatalf("Checks should report the joint image set:\n%s", joined)
	}
}
