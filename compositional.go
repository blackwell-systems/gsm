package gsm

import (
	"fmt"
	"sort"
)

// Compositional (footprint-local) verification.
//
// Build verifies WFC and CC by enumerating the whole state space, which caps it
// at small machines. But the convergence conditions are LOCAL: an invariant's
// repair and an event's effect touch only their declared footprint, so a machine
// splits into footprint-connected COMPONENTS (sets of state variables) that do not
// interact, and it suffices to verify each component over the subspace of ITS OWN
// variables. Certification cost is then exponential in the largest component, not
// in the whole machine.
//
// Why a cross-component pair needs no check. Two events in different components
// read and write disjoint state variables, so their raw effects commute
// (normalization-confluence coq/Gsm.v, disjoint_events_commute), and normalization
// acts on each component separately. CC1 for the pair then holds at every VALID
// state (coq/Calculus.v, calc_components_cc1_valid). It need not hold at an invalid
// state: there it holds iff each event absorbs its own component's repair
// (calc_components_cc1_iff), which an event may not do. gsm never reaches that case,
// because Machine.Apply normalizes an invalid input before applying the event, so
// every order of events starts from a valid state. The disjointness here is of state
// variables. Disjoint INVARIANT footprints (the sets of invariants an event can
// affect) with repair locality are not enough for CC1 (coq/Calculus.v,
// base_thm_footprint_cc1_refuted), and gsm does not rely on them: an invariant whose
// footprint meets an event's writes is in that event's component.
//
// Footprint conformance is checked before the disjointness certificate is used:
// verifyFootprints (footprint.go) confirms that each event (guard and effect)
// reads and writes only its declared write set, and each invariant check and
// repair only its footprint. That is the precondition disjoint_events_commute
// assumes (an event reads only its own footprint). For combinator rules the check
// is syntactic and exact; for closures it is a perturbation test that detects
// dependence on any one or two outside variables but not a joint dependence on
// three or more (see footprint.go). BuildCompositional therefore accepts closure
// rules only with the TrustClosureFootprints option, and then reports
// AssuranceOracleComponentsTested. A component is verified with all other
// variables held at zero, so BuildCompositional requires the zero state to be valid.

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
	// Map order is random; order components by their lowest variable index (every
	// component has a variable, appended in index order) so verification, and so the
	// first failure it reports, is the same on every run.
	sort.Slice(out, func(i, j int) bool { return out[i].vars[0] < out[j].vars[0] })
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
// Like Build, it rejects any effect or repair result it computes that is not a state of
// this machine (see EffectFunc). The returned machine runs the rules again at Apply time
// and panics on such a result (see Machine.Apply), or on a repair chain longer than the
// verified bound (see Machine.Normalize).
//
// Preconditions (else it returns an error, and you should use Build): every
// invariant declares a footprint (Watches) and every event declares its writes
// (Writes); the zero state is valid; every variable's range fits its bit field; and
// no single component exceeds maxComponentBits.
//
// Closure rules need an opt-in. A combinator rule's footprint is checked exactly,
// from its expression tree. A Go closure is opaque, so its footprint can only be
// tested by perturbation (see footprint.go), which misses a closure that depends
// jointly on three or more outside variables. By default BuildCompositional
// therefore returns an error naming the first closure rule; pass
// TrustClosureFootprints() to accept a tested footprint for closures, in which case
// Report.Assurance is AssuranceOracleComponentsTested.
func (r *Registry) BuildCompositional(opts ...CompositionalOption) (*Machine, *Report, error) {
	var o compositionalOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	m, rep, err := r.buildCompositional(o)
	if err == nil && machineObserver != nil {
		machineObserver("compositional", r, m)
	}
	return m, rep, err
}

// CompositionalOption configures BuildCompositional.
type CompositionalOption func(*compositionalOptions)

type compositionalOptions struct {
	trustClosures bool
}

// TrustClosureFootprints lets BuildCompositional certify a machine whose rules
// include Go closures (Holds/Repair/Apply/Guard rather than the combinator
// vocabulary). The caller accepts that each closure reads and writes only its
// declared footprint (Writes for an event, Watches for an invariant) and is
// deterministic. gsm tests this by perturbation: from every state of the closure's
// component it changes each outside variable, and each pair of outside variables,
// to every other value. That catches a dependence on one or two outside variables
// but not a joint dependence on three or more, so for closures footprint
// conformance is tested, not proved. The report says so: Report.Assurance is
// AssuranceOracleComponentsTested. Build checks closures exactly, with no
// footprint assumption, for machines small enough to enumerate.
func TrustClosureFootprints() CompositionalOption {
	return func(o *compositionalOptions) { o.trustClosures = true }
}

// firstClosureRule names the first rule whose footprint BuildCompositional cannot
// check syntactically (a closure), or returns "" when every rule is a combinator rule.
func (r *Registry) firstClosureRule() string {
	for _, ev := range r.events {
		if !ev.syntactic() {
			return fmt.Sprintf("event %q", ev.name)
		}
	}
	for _, inv := range r.invariants {
		if !inv.syntactic() {
			return fmt.Sprintf("invariant %q", inv.name)
		}
	}
	return ""
}

// checkDomains rejects a variable whose domain does not fit its bit field. Int
// computes max - min + 1 in int, so a range such as Int(0, math.MaxInt) overflows
// to a negative domain; the perturbation and enumeration loops would then run for
// 2^63 iterations instead of failing.
func (r *Registry) checkDomains() error {
	for _, v := range r.vars {
		if v.domain < 2 || v.bits > 63 || uint64(v.domain) > uint64(1)<<v.bits {
			return fmt.Errorf("gsm: variable %q has an invalid domain (%d values in %d bits): its range is too wide "+
				"(max - min + 1 overflows int); declare a smaller range", v.name, v.domain, v.bits)
		}
	}
	return nil
}

func (r *Registry) buildCompositional(o compositionalOptions) (_ *Machine, rep *Report, err error) {
	if err = r.checkNames(); err != nil {
		return nil, nil, err
	}
	before := r.shape()
	// A rule that declares on the registry mid-run can also make a later check fail (a
	// result built after a declaration has the new variable list, so it is not a state of
	// the machine being verified). The change is the cause, so report it instead.
	defer func() {
		if err != nil {
			if gerr := r.checkUnchanged(before); gerr != nil {
				err = gerr
			}
			rep.noteDomainViolation(err)
		}
	}()
	if err = r.checkDomains(); err != nil {
		return nil, nil, err
	}
	closure := r.firstClosureRule()
	if closure != "" && !o.trustClosures {
		return nil, nil, fmt.Errorf("gsm: %s is a Go closure; BuildCompositional checks only combinator rules exactly "+
			"(a closure's footprint can only be tested by perturbation, which misses a joint dependence on three or "+
			"more outside variables). Write the rule with combinators, use Build (exact for any rule), or pass "+
			"gsm.TrustClosureFootprints() to accept a tested footprint", closure)
	}
	// State packs every variable into one uint64. A variable placed past bit 64
	// reads as 0 and ignores writes, so a machine that wide cannot be represented,
	// let alone certified (it used to be certified with those variables frozen).
	if r.totalBits > 64 {
		return nil, nil, fmt.Errorf("gsm: machine needs %d bits of state; State holds at most 64", r.totalBits)
	}
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
	repairBound := 0 // sum over components of the deepest repair chain (see Machine.Normalize)
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

	// Cross-component pairs commute from every valid state by construction
	// (calc_components_cc1_valid; see the comment at the top of this file), provided
	// every closure respects its declared footprint (verified per component below; any
	// violation aborts the build, so no cross-component pair is trusted unless every
	// component passed). Apply normalizes an invalid input first, so no order starts
	// anywhere else.
	//
	// The test is component membership, not overlap of per-event footprints. A
	// component is closed under everything that can link two events: shared write
	// variables, an invariant footprint overlapping an event's writes, and chains of
	// overlapping invariant footprints through which one event's repair cascade can
	// reach another event's variables. Two events in different components therefore
	// read and write disjoint variables, and so do all the repairs they can trigger.
	// Two events in the same component are checked exhaustively over that component.
	compOf := r.componentIndexOfEvent(comps)
	var localPairs []pair
	for _, p := range pairsToCheck {
		if compOf[p.i] != compOf[p.j] {
			pairsDisjoint++
			continue
		}
		pairsBrute++
		localPairs = append(localPairs, p)
	}

	// Size every component, and the footprint test of every closure rule, before
	// running any check, so a machine too large to verify fails at once rather than
	// after the smaller components (or never, for a perturbation of 2^40 cases).
	for ci := range comps {
		c := &comps[ci]
		bits, count := r.componentSize(c)
		if bits > maxComponentBits {
			return nil, report, fmt.Errorf("gsm: component with vars %v needs %d bits (max %d); refactor into smaller footprints",
				c.vars, bits, maxComponentBits)
		}
		if err := r.checkPerturbationCost(c, count); err != nil {
			return nil, report, err
		}
	}

	for ci := range comps {
		c := &comps[ci]
		_, count := r.componentSize(c)
		if count > report.MaxComponentStates {
			report.MaxComponentStates = count
		}

		// Footprint conformance: the closures actually respect their declared
		// footprints, which the disjointness certificate depends on.
		if err := r.verifyFootprints(c); err != nil {
			report.FootprintViolation = err.Error()
			return nil, report, err
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
		repairBound += depth

		// CC: brute-force the local pairs whose shared component is this one.
		for _, p := range localPairs {
			if compOf[p.i] != ci {
				continue
			}
			if err := r.verifyComponentCC(c, p.i, p.j, report); err != nil {
				return nil, report, err
			}
		}
	}

	// The oracle gate: the verified table oracle must certify every component's
	// tables, or there is no machine.
	var local [][2]int
	for _, p := range localPairs {
		local = append(local, [2]int{p.i, p.j})
	}
	for ci := range comps {
		tb, terr := r.componentTables(&comps[ci], local)
		if terr == nil {
			terr = certifyTables(tb.Lookup(), fmt.Sprintf("the tables of component %v", comps[ci].vars))
		} else {
			terr = &oracleError{fmt.Sprintf("gsm: cannot give component %v to the verified table oracle: %v; not certified", comps[ci].vars, terr)}
		}
		if terr != nil {
			report.failClosed(terr)
			return nil, report, terr
		}
	}

	report.WFC = true
	report.CC = true
	report.Assurance = AssuranceOracleComponents
	if closure != "" {
		report.Assurance = AssuranceOracleComponentsTested
	}
	report.FootprintChecked = true
	report.MaxRepairLen = maxRepair
	report.PairsDisjoint = pairsDisjoint
	report.PairsBrute = pairsBrute
	report.PairsTotal = pairsDisjoint + pairsBrute

	if err := r.checkUnchanged(before); err != nil {
		return nil, report, err
	}
	m := &Machine{
		name:       r.name,
		vars:       r.vars,
		dom:        newDomainCheck(r.vars),
		events:     make(map[string]int),
		lazy:       true,
		invariants: r.invariants,
		eventDefs:  r.events,

		repairBound: repairBound,
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
	run := r.checked()
	r.enumComponent(c, func(s State) {
		if outerErr != nil {
			return
		}
		seen := map[uint64]bool{s.packed: true}
		depth := 0
		for !r.allInvariantsHold(s) {
			var err error
			if s, err = run.applyFirstRepair(s); err != nil {
				outerErr = err
				return
			}
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

// verifyComponentCC brute-forces CC for one event pair over the valid states of
// the component subspace (CC1 over valid states, the same domain Build checks; the
// zero state, which BuildCompositional requires to be valid, is among them).
// localApply stays within the subspace (writes and repairs are footprint-local,
// verified by verifyFootprints), so this is sound.
func (r *Registry) verifyComponentCC(c *component, i, j int, report *Report) error {
	// The result checks here never fire first: the footprint pass ran every effect, and the
	// footprint pass or WFC every repair, on every state of the component. They keep CC from
	// ever comparing an unchecked state if that coverage changes.
	run := r.checked()
	localApply := func(ev eventDef, s State) (State, error) {
		after, err := run.applyEvent(ev, s)
		for err == nil && !r.allInvariantsHold(after) {
			after, err = run.applyFirstRepair(after)
		}
		return after, err
	}
	twice := func(first, second eventDef, s State) (State, error) {
		mid, err := localApply(first, s)
		if err != nil {
			return mid, err
		}
		return localApply(second, mid)
	}
	var ccErr error
	r.enumComponent(c, func(s State) {
		if ccErr != nil || !r.allInvariantsHold(s) {
			return
		}
		ij, err := twice(r.events[i], r.events[j], s)
		if err != nil {
			ccErr = err
			return
		}
		ji, err := twice(r.events[j], r.events[i], s)
		if err != nil {
			ccErr = err
			return
		}
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

// lazyRepairBoundError is the panic message when a lazy machine's repairs from in have
// not reached a valid state after bound steps (now is the state reached, last the
// invariant repaired last).
//
// The bound is exact for the machine BuildCompositional verified. Each component's
// deepest repair chain was measured over every state of that component, and a repair
// in one component neither reads nor writes another component's variables, so from any
// state the repairs take at most the sum of those depths, whatever order the
// components' invariants fire in. A longer chain means a rule reads or writes outside
// its declared footprint, or is not deterministic, and the repairs may cycle forever.
func (m *Machine) lazyRepairBoundError(in, now State, last string, bound int) string {
	if m.abstract {
		return fmt.Sprintf("gsm: machine %q: repairs from %s did not reach a valid state within %d steps, the most "+
			"Build verified by abstraction for this machine (the deepest repair chain over the representatives, "+
			"which term_abs makes a bound for every value); a bug in gsm (state reached: %s, last repaired: "+
			"invariant %q)", m.name, in, bound, now, last)
	}
	return fmt.Sprintf("gsm: machine %q: repairs from %s did not reach a valid state within %d steps, the most "+
		"BuildCompositional verified for this machine (the sum of each component's deepest repair chain); "+
		"a rule reads or writes outside its declared footprint or is not deterministic, and the repairs may "+
		"cycle (state reached: %s, last repaired: invariant %q)", m.name, in, bound, now, last)
}
