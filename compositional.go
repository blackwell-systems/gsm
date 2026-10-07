package gsm

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// Compositional (footprint-local) verification.
//
// The theory is normalization-confluence coq/CompositionalCheck.v (docs/theory.md §11.10).
// Every rule reads and writes a known set of variables, its footprint. The variables
// split into footprint COMPONENTS: union-find links every variable of every rule's
// footprint, so each rule's footprint lies in one component. A component is checked
// over its own subspace (its variables, every other variable at zero), and the result
// is exact for the whole registry:
//
//   - WFC: repair terminates from every state iff it does in every component from
//     every state of its subspace (wfc_iff, term_iff). The machine's repair bound is the
//     sum of the components' deepest chains (bound_sum).
//   - CC1 for two events in different components holds at every valid state with no
//     check (cc1_cross; their raw effects commute, raw_cross_commute). It need not hold
//     at an invalid state (coq/Calculus.v, calc_components_cc1_iff); gsm never applies
//     an event to one, because Machine.Apply normalizes an invalid input first.
//   - CC1 for two events in the same component holds at the valid states of the
//     registry iff at the valid states of the component's subspace (cc1_same_iff,
//     cc1_component).
//   - So gsm's guarantee (every order of the same events from a valid state reaches one
//     state, which is exactly CC1 on the valid states, conv_iff_cc1) holds for the
//     registry iff it holds in every component (compositional_exact,
//     compositional_gsm). A pass is the guarantee, and a failure in a component is a
//     failure of the registry.
//   - gsm checks a component with the registry's own rules, every other variable held
//     at zero, which needs a valid zero state: with no shared variables those rules on
//     the subspace are the component's (gsm_literal, gsm_literal_step).
//
// gsm checks WFC and CC1 on valid states, not CC2 (docs/theory.md §6.5), and so does
// this. The disjointness is of state variables, reads included: a rule whose read is
// missing from its footprint breaks the reduction (ws_diverges), so an event's footprint
// is its guard's reads, its effect's reads and its writes, and an invariant's is its
// check's reads and its repair's reads and writes. A repair that writes outside its
// component breaks it too (rc_diverges), and so does a written variable shared by two
// components (sw_diverges); gsm has no shared variables, since a read merges the reader's
// component with the writer's (inside_merge).
//
// Footprints. For combinator rules they are derived from the expression trees, which is
// exact (ast_event_local, ast_check_local, ast_repair_local; ast_hyps for the component
// assignment). A Go closure is opaque: its footprint is its declaration (an event's
// Writes, which it must also read within; an invariant's Watches), and verifyFootprints
// (footprint.go) tests it by perturbation, which detects a dependence on one or two
// outside variables but not a joint dependence on three or more. That test is the trust
// boundary: BuildCompositional accepts closure rules only with TrustClosureFootprints and
// then reports AssuranceOracleComponentsTested, and Build never decomposes a closure
// rule (it checks such a registry globally, exactly).

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

// component is one footprint-connected group: its variable indices (ascending), the
// invariants whose footprint lies in it, and the events whose footprint lies in it (both
// in declaration order).
type component struct {
	vars       []int
	invariants []int
	events     []int
}

// compPlan is the footprint decomposition of a registry.
type compPlan struct {
	comps []component
	// evComp is each event's component, or -1 for an event whose footprint is empty: a
	// combinator event that reads and writes nothing is the identity, which commutes with
	// every event (it lies inside any component, and cc1_cross applies to it).
	evComp []int
	// closure names the first closure rule (`event "x"`), or is "" when every rule is a
	// combinator rule and every footprint was derived from an expression tree.
	closure string
	// The checked pairs and the pair mode, taken with the plan, before any rule runs: a
	// rule that declares on the registry mid-run is reported by checkUnchanged, and the
	// check must not index the events it added.
	declared [][2]int
	allPairs bool
}

// exprForeign returns why e cannot give an exact footprint for r (it mentions a variable
// of another registry, or a node gsm cannot inspect), or "".
func (r *Registry) exprForeign(e Expr) string {
	switch x := e.(type) {
	case varRef:
		if !r.owns(x.v) {
			return fmt.Sprintf("mentions variable %q, which is not a variable of this registry", x.v.name)
		}
		return ""
	case litE:
		return ""
	case binE:
		if why := r.exprForeign(x.a); why != "" {
			return why
		}
		return r.exprForeign(x.b)
	}
	return fmt.Sprintf("uses an expression gsm cannot inspect (%T)", e)
}

// predForeign is exprForeign for a predicate.
func (r *Registry) predForeign(p Pred) string {
	switch x := p.(type) {
	case cmpP:
		if why := r.exprForeign(x.a); why != "" {
			return why
		}
		return r.exprForeign(x.b)
	case boolP:
		for _, q := range x.ps {
			if why := r.predForeign(q); why != "" {
				return why
			}
		}
		return ""
	case notP:
		return r.predForeign(x.p)
	}
	return fmt.Sprintf("uses a predicate gsm cannot inspect (%T)", p)
}

// transformForeign is exprForeign for a transform.
func (r *Registry) transformForeign(t Transform) string {
	for _, a := range t {
		if !r.owns(a.v) {
			return fmt.Sprintf("writes variable %q, which is not a variable of this registry", a.v.name)
		}
		if why := r.exprForeign(a.e); why != "" {
			return why
		}
	}
	return ""
}

// eventFootprint returns the variables event ev reads or writes, for the component
// computation. A combinator event's footprint is derived from its trees: the guard's
// reads (readsP), the effect's reads (readsT) and its writes (writesT), as the theory's
// read set of a guarded event includes its write set. A closure event's footprint is its
// declared Writes, within which it must also read (tested by perturbation). why is
// non-empty when no footprint can be given.
func (r *Registry) eventFootprint(ev eventDef) (fp []int, why string) {
	if !ev.syntactic() {
		if len(ev.writes) == 0 {
			return nil, fmt.Sprintf("gsm: event %q declares no writes; call Writes(...) so it can be localized", ev.name)
		}
		return dedup(ev.writes), ""
	}
	if ev.guardAST != nil {
		if w := r.predForeign(ev.guardAST); w != "" {
			return nil, fmt.Sprintf("gsm: event %q guard %s", ev.name, w)
		}
	}
	if w := r.transformForeign(ev.effectAST); w != "" {
		return nil, fmt.Sprintf("gsm: event %q %s", ev.name, w)
	}
	var guard []int
	if ev.guardAST != nil {
		guard = ev.guardAST.vars()
	}
	return dedup(guard, ev.effectAST.readVars(), ev.effectAST.writeVars()), ""
}

// invariantFootprint returns the variables invariant inv's check and repair read or
// write. A combinator invariant's footprint is derived from its trees (the check's reads,
// the repair's reads and writes); a closure invariant's is its declared Watches.
func (r *Registry) invariantFootprint(inv invariantDef) (fp []int, why string) {
	if !inv.syntactic() {
		if len(inv.footprint) == 0 {
			return nil, fmt.Sprintf("gsm: invariant %q declares no footprint; call Watches(...) so it can be localized", inv.name)
		}
		return dedup(inv.footprint), ""
	}
	if w := r.predForeign(inv.predAST); w != "" {
		return nil, fmt.Sprintf("gsm: invariant %q %s", inv.name, w)
	}
	if w := r.transformForeign(inv.repairAST); w != "" {
		return nil, fmt.Sprintf("gsm: invariant %q repair %s", inv.name, w)
	}
	fp = dedup(inv.predAST.vars(), inv.repairAST.readVars(), inv.repairAST.writeVars())
	if len(fp) == 0 {
		return nil, fmt.Sprintf("gsm: invariant %q reads and writes no variable, so it lies in no footprint component", inv.name)
	}
	return fp, ""
}

// planComponents partitions the variables into footprint components by union-find over
// every rule's footprint (reads and writes), and assigns each rule its component. It
// returns why the registry cannot be decomposed, naming the rule, when a footprint
// cannot be given. The variable indices must be valid (checkNames).
func (r *Registry) planComponents() (*compPlan, string) {
	p := &compPlan{evComp: make([]int, len(r.events)), closure: r.firstClosureRule(), declared: r.ccPairs(),
		allPairs: r.allIndependent}
	uf := newUnionFind(len(r.vars))
	link := func(fp []int) {
		for k := 1; k < len(fp); k++ {
			uf.union(fp[0], fp[k])
		}
	}
	evFP := make([][]int, len(r.events))
	for ei, ev := range r.events {
		fp, why := r.eventFootprint(ev)
		if why != "" {
			return nil, why
		}
		evFP[ei] = fp
		link(fp)
	}
	invFP := make([][]int, len(r.invariants))
	for ii, inv := range r.invariants {
		fp, why := r.invariantFootprint(inv)
		if why != "" {
			return nil, why
		}
		invFP[ii] = fp
		link(fp)
	}

	byRoot := map[int]int{}
	for vi := range r.vars {
		root := uf.find(vi)
		ci, ok := byRoot[root]
		if !ok {
			ci = len(p.comps)
			byRoot[root] = ci
			p.comps = append(p.comps, component{})
		}
		p.comps[ci].vars = append(p.comps[ci].vars, vi)
	}
	// Variables are visited in index order, so components are ordered by their lowest
	// variable and verification (and the first failure it reports) is the same every run.
	for ii := range r.invariants {
		ci := byRoot[uf.find(invFP[ii][0])]
		p.comps[ci].invariants = append(p.comps[ci].invariants, ii)
	}
	for ei := range r.events {
		if len(evFP[ei]) == 0 {
			p.evComp[ei] = -1
			continue
		}
		ci := byRoot[uf.find(evFP[ei][0])]
		p.evComp[ei] = ci
		p.comps[ci].events = append(p.comps[ci].events, ei)
	}
	return p, ""
}

// BuildCompositional verifies WFC and CC per footprint component and returns a
// lazy Machine (event application and normalization computed at runtime, not from
// global tables). It certifies convergence for machines whose global state space
// is far too large for Build, provided every component is individually small.
//
// Build already does this by default for a registry too large to enumerate whose rules
// are combinators (see Build and Report.Compositional). BuildCompositional runs the
// per-component check whatever the registry's size, with one component or many, and
// accepts closure rules with TrustClosureFootprints.
//
// Like Build, it rejects any effect or repair result it computes that is not a state of
// this machine (see EffectFunc). The returned machine runs the rules again at Apply time
// and panics on such a result (see Machine.Apply), or on a repair chain longer than the
// verified bound (see Machine.Normalize).
//
// Preconditions (else it returns an error, and you should use Build): every closure
// invariant declares a footprint (Watches) and every closure event its writes (Writes);
// a combinator rule's footprint is derived from its expression trees, reads included;
// the zero state is valid; every variable's range fits its bit field; and no single
// component exceeds 20 bits.
//
// Closure rules need an opt-in. A combinator rule's footprint is derived exactly,
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
	rep.setRegime(err == nil && m != nil)
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

// firstViolated returns the index of the first invariant s violates, or -1.
func (r *Registry) firstViolated(s State) int {
	for i, inv := range r.invariants {
		if !inv.check(s) {
			return i
		}
	}
	return -1
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
	}
	plan, why := r.planComponents()
	if why != "" {
		return nil, nil, fmt.Errorf("%s", why)
	}
	zero := State{packed: 0, vars: r.vars}
	if !r.allInvariantsHold(zero) {
		return nil, nil, fmt.Errorf("gsm: zero state is not valid; compositional verification needs it (use Build)")
	}
	return r.checkComponents(plan, before)
}

// componentNames renders a component's variables by name, for messages.
func (r *Registry) componentNames(c *component) string {
	names := make([]string, len(c.vars))
	for i, vi := range c.vars {
		names[i] = r.vars[vi].name
	}
	return "{" + strings.Join(names, ", ") + "}"
}

// checkComponents runs the per-component check on plan p: footprint conformance, WFC
// and the repair bound per component, CC1 for every checked pair whose events share a
// component (cross-component pairs need none, cc1_cross), the undeclared pairs and
// idempotence the same way, and the verified table oracle on every component's tables.
// The caller has checked the preconditions (names, domains, at most 64 bits, a Repair on
// every invariant, a valid zero state). It returns a lazy machine.
func (r *Registry) checkComponents(p *compPlan, before registryShape) (*Machine, *Report, error) {
	report := &Report{
		Name:       r.name,
		VarCount:   len(r.vars),
		EventCount: len(r.events),
		Components: len(p.comps),
	}
	comps := p.comps

	// Size every component, and the footprint test of every closure rule, before
	// running any check, so a machine too large to verify fails at once rather than
	// after the smaller components (or never, for a perturbation of 2^40 cases).
	for ci := range comps {
		c := &comps[ci]
		bits, count := r.componentSize(c)
		if bits > maxComponentBits {
			return nil, report, fmt.Errorf("gsm: component %s needs %d bits (max %d); refactor into smaller footprints",
				r.componentNames(c), bits, maxComponentBits)
		}
		if err := r.checkPerturbationCost(c, count); err != nil {
			return nil, report, err
		}
	}
	report.Compositional = r.reduction(p)

	// Footprint conformance: the closures respect their declared footprints, which the
	// decomposition depends on. Combinator footprints are derived from the trees.
	for ci := range comps {
		if err := r.verifyFootprints(&comps[ci]); err != nil {
			report.FootprintViolation = err.Error()
			return nil, report, err
		}
	}

	// The rules run on states over a check-private copy of the variable list, so a write
	// that saturates can be attributed to this check (see clampRecorder), as Build does.
	bvars := append([]Var(nil), r.vars...)
	rec := watchClamps(bvars)
	defer rec.stop()
	run := checkedRules{r: r, dom: newDomainCheck(bvars), clamp: rec}

	// WFC and normal forms, every component first (as Build computes every normal form
	// before any step), then the step tables.
	tabs := make([]*compTables, len(comps))
	repairBound := 0
	for ci := range comps {
		t, err := r.newCompTables(&comps[ci], bvars)
		if err != nil {
			return nil, report, err
		}
		if err := t.normalForms(run, report); err != nil {
			return nil, report, err
		}
		tabs[ci] = t
		report.Compositional.RepairBounds = append(report.Compositional.RepairBounds, t.depth)
		repairBound += t.depth
		if t.n > report.MaxComponentStates {
			report.MaxComponentStates = t.n
		}
	}
	report.WFC = true
	// The longest repair chain of the machine is the sum of the components' deepest
	// chains: a repair step is a step of exactly one component (rho_step), and the
	// components' deepest states combine into one state. bound_sum makes it the bound.
	report.MaxRepairLen = repairBound
	for ci := range comps {
		if err := tabs[ci].steps(run, report); err != nil {
			return nil, report, err
		}
	}
	report.Saturations = rec.saturations()

	// CC1 for the checked pairs, in Build's order.
	declared := p.declared
	sameComp := func(i, j int) int {
		if ci := p.evComp[i]; ci >= 0 && ci == p.evComp[j] {
			return ci
		}
		return -1
	}
	pairsDisjoint, pairsBrute := 0, 0
	for _, pr := range declared {
		i, j := pr[0], pr[1]
		ci := sameComp(i, j)
		if ci < 0 {
			pairsDisjoint++ // cc1_cross: different components (or an event that touches nothing)
			continue
		}
		pairsBrute++
		if f := tabs[ci].witness(i, j); f != nil {
			report.CC = false
			report.PairsTotal = pairsDisjoint + pairsBrute
			report.PairsDisjoint, report.PairsBrute = pairsDisjoint, pairsBrute
			report.CCFailure = f
			return nil, report, fmt.Errorf("gsm: Compensation Commutativity (CC) check failed for (%q, %q) in component %s",
				f.Event1, f.Event2, r.componentNames(tabs[ci].c))
		}
	}
	report.CC = true
	report.PairsTotal = pairsDisjoint + pairsBrute
	report.PairsDisjoint, report.PairsBrute = pairsDisjoint, pairsBrute

	// In declared-only mode every undeclared pair is checked too, without failing, as
	// Build does; a cross-component pair commutes at every valid state (cc1_cross).
	if !p.allPairs {
		isDeclared := make(map[[2]int]bool, len(declared))
		for _, pr := range declared {
			isDeclared[pr] = true
		}
		for i := 0; i < len(p.evComp); i++ {
			for j := i + 1; j < len(p.evComp); j++ {
				if isDeclared[[2]int{i, j}] {
					continue
				}
				report.PairsUndeclared++
				if ci := sameComp(i, j); ci >= 0 {
					if f := tabs[ci].witness(i, j); f != nil {
						report.CausalOrderRequired = append(report.CausalOrderRequired, *f)
					}
				}
			}
		}
	}
	// Idempotence decomposes the same way: an event's step changes only its component
	// (G_own, G_other), so it is idempotent at a valid state iff at the state's
	// restriction to its component.
	for ei, ci := range p.evComp {
		if ci >= 0 && !tabs[ci].idempotent(ei) {
			report.NotIdempotent = append(report.NotIdempotent, r.events[ei].name)
		}
	}

	// The oracle gate: the verified table oracle must certify every component's tables,
	// or there is no machine.
	for ci := range comps {
		if err := certifyTables(tabs[ci].lookup(declared, p.evComp, ci), fmt.Sprintf("the tables of component %s",
			r.componentNames(&comps[ci]))); err != nil {
			report.failClosed(err)
			return nil, report, err
		}
	}

	report.Assurance = AssuranceOracleComponents
	if p.closure != "" {
		report.Assurance = AssuranceOracleComponentsTested
	}
	report.FootprintChecked = true

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

// reduction describes plan p for Report.Compositional, before any check runs; the
// repair bounds are filled in as WFC is checked.
func (r *Registry) reduction(p *compPlan) *CompositionalReduction {
	red := &CompositionalReduction{
		Components:      len(p.comps),
		FootprintsExact: p.closure == "",
		GlobalStates:    big.NewInt(1),
	}
	for ci := range p.comps {
		c := &p.comps[ci]
		names := make([]string, len(c.vars))
		for k, vi := range c.vars {
			names[k] = r.vars[vi].name
		}
		red.Vars = append(red.Vars, names)
		_, n := r.componentSize(c)
		red.StatesChecked += n
		if n > red.LargestStates {
			red.LargestStates = n
		}
	}
	for _, v := range r.vars {
		red.GlobalStates.Mul(red.GlobalStates, big.NewInt(int64(v.domain)))
	}
	for _, pr := range p.declared {
		if ci := p.evComp[pr[0]]; ci < 0 || ci != p.evComp[pr[1]] {
			red.CrossPairs++
		}
	}
	return red
}

// compTables is one component's tables over its subspace: the states that assign the
// component's variables and hold every other variable at zero, in ascending encoding
// order (so the zero state is 0). nf and step are indices into states; step has one row
// per event of the component, in c.events order.
type compTables struct {
	r      *Registry
	c      *component
	vars   []Var // the check's variable list (states are over it)
	n      int
	states []uint64
	mask   uint64 // the bits of the component's variables
	stride []int  // index weight of each of c.vars (c.vars[0] varies fastest)
	nf     []int32
	step   [][]int32
	local  map[int]int // event index -> row of step
	depth  int         // deepest repair chain
}

func (r *Registry) newCompTables(c *component, vars []Var) (*compTables, error) {
	_, n := r.componentSize(c)
	t := &compTables{r: r, c: c, vars: vars, n: n, stride: make([]int, len(c.vars)), local: map[int]int{}}
	w := 1
	for k, vi := range c.vars {
		v := r.vars[vi]
		t.mask |= uint64((1<<v.bits)-1) << v.offset
		t.stride[k] = w
		w *= v.domain
	}
	// Ascending encodings: c.vars are in index order, so in offset order, and the
	// lowest variable is the least significant digit.
	t.states = make([]uint64, n)
	digits := make([]int, len(c.vars))
	var packed uint64
	for i := 0; i < n; i++ {
		t.states[i] = packed
		for k := range digits {
			v := r.vars[c.vars[k]]
			digits[k]++
			if digits[k] < v.domain {
				packed += uint64(1) << v.offset
				break
			}
			packed -= uint64(digits[k]-1) << v.offset
			digits[k] = 0
		}
	}
	for k, ei := range c.events {
		t.local[ei] = k
	}
	return t, nil
}

// index returns the position of packed among the component's states, or false when it
// is not one (a variable outside the component is not zero, or a value is outside its
// domain).
func (t *compTables) index(packed uint64) (int, bool) {
	if packed&^t.mask != 0 {
		return 0, false
	}
	i := 0
	for k, vi := range t.c.vars {
		v := t.r.vars[vi]
		d := int((packed >> v.offset) & ((1 << v.bits) - 1))
		if d >= v.domain {
			return 0, false
		}
		i += d * t.stride[k]
	}
	return i, true
}

func (t *compTables) state(i int) State { return State{packed: t.states[i], vars: t.vars} }

// outside names the first variable outside the component on which out differs from in.
func (t *compTables) outside(in, out State) string {
	in2 := State{packed: in.packed, vars: t.r.vars}
	out2 := State{packed: out.packed, vars: t.r.vars}
	if v := t.r.firstOutsideWrite(in2, out2, indexSet(t.c.vars), t.mask); v >= 0 {
		return t.r.vars[v].name
	}
	return "?"
}

// normalForms checks WFC over the component's subspace: repair, by the registry's
// first violated invariant (gsm_literal: on the subspace that is the component's own
// repair), terminates from every state. It records each normal form and the deepest
// chain.
func (t *compTables) normalForms(run checkedRules, report *Report) error {
	r := t.r
	t.nf = make([]int32, t.n)
	var chain []uint64
	for i := 0; i < t.n; i++ {
		s := t.state(i)
		chain = append(chain[:0], s.packed)
		depth := 0
		for {
			ii := r.firstViolated(s)
			if ii < 0 {
				break
			}
			inv := r.invariants[ii]
			next, err := run.repair(inv, s)
			if err != nil {
				return err
			}
			if _, ok := t.index(next.packed); !ok {
				report.WFC = false
				err := fmt.Errorf("gsm: invariant %q repair writes variable %q outside its footprint component %s",
					inv.name, t.outside(s, next), r.componentNames(t.c))
				report.FootprintViolation = err.Error()
				return err
			}
			s = next
			depth++
			for _, p := range chain {
				if p == s.packed {
					report.WFC = false
					return fmt.Errorf("gsm: WFC check failed: compensation does not terminate in component %s",
						r.componentNames(t.c))
				}
			}
			chain = append(chain, s.packed)
		}
		j, _ := t.index(s.packed)
		t.nf[i] = int32(j)
		if depth > t.depth {
			t.depth = depth
		}
	}
	return nil
}

// steps fills the step table: every event of the component applied to every state of
// the subspace, then normalized.
func (t *compTables) steps(run checkedRules, report *Report) error {
	r := t.r
	t.step = make([][]int32, len(t.c.events))
	for k, ei := range t.c.events {
		ev := r.events[ei]
		row := make([]int32, t.n)
		for i := 0; i < t.n; i++ {
			s := t.state(i)
			after, err := run.applyEvent(ev, s)
			if err != nil {
				return err
			}
			j, ok := t.index(after.packed)
			if !ok {
				err := fmt.Errorf("gsm: event %q writes variable %q outside its footprint component %s",
					ev.name, t.outside(s, after), r.componentNames(t.c))
				report.FootprintViolation = err.Error()
				return err
			}
			row[i] = t.nf[j]
		}
		t.step[k] = row
	}
	return nil
}

// valid reports whether state i is valid (its own normal form).
func (t *compTables) valid(i int) bool { return int(t.nf[i]) == i }

// witness returns the CC1 failure of events i and j (both of this component) at the
// first valid state of the subspace where the two orders differ, or nil. By
// cc1_same_iff it is also the first valid state of the whole machine where they differ:
// zeroing the other components keeps a failure and lowers the encoding.
func (t *compTables) witness(i, j int) *CCFailure {
	a, b := t.step[t.local[i]], t.step[t.local[j]]
	for s := 0; s < t.n; s++ {
		if !t.valid(s) {
			continue
		}
		ij, ji := b[a[s]], a[b[s]]
		if ij != ji {
			return &CCFailure{
				Event1: t.r.events[i].name, Event2: t.r.events[j].name,
				State: t.state(s), Result1: t.state(int(ij)), Result2: t.state(int(ji)),
			}
		}
	}
	return nil
}

// idempotent reports whether event e (of this component) applied twice reaches the
// state it reaches once, from every valid state of the subspace.
func (t *compTables) idempotent(e int) bool {
	row := t.step[t.local[e]]
	for s := 0; s < t.n; s++ {
		if t.valid(s) && row[row[s]] != row[s] {
			return false
		}
	}
	return true
}

// lookup gives the table oracle this component's tables, with the declared pairs whose
// events both lie in it (renumbered to the component's events).
func (t *compTables) lookup(declared [][2]int, evComp []int, ci int) oracle.Lookup {
	pairs := [][2]int{}
	for _, pr := range declared {
		if evComp[pr[0]] == ci && evComp[pr[1]] == ci {
			pairs = append(pairs, [2]int{t.local[pr[0]], t.local[pr[1]]})
		}
	}
	return oracle.Lookup{
		N: t.n, NE: len(t.step),
		NF:    func(k int) int { return int(t.nf[k]) },
		Step:  func(e, k int) int { return int(t.step[e][k]) },
		Pairs: pairs,
	}
}

// tables returns lookup's tables as slices, for the extracted checkers.
func (t *compTables) tables(declared [][2]int, evComp []int, ci int) oracle.Tables {
	l := t.lookup(declared, evComp, ci)
	tb := oracle.Tables{NF: make([]int, l.N), Step: make([][]int, l.NE), Pairs: l.Pairs}
	for k := range tb.NF {
		tb.NF[k] = l.NF(k)
	}
	for e := range tb.Step {
		tb.Step[e] = make([]int, l.N)
		for k := range tb.Step[e] {
			tb.Step[e][k] = l.Step(e, k)
		}
	}
	return tb
}

// namedTables is one component's tables for the extracted checkers.
type namedTables struct {
	name   string // "<registry>/component<k>", k from 1
	tables oracle.Tables
}

// componentTablesFor recomputes, for a registry Build or BuildCompositional checked per
// component, every component's tables as the table oracle reads them. It reruns the
// rules; the gsmgate build and the differential test use it to hand each component to
// the extracted table checker. It returns an error when the registry does not decompose
// or a component's repair does not terminate.
func (r *Registry) componentTablesFor() ([]namedTables, error) {
	if err := r.checkNames(); err != nil {
		return nil, err
	}
	p, why := r.planComponents()
	if why != "" {
		return nil, fmt.Errorf("%s", why)
	}
	declared := p.declared
	var out []namedTables
	for ci := range p.comps {
		t, err := r.newCompTables(&p.comps[ci], r.vars)
		if err != nil {
			return nil, err
		}
		rep := &Report{}
		if err := t.normalForms(r.checked(), rep); err != nil {
			return nil, err
		}
		if err := t.steps(r.checked(), rep); err != nil {
			return nil, err
		}
		nt := namedTables{name: fmt.Sprintf("%s/component%d", r.name, ci+1), tables: t.tables(declared, p.evComp, ci)}
		out = append(out, nt)
	}
	return out, nil
}

// componentOf returns the index of the component of event name in the registry's plan,
// or -1.
func (r *Registry) componentOf(event string) int {
	p, why := r.planComponents()
	if why != "" {
		return -1
	}
	for ei, ev := range r.events {
		if ev.name == event {
			return p.evComp[ei]
		}
	}
	return -1
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

// CompositionalReduction records, in Report.Compositional, that the machine was checked
// per footprint component: by Build (its default for a registry too large to enumerate
// whose rules are combinators) or by BuildCompositional. The result is exact for the whole
// machine (normalization-confluence coq/CompositionalCheck.v, compositional_exact): a pass
// is the guarantee, and a failure in a component is a failure of the machine.
type CompositionalReduction struct {
	// Components is the number of footprint components.
	Components int
	// Vars names each component's variables, in declaration order.
	Vars [][]string
	// LargestStates is the number of states of the largest component's subspace.
	LargestStates int
	// StatesChecked is the number of states enumerated, summed over the components.
	StatesChecked int
	// GlobalStates is the number of states of the whole machine (the product of every
	// variable's domain), which the per-component check does not enumerate.
	GlobalStates *big.Int
	// CrossPairs is the number of checked event pairs whose events lie in different
	// components (or one of which reads and writes nothing): no check is needed, since
	// they commute at every valid state (cc1_cross). On a machine that passed it equals
	// Report.PairsDisjoint, which counts the pairs reached before a failure.
	CrossPairs int
	// RepairBounds is each component's deepest repair chain, for the components whose WFC
	// was checked (all of them unless WFC failed); Report.MaxRepairLen is their sum, the
	// machine's repair bound (bound_sum).
	RepairBounds []int
	// FootprintsExact is true when every rule is a combinator rule, whose footprint gsm
	// derives from its expression trees, reads included (exact: ast_event_local,
	// ast_check_local, ast_repair_local). It is false when closure footprints were tested
	// by perturbation (BuildCompositional with TrustClosureFootprints).
	FootprintsExact bool
}

// String is the report's line for a machine the per-component check verified.
func (c CompositionalReduction) String() string { return "Verified compositionally: " + c.detail() }

// detail describes the components, the cost, the cross-component pairs and how the
// footprints were checked.
func (c CompositionalReduction) detail() string {
	how := "footprints checked exactly (combinators)"
	if !c.FootprintsExact {
		how = "footprints tested by perturbation (TrustClosureFootprints)"
	}
	global := "?"
	if c.GlobalStates != nil {
		global = groupDigits(c.GlobalStates.String())
	}
	noun := "components"
	if c.Components == 1 {
		noun = "component"
	}
	return fmt.Sprintf("%d %s (largest %s states; %s states checked instead of %s); %d cross-component pairs need "+
		"no check (disjoint footprints, reads included); %s", c.Components, noun,
		groupDigits(fmt.Sprint(c.LargestStates)), groupDigits(fmt.Sprint(c.StatesChecked)), global, c.CrossPairs, how)
}

// groupDigits writes a decimal number with thousands separators.
func groupDigits(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// lazyRepairBoundError is the panic message when a lazy machine's repairs from in have
// not reached a valid state after bound steps (now is the state reached, last the
// invariant repaired last).
//
// The bound is exact for the machine the per-component check verified. Each component's
// deepest repair chain was measured over every state of that component, and a repair
// in one component neither reads nor writes another component's variables, so from any
// state the repairs take at most the sum of those depths, whatever order the
// components' invariants fire in (bound_sum). A longer chain means a rule reads or writes
// outside its declared footprint, or is not deterministic, and the repairs may cycle
// forever.
func (m *Machine) lazyRepairBoundError(in, now State, last string, bound int) string {
	if m.abstract {
		return fmt.Sprintf("gsm: machine %q: repairs from %s did not reach a valid state within %d steps, the most "+
			"Build verified by abstraction for this machine (the deepest repair chain over the representatives, "+
			"which term_abs makes a bound for every value); a bug in gsm (state reached: %s, last repaired: "+
			"invariant %q)", m.name, in, bound, now, last)
	}
	return fmt.Sprintf("gsm: machine %q: repairs from %s did not reach a valid state within %d steps, the most "+
		"the per-component check verified for this machine (the sum of each component's deepest repair chain); "+
		"a rule reads or writes outside its declared footprint or is not deterministic, and the repairs may "+
		"cycle (state reached: %s, last repaired: invariant %q)", m.name, in, bound, now, last)
}
