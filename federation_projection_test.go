package gsm

import (
	"errors"
	"strings"
	"testing"
)

// The XU check (distributed projection merging), ported from normalization-confluence
// coq/FederationEvents.v (XU, dist_interleavings_converge, supply_instance) and
// coq/FederationGRS.v (fed_grs_c1_c2_insufficient).

// flipFederation is fed_grs_c1_c2_insufficient. The target's state is (flag, acc) with flag
// shared; the morphism's image is always flag = false (gg_G is constant). g1 and g2 are the same
// step, flag := !flag and acc := acc xor flag, so the target's own CC holds; C1 holds (one image,
// so the shared component never moves) and C2 holds (the repair between two flips resets flag
// to false before the second one reads it). XU fails at the valid state flag = true, which a
// node reaches when it applies g1 and has not yet merged the next projection.
func flipFederation() (f *Federation, hub, node *Registry, flag, acc Var) {
	hub = NewRegistry("hub")
	on := hub.Bool("on")
	hub.Event("ping").Writes(on).Apply(func(s State) State { return s.SetBool(on, !s.GetBool(on)) }).Add()

	node = NewRegistry("node")
	flag = node.Bool("flag")
	acc = node.Bool("acc")
	flip := func(s State) State {
		return s.SetBool(flag, !s.GetBool(flag)).SetBool(acc, s.GetBool(acc) != s.GetBool(flag))
	}
	node.Event("g1").Writes(flag, acc).Apply(flip).Add()
	node.Event("g2").Writes(flag, acc).Apply(flip).Add()

	f = NewFederation("flips")
	f.Morphism(hub, node).Shared(flag).
		Map(func(_, d State) State { return d.SetBool(flag, false) }).Add()
	return f, hub, node, flag, acc
}

// dAct is one step of a distributed run on a single-edge federation src→dst: an event on the
// source node, an event on the target node, or (event == "") the target merging the projection
// of the source's current state.
type dAct struct {
	src   bool
	event string
}

// runDistributed runs w with each registry on its own node: the source applies its events to
// its own Machine, the target applies its events to its own Machine and merges projections
// (SharedProjection, MergeProjection) where w says. Propagation then completes: the target
// merges the projection of the source's final state, which is N of the theorem on valid states.
// It returns both nodes' final states.
func runDistributed(t *testing.T, m *FedMachine, src, dst *Registry, s0 FedState, w []dAct) (State, State) {
	t.Helper()
	sm, dm := m.Component(src), m.Component(dst)
	ss, ds := m.Of(s0, src), m.Of(s0, dst)
	merge := func() {
		p, err := m.SharedProjection(ss, src, dst)
		if err != nil {
			t.Fatalf("SharedProjection: %v", err)
		}
		if ds, err = dm.MergeProjection(ds, p); err != nil {
			t.Fatalf("MergeProjection: %v", err)
		}
	}
	for _, a := range w {
		switch {
		case a.event == "":
			merge()
		case a.src:
			ss = sm.Apply(ss, a.event)
		default:
			ds = dm.Apply(ds, a.event)
		}
	}
	merge()
	return ss, ds
}

// fedRun is the FedMachine run of w's events from N(s0): the right side of propagation_flush.
func fedRun(m *FedMachine, src, dst *Registry, s0 FedState, w []dAct) (State, State) {
	s := m.Normalize(s0)
	for _, a := range w {
		switch {
		case a.event == "":
		case a.src:
			s = m.Apply(s, src, a.event)
		default:
			s = m.Apply(s, dst, a.event)
		}
	}
	return m.Of(s, src), m.Of(s, dst)
}

// allValidStarts returns every federated state whose components are each valid, consistent
// with the morphism or not (dist_interleavings_converge assumes only Inv s).
func allValidStarts(m *FedMachine, src, dst *Registry) []FedState {
	var out []FedState
	for _, a := range src.validStates() {
		for _, b := range dst.validStates() {
			fs := m.NewState()
			fs.states[m.idx[src]] = State{packed: a.packed, vars: m.Component(src).vars}
			fs.states[m.idx[dst]] = State{packed: b.packed, vars: m.Component(dst).vars}
			out = append(out, fs)
		}
	}
	return out
}

// eachWord calls fn with every distributed word over evs: every permutation of the events, with
// a merge before any subset of them.
func eachWord(evs []dAct, fn func(w []dAct)) {
	evs = append([]dAct(nil), evs...)
	var perm func(k int)
	perm = func(k int) {
		if k < len(evs) {
			for i := k; i < len(evs); i++ {
				evs[k], evs[i] = evs[i], evs[k]
				perm(k + 1)
				evs[k], evs[i] = evs[i], evs[k]
			}
			return
		}
		for mask := 0; mask < 1<<len(evs); mask++ {
			var w []dAct
			for i, e := range evs {
				if mask&(1<<i) != 0 {
					w = append(w, dAct{})
				}
				w = append(w, e)
			}
			fn(w)
		}
	}
	perm(0)
}

// TestProjection_C1C2Insufficient_BuildReports: fed_grs_c1_c2_insufficient builds (C1 and C2
// hold, so the FedMachine is certified), and the report says projection merging is not
// certified, with the XU witness.
func TestProjection_C1C2Insufficient_BuildReports(t *testing.T) {
	f, hub, node, flag, acc := flipFederation()
	m, rep, err := f.Build()
	if err != nil {
		t.Fatalf("C1 and C2 hold, Build must accept: %v", err)
	}
	if rep.ProjectionSafe {
		t.Fatal("ProjectionSafe = true for a federation that fails XU")
	}
	if len(rep.ProjectionWitnesses) != 1 {
		t.Fatalf("want one witness (target node), got %d: %v", len(rep.ProjectionWitnesses), rep.ProjectionWitnesses)
	}
	w := rep.ProjectionWitnesses[0]
	if w.Target != "node" || w.Event != "g1" || w.Morphism != "morphism hub→node" {
		t.Fatalf("wrong witness: %+v", w)
	}
	// The gg witness: b = (true, false); merge first gives (false, false), event first (false, true).
	if !w.State.GetBool(flag) || w.State.GetBool(acc) {
		t.Fatalf("witness state = %s, want flag=true acc=false", w.State)
	}
	if w.MergeFirst.GetBool(flag) || w.MergeFirst.GetBool(acc) || w.EventFirst.GetBool(flag) || !w.EventFirst.GetBool(acc) {
		t.Fatalf("witness results = %s / %s, want {false,false} / {false,true}", w.MergeFirst, w.EventFirst)
	}
	last := rep.Checks[len(rep.Checks)-1]
	if last != rep.ProjectionLine || !strings.Contains(last, "not certified") || !strings.Contains(last, `XU fails on "node"`) {
		t.Fatalf("Checks does not end with the projection line: %q (ProjectionLine %q)", last, rep.ProjectionLine)
	}
	if !strings.Contains(rep.String(), "distributed projection merging") {
		t.Fatalf("report text omits the projection line:\n%s", rep)
	}

	// The FedMachine model is unaffected: every order of the target's events converges
	// (fed_permutations_converge), from every valid consistent state.
	for _, s0 := range allValidStarts(m, hub, node) {
		s0 = m.Normalize(s0)
		x := m.Apply(m.Apply(s0, node, "g1"), node, "g2")
		y := m.Apply(m.Apply(s0, node, "g2"), node, "g1")
		if m.Of(x, node).ID() != m.Of(y, node).ID() {
			t.Fatalf("FedMachine orders diverge from %v: %s vs %s", s0, m.Of(x, node), m.Of(y, node))
		}
	}
}

// TestProjection_RequireRejects: with RequireProjectionSafe the same federation is a build
// error naming the target, event, state and both results.
func TestProjection_RequireRejects(t *testing.T) {
	f, _, _, _, _ := flipFederation()
	m, rep, err := f.RequireProjectionSafe().Build()
	if m != nil || err == nil {
		t.Fatalf("RequireProjectionSafe accepted a federation that fails XU (err %v)", err)
	}
	var pe *ProjectionOrderError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ProjectionOrderError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrProjectionNotCertified) {
		t.Fatalf("error does not wrap ErrProjectionNotCertified: %v", err)
	}
	if pe.Target != "node" || pe.Event != "g1" || pe.MergeFirst.ID() == pe.EventFirst.ID() {
		t.Fatalf("wrong failure: %+v", pe)
	}
	for _, want := range []string{`"flips"`, `"g1"`, `"node"`, "morphism hub→node", pe.State.String(),
		pe.Merged.String(), pe.MergeFirst.String(), pe.EventFirst.String(), pe.Source} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if rep == nil || rep.ProjectionSafe || len(rep.ProjectionWitnesses) != 1 || rep.Assurance != "" {
		t.Fatalf("report on the rejected build: %+v", rep)
	}
}

// TestProjection_C1C2Insufficient_Diverges is the divergence fed_grs_c1_c2_insufficient
// proves, on two target nodes running their own Machine. Both see g1 then g2 from the same
// start and the same source; node A merges the projection between them, node B only after.
// Once propagation completes they disagree on acc, so the federation does not converge under
// projection merging although every FedMachine order does.
func TestProjection_C1C2Insufficient_Diverges(t *testing.T) {
	f, hub, node, flag, acc := flipFederation()
	m, _, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	nm := m.Component(node)
	s0 := m.Normalize(m.NewState())
	p, err := m.SharedProjection(m.Of(s0, hub), hub, node)
	if err != nil {
		t.Fatal(err)
	}
	merge := func(s State) State {
		out, err := nm.MergeProjection(s, p)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	a := m.Of(s0, node)
	a = nm.Apply(a, "g1")
	a = merge(a) // the projection arrives between the events
	a = nm.Apply(a, "g2")
	a = merge(a) // propagation completes

	b := m.Of(s0, node)
	b = nm.Apply(b, "g1")
	b = nm.Apply(b, "g2") // applied before the projection arrives: reads flag = true
	b = merge(b)          // propagation completes

	if a.GetBool(flag) || b.GetBool(flag) {
		t.Fatalf("both nodes should end with the source's image flag = false: %s, %s", a, b)
	}
	if a.GetBool(acc) || !b.GetBool(acc) {
		t.Fatalf("want node A acc=false and node B acc=true (the gg normal forms), got %s and %s", a, b)
	}
	if !nm.IsValid(a) || !nm.IsValid(b) {
		t.Fatalf("both nodes stay valid (M1), yet diverge: %s, %s", a, b)
	}
	// Node A agrees with the FedMachine run; node B does not.
	fs := m.Apply(m.Apply(s0, node, "g1"), node, "g2")
	if m.Of(fs, node).ID() != a.ID() || m.Of(fs, node).ID() == b.ID() {
		t.Fatalf("FedMachine gives %s; node A %s, node B %s", m.Of(fs, node), a, b)
	}

	// The same divergence through the generic runner: some word disagrees with the FedMachine.
	evs := []dAct{{event: "g1"}, {event: "g2"}}
	diverged := false
	eachWord(evs, func(w []dAct) {
		_, got := runDistributed(t, m, hub, node, s0, w)
		_, want := fedRun(m, hub, node, s0, w)
		if got.ID() != want.ID() {
			diverged = true
		}
	})
	if !diverged {
		t.Fatal("no interleaving of events and merges diverged from the FedMachine run")
	}
}

// TestProjection_SupplyInstanceCertified: supply_instance satisfies XU (its unlist writes the
// shared flag, but the next merge overwrites it and nothing reads it), so the report says
// projection merging is certified, RequireProjectionSafe accepts it, and every interleaving of
// local events and merges, from every valid start (consistent or not), ends where the FedMachine
// run of its events does (propagation_flush), so all of them agree (dist_interleavings_converge).
func TestProjection_SupplyInstanceCertified(t *testing.T) {
	f, mfr, sup, _, _ := supplyFederation()
	m, rep, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ProjectionSafe || len(rep.ProjectionWitnesses) != 0 {
		t.Fatalf("supply_instance should be certified: %q", rep.ProjectionLine)
	}
	if last := rep.Checks[len(rep.Checks)-1]; !strings.Contains(last, "certified (XU)") || strings.Contains(last, "not certified") {
		t.Fatalf("Checks does not say certified (XU): %q", last)
	}
	f2, _, _, _, _ := supplyFederation()
	if _, _, err := f2.RequireProjectionSafe().Build(); err != nil {
		t.Fatalf("RequireProjectionSafe rejected supply_instance: %v", err)
	}

	evs := []dAct{{src: true, event: "recall"}, {event: "sell"}, {event: "unlist"}, {event: "sell"}}
	runs := 0
	for _, s0 := range allValidStarts(m, mfr, sup) {
		results := map[string]bool{}
		eachWord(evs, func(w []dAct) {
			gs, gd := runDistributed(t, m, mfr, sup, s0, w)
			ws, wd := fedRun(m, mfr, sup, s0, w)
			if gs.ID() != ws.ID() || gd.ID() != wd.ID() {
				t.Fatalf("from %v, word %v: distributed %s %s, FedMachine %s %s", s0, w, gs, gd, ws, wd)
			}
			results[gs.String()+" "+gd.String()] = true
			runs++
		})
		if len(results) != 1 {
			t.Fatalf("from %v the interleavings reach %d different states: %v", s0, len(results), results)
		}
	}
	if runs == 0 {
		t.Fatal("no runs")
	}
}

// TestProjection_StaticWitnessUnreachable: XU is static, so a witness can be a state a given
// deployment never reaches. In the levels federation bump fires only at level = 3, which no
// source maps to and no target event writes: Build accepts it, the report is not certified
// (witness at level = 3), and yet every interleaving from a consistent start converges.
func TestProjection_StaticWitnessUnreachable(t *testing.T) {
	src := NewRegistry("src")
	on := src.Bool("on")
	src.Event("flip").Writes(on).Apply(func(s State) State { return s.SetBool(on, true) }).Add()
	dst := NewRegistry("dst")
	level := dst.Int("level", 0, 3)
	count := dst.Int("count", 0, 2)
	dst.Event("bump").Writes(count).
		Guard(func(s State) bool { return s.GetInt(level) == 3 }).
		Apply(func(s State) State { return s.SetInt(count, s.GetInt(count)+1) }).Add()
	f := NewFederation("levels")
	f.Morphism(src, dst).Shared(level).
		Map(func(a, b State) State {
			if a.GetBool(on) {
				return b.SetInt(level, 1)
			}
			return b.SetInt(level, 0)
		}).Add()
	m, rep, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	if rep.ProjectionSafe || len(rep.ProjectionWitnesses) != 1 || rep.ProjectionWitnesses[0].State.GetInt(level) != 3 {
		t.Fatalf("want a not-certified report with a witness at level = 3: %q", rep.ProjectionLine)
	}
	s0 := m.Normalize(m.NewState())
	evs := []dAct{{src: true, event: "flip"}, {event: "bump"}}
	eachWord(evs, func(w []dAct) {
		_, gd := runDistributed(t, m, src, dst, s0, w)
		_, wd := fedRun(m, src, dst, s0, w)
		if gd.ID() != wd.ID() {
			t.Fatalf("word %v diverges from a consistent start: %s vs %s", w, gd, wd)
		}
	})
}

// TestProjection_StructuralReasons: a cyclic network and a multi-source target are reported as
// not certified (the theorem is for acyclic networks, and on a cycle nodes without a shared reset
// can settle on a ghost fixed point, dist_cyc_ghost; SharedProjection does not send the
// resolver's merge), and RequireProjectionSafe makes each a build error wrapping
// ErrProjectionNotCertified.
func TestProjection_StructuralReasons(t *testing.T) {
	cyc := func() *Federation {
		f, _, _, _ := alarmCycle(func(*Registry, Var, Var) {})
		return f
	}
	cases := []struct {
		name string
		fed  func() *Federation
		want string
	}{
		{"cyclic", cyc, "the network is cyclic"},
		{"cyclic ghost", cyc, "can settle on a larger fixed point than the FedMachine's least one (dist_cyc_ghost"},
		{"multi-source", func() *Federation { return resolvedDoor(false) }, `multi-source target(s) ["door"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, rep, err := c.fed().Build()
			if err != nil {
				t.Fatal(err)
			}
			if rep.ProjectionSafe || len(rep.ProjectionWitnesses) != 0 || !strings.Contains(rep.ProjectionLine, c.want) {
				t.Fatalf("want not certified for %q, no witness: %q", c.want, rep.ProjectionLine)
			}
			_, _, err = c.fed().RequireProjectionSafe().Build()
			if !errors.Is(err, ErrProjectionNotCertified) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("RequireProjectionSafe: want ErrProjectionNotCertified naming %q, got %v", c.want, err)
			}
			var pe *ProjectionOrderError
			if errors.As(err, &pe) {
				t.Fatalf("structural reason reported as an XU witness: %v", err)
			}
		})
	}
}
