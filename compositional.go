package gsm

import "fmt"

// Compositional (footprint-local) verification.
//
// Build verifies WFC and CC by enumerating the whole state space, which caps it
// at small machines. But the convergence conditions are LOCAL: an invariant's
// repair and an event's effect touch only their declared footprint, and events
// with disjoint footprints commute (proved: normalization-confluence coq/Gsm.v,
// disjoint_events_commute). So a machine splits into footprint-connected
// COMPONENTS that do not interact, and it suffices to verify each component over
// the subspace of ITS OWN variables. Certification cost is then exponential in
// the largest component, not in the whole machine.
//
// Trust model: this relies on the declared footprints being honest (an
// invariant's repair modifies only its Watches() vars and reads only those; an
// event's effect touches only its Writes() vars). Build's disjointness path
// already assumes exactly this; BuildCompositional extends it to WFC and to the
// brute-force CC path. A component is verified with all other variables held at
// zero, so BuildCompositional requires the zero state to be valid.

// maxComponentBits caps a single component's subspace so enumeration stays cheap.
const maxComponentBits = 20

// unionFind is a tiny disjoint-set over variable indices.
type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{p}
}

func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b int) { u.parent[u.find(a)] = u.find(b) }

// component is one footprint-connected group: its variable indices, the
// invariants whose footprint lies in it, and the events that write into it.
type component struct {
	vars       []int
	invariants []int
	events     []int
}

// buildComponents partitions variables into footprint-connected components: all
// variables inside one invariant footprint are linked, all variables inside one
// event write-set are linked, and an event's writes are linked to the footprints
// of the invariants they overlap (the event can trigger those invariants).
func (r *Registry) buildComponents() []component {
	uf := newUnionFind(len(r.vars))
	for _, inv := range r.invariants {
		for k := 1; k < len(inv.footprint); k++ {
			uf.union(inv.footprint[0], inv.footprint[k])
		}
	}
	for _, ev := range r.events {
		for k := 1; k < len(ev.writes); k++ {
			uf.union(ev.writes[0], ev.writes[k])
		}
		for _, inv := range r.invariants {
			if overlaps(ev.writes, inv.footprint) && len(ev.writes) > 0 && len(inv.footprint) > 0 {
				uf.union(ev.writes[0], inv.footprint[0])
			}
		}
	}

	byRoot := map[int]*component{}
	get := func(root int) *component {
		c := byRoot[root]
		if c == nil {
			c = &component{}
			byRoot[root] = c
		}
		return c
	}
	for vi := range r.vars {
		c := get(uf.find(vi))
		c.vars = append(c.vars, vi)
	}
	for ii, inv := range r.invariants {
		if len(inv.footprint) == 0 {
			continue
		}
		c := get(uf.find(inv.footprint[0]))
		c.invariants = append(c.invariants, ii)
	}
	for ei, ev := range r.events {
		if len(ev.writes) == 0 {
			continue
		}
		c := get(uf.find(ev.writes[0]))
		c.events = append(c.events, ei)
	}

	out := make([]component, 0, len(byRoot))
	for _, c := range byRoot {
		out = append(out, *c)
	}
	return out
}

func overlaps(a, b []int) bool {
	set := map[int]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}

// BuildCompositional verifies WFC and CC per footprint component and returns a
// lazy Machine (event application and normalization computed at runtime, not from
// global tables). It certifies convergence for machines whose global state space
// is far too large for Build, provided every component is individually small.
//
// Preconditions (else it returns an error, and you should use Build): every
// invariant declares a footprint (Watches) and every event declares its writes
// (Writes); the zero state is valid; and no single component exceeds
// maxComponentBits.
func (r *Registry) BuildCompositional() (*Machine, *Report, error) {
	for _, inv := range r.invariants {
		if inv.repair == nil {
			return nil, nil, fmt.Errorf("gsm: invariant %q has no Repair", inv.name)
		}
		if len(inv.footprint) == 0 {
			return nil, nil, fmt.Errorf("gsm: invariant %q declares no footprint; call Watches(...) so it can be localized", inv.name)
		}
	}
	for _, ev := range r.events {
		if len(ev.writes) == 0 {
			return nil, nil, fmt.Errorf("gsm: event %q declares no writes; call Writes(...) so it can be localized", ev.name)
		}
	}

	zero := State{packed: 0, vars: r.vars}
	if !r.allInvariantsHold(zero) {
		return nil, nil, fmt.Errorf("gsm: zero state is not valid; compositional verification needs it (use Build)")
	}

	report := &Report{
		Name:       r.name,
		VarCount:   len(r.vars),
		EventCount: len(r.events),
	}
	comps := r.buildComponents()
	report.Components = len(comps)

	maxRepair := 0
	pairsDisjoint, pairsBrute := 0, 0

	// Which event pairs must be checked for CC (mirrors verifyCC).
	type pair struct{ i, j int }
	var pairsToCheck []pair
	if r.allIndependent {
		for i := 0; i < len(r.events); i++ {
			for j := i + 1; j < len(r.events); j++ {
				pairsToCheck = append(pairsToCheck, pair{i, j})
			}
		}
	} else {
		for _, p := range r.independent {
			i, j := p[0], p[1]
			if i > j {
				i, j = j, i
			}
			pairsToCheck = append(pairsToCheck, pair{i, j})
		}
	}

	// Cross-component (footprint-disjoint) pairs commute by construction.
	inSameComponent := r.componentIndexOfEvent(comps)
	var localPairs []pair
	for _, p := range pairsToCheck {
		if r.eventsDisjoint(p.i, p.j) {
			pairsDisjoint++
			continue
		}
		if inSameComponent[p.i] != inSameComponent[p.j] || inSameComponent[p.i] < 0 {
			// Not footprint-disjoint yet not co-located: cannot localize safely.
			return nil, report, fmt.Errorf("gsm: events %q and %q share footprint across components; cannot localize (use Build)",
				r.events[p.i].name, r.events[p.j].name)
		}
		pairsBrute++
		localPairs = append(localPairs, p)
	}

	for ci := range comps {
		c := &comps[ci]
		bits, count := r.componentSize(c)
		if bits > maxComponentBits {
			return nil, report, fmt.Errorf("gsm: component with vars %v needs %d bits (max %d); refactor into smaller footprints",
				c.vars, bits, maxComponentBits)
		}
		if count > report.MaxComponentStates {
			report.MaxComponentStates = count
		}

		// WFC: repair terminates from every sub-state of this component.
		depth, err := r.verifyComponentWFC(c, count)
		if err != nil {
			report.WFC = false
			return nil, report, err
		}
		if depth > maxRepair {
			maxRepair = depth
		}

		// CC: brute-force the local pairs whose shared component is this one.
		for _, p := range localPairs {
			if inSameComponent[p.i] != ci {
				continue
			}
			if err := r.verifyComponentCC(c, p.i, p.j, report); err != nil {
				return nil, report, err
			}
		}
	}

	report.WFC = true
	report.CC = true
	report.MaxRepairLen = maxRepair
	report.PairsDisjoint = pairsDisjoint
	report.PairsBrute = pairsBrute
	report.PairsTotal = pairsDisjoint + pairsBrute

	m := &Machine{
		name:       r.name,
		vars:       r.vars,
		events:     make(map[string]int),
		lazy:       true,
		invariants: r.invariants,
		eventDefs:  r.events,
	}
	for i, ev := range r.events {
		m.events[ev.name] = i
	}
	return m, report, nil
}

// componentIndexOfEvent maps each event index to the component index it belongs
// to (by its write-set), or -1 if it writes nothing.
func (r *Registry) componentIndexOfEvent(comps []component) []int {
	out := make([]int, len(r.events))
	for i := range out {
		out[i] = -1
	}
	for ci := range comps {
		for _, ei := range comps[ci].events {
			out[ei] = ci
		}
	}
	return out
}

// componentSize returns (total bits, state count) of a component's subspace.
func (r *Registry) componentSize(c *component) (int, int) {
	bits, count := 0, 1
	for _, vi := range c.vars {
		bits += int(r.vars[vi].bits)
		count *= r.vars[vi].domain
	}
	return bits, count
}

// enumComponent calls fn for every assignment of the component's variables, with
// all other variables held at zero.
func (r *Registry) enumComponent(c *component, fn func(State)) {
	var rec func(k int, s State)
	rec = func(k int, s State) {
		if k == len(c.vars) {
			fn(s)
			return
		}
		v := r.vars[c.vars[k]]
		for d := 0; d < v.domain; d++ {
			rec(k+1, s.setRaw(v, uint64(d)))
		}
	}
	rec(0, State{packed: 0, vars: r.vars})
}

// verifyComponentWFC checks that repair terminates from every sub-state; returns
// the deepest repair chain seen. With other variables at zero (valid), only this
// component's invariants can fire.
func (r *Registry) verifyComponentWFC(c *component, count int) (int, error) {
	maxDepth := 0
	var outerErr error
	r.enumComponent(c, func(s State) {
		if outerErr != nil {
			return
		}
		seen := map[uint64]bool{s.packed: true}
		depth := 0
		for !r.allInvariantsHold(s) {
			s = r.applyFirstRepair(s)
			depth++
			if seen[s.packed] || depth > count {
				outerErr = fmt.Errorf("gsm: WFC failed in component %v; compensation does not terminate", c.vars)
				return
			}
			seen[s.packed] = true
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	})
	return maxDepth, outerErr
}

// verifyComponentCC brute-forces CC for one event pair over the component
// subspace. localApply stays within the subspace (writes and repairs are
// footprint-local), so this is sound.
func (r *Registry) verifyComponentCC(c *component, i, j int, report *Report) error {
	localApply := func(ev eventDef, s State) State {
		after := s
		if ev.guard == nil || ev.guard(s) {
			after = ev.effect(s)
		}
		after = r.clampState(after)
		for !r.allInvariantsHold(after) {
			after = r.applyFirstRepair(after)
		}
		return after
	}
	var ccErr error
	r.enumComponent(c, func(s State) {
		if ccErr != nil {
			return
		}
		ij := localApply(r.events[j], localApply(r.events[i], s))
		ji := localApply(r.events[i], localApply(r.events[j], s))
		if ij.packed != ji.packed {
			report.CC = false
			report.CCFailure = &CCFailure{
				Event1: r.events[i].name, Event2: r.events[j].name,
				State: s, Result1: ij, Result2: ji,
			}
			ccErr = fmt.Errorf("gsm: Compensation Commutativity (CC) failed for (%q, %q)",
				r.events[i].name, r.events[j].name)
		}
	})
	return ccErr
}
