package gsm

import (
	"fmt"
	"strings"
)

// Federation is a network of component registries connected by directed registry
// morphisms encoding cross-registry (cross-organizational) semantic constraints.
//
// Per "Normalization Confluence in Federated Registry Networks" (Blackwell, 2026), §8:
// a federation is itself a registry — the federated state space is the product of the
// component spaces, morphism-consistency conditions are extra invariants, and morphism
// repair is extra compensation. In an acyclic network in which every single-source morphism
// preserves validity under shared-component overwrite (M1) and every multi-source target has
// a Resolver satisfying source-determinacy (R1) and validity-preservation (R2), the federated
// repair terminates in a valid state. That is not convergence of event orders: the paper's
// theorem that M1 (or R1/R2) and component WFC + CC suffice is false as stated
// (normalization-confluence coq/FederationGRS.v, fed_thm_fed_convergence_refuted). Event
// orders converge under the cross-registry conditions C1 and C2 in addition
// (fed_thm_fed_convergence_guarded; coq/FederationEvents.v, fed_events_commute), which Build
// also checks. That is convergence of the FedMachine model, where the repair runs after every
// event; a deployment that propagates by projections as separate steps (SharedProjection,
// MergeProjection) needs the stronger XU (dist_interleavings_converge), which Build reports and
// RequireProjectionSafe requires. The federated normal form is *constructive*: normalize each source
// independently, then propagate shared components through morphisms/resolvers in topological
// order. We never materialize the product machine, so federation sidesteps the
// single-registry ceiling.
//
// Build enforces the corrected theorem's preconditions: components converge (WFC + CC), the
// network is acyclic, single-source morphisms satisfy M1, resolvers satisfy R1/R2, and target
// events satisfy C1 and C2, all by finite enumeration. A FedMachine exists only if the whole
// network is proven convergent.
type Federation struct {
	name        string
	comps       []*Registry
	idx         map[*Registry]int
	edges       []edgeDef
	resolvers   map[*Registry]Resolver
	allowCycles bool
	certified   []*certifiedEmbed // sub-federations embedded on certificate (see certificate.go)

	// requireProjection makes Build fail unless distributed projection merging is certified
	// (RequireProjectionSafe).
	requireProjection bool

	// monotoneSubs names embedded sub-federations that opted in to AllowMonotoneCycles. The
	// opt-in does not carry over to f; Build names them when it rejects a cycle, so the parent
	// can opt in itself.
	monotoneSubs []string
}

// A Resolver merges the morphism images of a target's incoming edges into its shared
// component when the target has more than one source (multi-source / DAG). It receives the
// target's current state and each source's finalized state keyed by source registry name,
// and returns the target with its shared variables set.
//
// A resolver generalizes the single-source authority function: when |sources| == 1, the
// authority morphism is the special case. Multi-source convergence is the paper's "Federated
// Convergence with Resolution" theorem, which holds whenever the resolver is (R1) a function
// of the sources alone — independent of the target's local state — and (R2) validity-
// preserving (the multi-source generalization of M1). gsm certifies exactly those hypotheses:
// Build EXHAUSTIVELY VERIFIES, over every reachable combination of valid source states, that
// the resolver writes only shared variables, satisfies R1, and satisfies R2 — the same
// verify-the-preconditions contract gsm applies to single-registry WFC/CC and tree-federation
// M1. A resolver that reads local state (violates R1), can produce an invalid target (violates
// R2), or writes non-shared variables is rejected at build. Determinism — the resolver is a
// pure function of a name-keyed source map — gives order-independence. Like a morphism Map,
// it must return a state of the target registry (see MorphismBuilder.Map).
type Resolver func(dst State, sources map[string]State) State

type edgeDef struct {
	src, dst *Registry
	shared   []Var
	mapFn    func(srcNF, dst State) State
}

// NewFederation creates an empty federation.
func NewFederation(name string) *Federation {
	return &Federation{name: name, idx: map[*Registry]int{}, resolvers: map[*Registry]Resolver{}}
}

// Resolve declares how a multi-source target combines its incoming morphism images into its
// shared component. Required for any target with more than one incoming morphism (a target
// with a single source uses that morphism's Map directly and needs no resolver). See Resolver.
//
// A target has at most one resolver. Resolve panics if target already has one, whether from an
// earlier Resolve or from an embedded sub-federation, as MorphismBuilder.Add panics on a
// misdeclared morphism: a second resolver would otherwise silently replace the first.
func (f *Federation) Resolve(target *Registry, r Resolver) *Federation {
	f.register(target)
	f.addResolver(target, r, "Resolve")
	return f
}

// addResolver records r as target's resolver, panicking if target already has one. via names the
// declaration (Resolve, Embed or EmbedCertified) in the panic message.
func (f *Federation) addResolver(target *Registry, r Resolver, via string) {
	if _, dup := f.resolvers[target]; dup {
		panic(fmt.Sprintf("gsm: federation %q: %s declares a second Resolver for target %q; a target has "+
			"exactly one resolver, and a second one would replace the first", f.name, via, target.name))
	}
	f.resolvers[target] = r
}

// AllowMonotoneCycles permits cyclic morphism networks. By default the network must be
// acyclic (the paper's tree/DAG theorems). With this opt-in, Build instead requires every
// morphism/resolver to be MONOTONE with respect to the componentwise order on variable
// values, and computes the federated normal form by Kleene iteration to the least fixed
// point. This is the paper's "Monotone Convergence Despite Cycles" theorem: on ordered
// (lattice) shared domains, a monotone repair operator converges — order-independently, by
// chaotic iteration — even when the constraint graph has cycles. Non-monotone cyclic
// networks (e.g. the negation counterexample) are still rejected.
//
// The opt-in belongs to the federation it is called on. Embedding a sub-federation that opted in
// does not opt the parent in: a parent whose network has a cycle (inside the sub or through its
// boundary morphisms) must call AllowMonotoneCycles itself.
func (f *Federation) AllowMonotoneCycles() *Federation {
	f.allowCycles = true
	return f
}

// Embed composes a sub-federation into this one: it brings in the sub-federation's component
// registries, internal morphisms, and resolvers so that a subsystem can be defined and
// verified independently (via its own Build) and then reused as a unit. After embedding,
// connect the subsystem to the rest of the network with additional morphisms as usual.
//
// This realizes the compositionality of federated convergence (a convergent sub-federation
// collapses to an effective registry): the composed federation's runtime is the flat
// convergent machine over all components — no product state space is materialized, since a
// FedState holds one State per component — and Build re-checks the (local) conditions on the
// combined network. If boundary morphisms introduce a cycle, the usual rules apply: Build
// rejects it unless AllowMonotoneCycles is set on f and the repair is monotone. A sub's own
// AllowMonotoneCycles does not carry over to f (see AllowMonotoneCycles). Embed panics if the
// sub declares a resolver for a target that already has one (see Resolve).
func (f *Federation) Embed(sub *Federation) *Federation {
	for _, r := range sub.comps {
		f.register(r)
	}
	f.edges = append(f.edges, sub.edges...)
	f.embedResolvers(sub, "Embed")
	f.noteMonotoneSub(sub)
	return f
}

// embedResolvers brings in sub's resolvers, in component order so a panic is deterministic.
func (f *Federation) embedResolvers(sub *Federation, via string) {
	for _, r := range sub.comps {
		if res, ok := sub.resolvers[r]; ok {
			f.addResolver(r, res, fmt.Sprintf("%s(%q)", via, sub.name))
		}
	}
}

// noteMonotoneSub records that sub opted in to AllowMonotoneCycles, without opting f in.
func (f *Federation) noteMonotoneSub(sub *Federation) {
	if sub.allowCycles {
		f.monotoneSubs = append(f.monotoneSubs, sub.name)
	}
	f.monotoneSubs = append(f.monotoneSubs, sub.monotoneSubs...)
}

// Add registers a component registry. Idempotent. Morphism also auto-registers its
// endpoints, so Add is only needed for isolated components.
func (f *Federation) Add(r *Registry) *Federation {
	f.register(r)
	return f
}

func (f *Federation) register(r *Registry) int {
	if i, ok := f.idx[r]; ok {
		return i
	}
	i := len(f.comps)
	f.comps = append(f.comps, r)
	f.idx[r] = i
	return i
}

// Morphism begins declaring a directed morphism ϕ: src → dst. In ϕ, src is authoritative
// over dst's shared component: the source's normal form deterministically fixes the shared
// state of the target (the authority argument, §8.3).
func (f *Federation) Morphism(src, dst *Registry) *MorphismBuilder {
	f.register(src)
	f.register(dst)
	return &MorphismBuilder{f: f, e: edgeDef{src: src, dst: dst}}
}

// MorphismBuilder fluently declares a registry morphism.
type MorphismBuilder struct {
	f *Federation
	e edgeDef
}

// Shared designates which of the target's variables form its shared component — the part
// the morphism controls. The remaining target variables are local (unconstrained by this
// morphism) and converge via the target's own compensation. Each Var must be the target
// registry's own (as returned by its Bool, Enum or Int); Federation.Build rejects one of
// another registry, even with the same name or index.
func (mb *MorphismBuilder) Shared(dstVars ...Var) *MorphismBuilder {
	mb.e.shared = append(mb.e.shared, dstVars...)
	return mb
}

// Map sets the morphism image function. Given the source's normal form and the current
// target state, it returns the target with ONLY its shared component overwritten to the
// morphism image. Build verifies that the function touches nothing outside Shared(),
// preserves target validity (the M1 condition), and returns a state of the target registry
// (its variable schema, every variable within its range); FedMachine panics if an image it
// computes at Apply time is not one.
func (mb *MorphismBuilder) Map(fn func(srcNF, dst State) State) *MorphismBuilder {
	mb.e.mapFn = fn
	return mb
}

// Add registers the morphism with the federation.
func (mb *MorphismBuilder) Add() *Federation {
	if mb.e.mapFn == nil {
		panic(fmt.Sprintf("gsm: morphism %s→%s has no Map function", mb.e.src.name, mb.e.dst.name))
	}
	if len(mb.e.shared) == 0 {
		panic(fmt.Sprintf("gsm: morphism %s→%s declares no Shared() target variables", mb.e.src.name, mb.e.dst.name))
	}
	mb.f.edges = append(mb.f.edges, mb.e)
	return mb.f
}

// FedReport summarizes a federated build: per-component convergence plus network shape.
type FedReport struct {
	Name       string
	Components []*Report
	Edges      int

	// Assurance states what certified the federation as a whole. Each component's own
	// Report.Assurance says what certified that component (Build gates it on the verified table
	// oracle); the federation-level conditions in Checks are verified by gsm's Go code and are
	// not oracle-certified, so the federated composition is not oracle-certified. Empty unless
	// Build returned a machine.
	Assurance string

	// Checks lists the federation-level checks Build ran and passed, in order, then one line
	// stating whether distributed projection merging is certified (ProjectionLine), which Build
	// reports but does not require unless RequireProjectionSafe was called. Empty unless Build
	// returned a machine.
	Checks []string

	// ProjectionSafe reports whether distributed projection merging is certified: a deployment in
	// which each node runs its own component Machine, applies local events, and merges its
	// sources' projections (SharedProjection, MergeProjection, MergeProjectionAfter) whenever they
	// arrive converges, once propagation completes, to the FedMachine run of the same events. It
	// holds when the network is acyclic, has no multi-source target, and every target satisfies XU
	// (dist_interleavings_converge in normalization-confluence coq/FederationEvents.v; see
	// RequireProjectionSafe). Build's C1 and C2 certify the FedMachine model only, so a federation
	// Build accepts can have ProjectionSafe false. Set whenever Build got far enough to check it
	// (every other check passed), including when RequireProjectionSafe made it fail.
	ProjectionSafe bool

	// ProjectionLine states the projection result in one line: "certified (XU)", or "not
	// certified" with the witnesses and reasons. It is also the last line of Checks.
	ProjectionLine string

	// ProjectionWitnesses holds, for each target where XU fails, the first failure found: a valid
	// target state, a target event and a source image at which merging before and after the event
	// give different states. Empty when XU holds everywhere it was checked.
	ProjectionWitnesses []*ProjectionOrderError

	// Runtime states, for each sub-federation embedded with EmbedCertified, what its internal
	// morphisms and resolvers run at Apply time: the certificate's verified tables (the usual
	// case), or the closures, with the reason, for the targets that cannot run from tables.
	// Empty when nothing was embedded on a certificate, or unless Build returned a machine.
	Runtime []string

	// Regime is the regime report for the federation (see RegimeSummary): its shape and
	// deployment options, what the checks that ran guarantee (each line with its theorems),
	// what the deployment must provide, and what is not covered. Set by Build and
	// BuildCoordinated when they return a machine; nil otherwise. String prints it after the
	// federation-level checks, before the component reports.
	Regime *RegimeSummary

	// shape is what Build saw that the fields above do not record, for Regime.
	shape fedShape
}

// fedAssurance is FedReport.Assurance for a federation Build accepted.
const fedAssurance = "components oracle-gated (see each component's Assurance); federation-level checks " +
	"verified by gsm's Go enumeration, not by the verified oracle; the federated composition is not oracle-certified"

func (r *FedReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Federation: %s\n", r.Name)
	fmt.Fprintf(&b, "  Components: %d\n", len(r.Components))
	fmt.Fprintf(&b, "  Morphisms:  %d\n", r.Edges)
	if r.Assurance != "" {
		fmt.Fprintf(&b, "  Federation assurance: %s\n", r.Assurance)
		for _, c := range r.Checks {
			fmt.Fprintf(&b, "    checked (Go, not oracle): %s\n", c)
		}
		for _, l := range r.Runtime {
			fmt.Fprintf(&b, "    runtime: %s\n", l)
		}
		if r.Regime != nil {
			b.WriteString(indentBlock(r.Regime.String(), "  "))
		}
	} else {
		b.WriteString("  Federation assurance: not certified\n")
	}
	b.WriteString("\n")
	for _, c := range r.Components {
		for _, line := range strings.Split(strings.TrimRight(c.String(), "\n"), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// fedEdge is a resolved morphism (component indices, not registry pointers).
type fedEdge struct {
	src, dst int
	shared   []Var // the target's shared component (read by SharedProjection)
	mapFn    func(srcNF, dst State) State
}

// FedMachine is an immutable, constructive federated normalizer. It holds the component
// machines separately (never a product machine) and applies the two-phase operator ρ_Fed.
type FedMachine struct {
	name      string
	comps     []*Machine
	idx       map[*Registry]int
	byName    map[string]*Registry // component name → registry, for name-keyed replay
	edges     []fedEdge
	topo      []int      // component indices in topological (source-first) order (acyclic only)
	out       [][]int    // out[i] = edge indices for morphisms with src == i
	in        [][]int    // in[j] = edge indices for morphisms with dst == j
	resolvers []Resolver // resolvers[j] merges a multi-source target's incoming edges (nil if none)

	cyclic    bool    // true when the network has cycles (requires monotone repair)
	sharedVar [][]int // sharedVar[j] = var indices of j that some morphism controls (reset to ⊥)
	kleeneCap int     // safe upper bound on Kleene iteration rounds

	// certTab[j] is target j's certificate table when j is internal to an EmbedCertified sub and
	// runs from that table (see certificate_exec.go); nil when j runs its morphism or resolver.
	certTab []*certTable
}

// FedState is a compact federated state: one component State per registry.
type FedState struct {
	states []State
}

// Build verifies the federation converges and returns a constructive federated machine.
// It refuses any network that the paper proves cannot converge:
//
//   - each component must satisfy WFC + CC (via Registry.Build);
//   - the network must be acyclic (Prop 8.13), unless AllowMonotoneCycles is set and every
//     morphism and resolver is verified monotone over the states the iteration visits;
//   - a registry with one incoming morphism is driven by that morphism, which must satisfy M1
//     (validity preservation under shared-component overwrite, Prop 8.14) and touch only the
//     declared Shared() variables; a registry with several incoming morphisms must have a
//     Resolver, verified source-determined (R1) and validity-preserving (R2);
//   - every target event must commute with every change of the target's shared component that
//     its source(s) can cause (cross-registry CC, C1; a failure is a *CrossOrderError);
//   - every pair of target events the target's own CC covers must still commute when the
//     morphism repair runs after each of them (repaired CC, C2; a failure is a
//     *SameTargetOrderError).
//
// C1 and C2 are checked statically, over every valid source state, so together they are
// sufficient for every interleaving of independent events to converge in an acyclic
// federation, and, unchanged, on an AllowMonotoneCycles network (cyc_check_gc_lfp of
// normalization-confluence's FederationEventsCyclesCheck.v; see verifyCrossOrder); a failure means the federation may diverge from the reported state, which a
// particular run need not reach.
//
// This is the federated analogue of gsm's single-registry contract: a FedMachine only
// exists if convergence is guaranteed.
//
// That guarantee is for the FedMachine model, where the morphism repair runs after every event.
// A distributed deployment that propagates by projections (SharedProjection, MergeProjection)
// interleaves repair with local events as separate steps, which needs the stronger condition
// XU (C1 at every valid target state); Build checks it on an acyclic network and reports the
// result in FedReport.ProjectionSafe, ProjectionLine and ProjectionWitnesses, but does not
// require it unless RequireProjectionSafe was called.
//
// Build verifies and builds the federation as it was when Build was called: a morphism,
// component, or resolver added to f while Build runs (from inside a morphism closure, say) is not
// part of it, and a component registry changed while Build runs is rejected.
func (f *Federation) Build() (*FedMachine, *FedReport, error) {
	g, before := f.frozen()
	m, rep, err := g.build()
	// A closure that declares on a component can also make a later check fail (the
	// component's variable list no longer matches the states verified). The change is the
	// cause, so it is reported over any error it caused, as Registry.Build does.
	if cerr := checkComponentsUnchanged(g.comps, before); cerr != nil {
		return nil, rep, cerr
	}
	if err != nil {
		return nil, rep, err
	}
	rep.setRegime(true)
	return m, rep, nil
}

// frozen returns a copy of f's wiring, which closures run during verification cannot change,
// and each component's declarations (shape) as of now, to compare with checkComponentsUnchanged
// once the closures have run.
func (f *Federation) frozen() (*Federation, []registryShape) {
	g := f.clone()
	before := make([]registryShape, len(g.comps))
	for i, r := range g.comps {
		before[i] = r.shape()
	}
	return g, before
}

// checkComponentsUnchanged rejects a component whose declarations differ from before (see frozen).
func checkComponentsUnchanged(comps []*Registry, before []registryShape) error {
	for i, r := range comps {
		if err := r.checkUnchanged(before[i]); err != nil {
			return err
		}
	}
	return nil
}

// federationGlobal is why a federation component is not checked per footprint
// component: the federation checks run on each component's step tables, which only the
// global check produces.
const federationGlobal = "it is a federation component, and the federation checks run on its step tables, " +
	"which only the global check produces"

// build is Build on f itself; Build calls it on a frozen copy.
func (f *Federation) build() (*FedMachine, *FedReport, error) {
	// Names first: certificate validation and replay address (registry, event) by name.
	for _, r := range f.comps {
		if err := r.checkNames(); err != nil {
			return nil, &FedReport{Name: f.name, Edges: len(f.edges)}, err
		}
		if r.abs != nil {
			// The federation checks run on each component's step tables, which a machine
			// verified by abstraction does not have.
			return nil, &FedReport{Name: f.name, Edges: len(f.edges)}, fmt.Errorf("gsm: component %q declares "+
				"Abstract: federation components are verified by enumeration, and abstraction is not supported in "+
				"a federation", r.name)
		}
	}
	if err := f.checkSharedVars(); err != nil {
		return nil, &FedReport{Name: f.name, Edges: len(f.edges)}, err
	}
	m := &FedMachine{
		name:   f.name,
		comps:  make([]*Machine, len(f.comps)),
		idx:    map[*Registry]int{},
		byName: make(map[string]*Registry, len(f.comps)),
	}
	for r, i := range f.idx {
		m.idx[r] = i
		m.byName[r.name] = r
	}

	report := &FedReport{Name: f.name, Edges: len(f.edges)}

	// Certificate validation runs first: it checks each certified embed still matches its
	// certificate and that the seam obeys the output-port restriction, before any component is
	// built. subOf classifies components as belonging to a certified sub or not.
	subOf := f.subOf()
	certSnaps, certErr := f.validateCertificates(subOf)
	if certErr != nil {
		return nil, report, certErr
	}

	for i, r := range f.comps {
		if _, ok := subOf[r]; ok {
			// Certified component: re-check it rather than trust the certificate's verdict.
			// Build re-runs WFC and CC; CC is cheap here because the step tables, which the
			// runtime Machine needs anyway, are already built. A certificate issued by an
			// earlier, weaker verifier for a non-convergent component is refused here.
			cm, cr, err := r.buildWith(buildOpts{global: federationGlobal})
			if err != nil {
				return nil, report, fmt.Errorf("gsm: certified component %q does not converge on re-check: %w", r.name, err)
			}
			m.comps[i] = cm
			report.Components = append(report.Components, cr)
			continue
		}
		cm, cr, err := r.buildWith(buildOpts{global: federationGlobal})
		if err != nil {
			return nil, report, fmt.Errorf("gsm: component %q does not converge: %w", r.name, err)
		}
		m.comps[i] = cm
		report.Components = append(report.Components, cr)
	}

	m.edges = make([]fedEdge, len(f.edges))
	m.out = make([][]int, len(f.comps))
	m.in = make([][]int, len(f.comps))
	for ei, e := range f.edges {
		si, di := f.idx[e.src], f.idx[e.dst]
		m.edges[ei] = fedEdge{src: si, dst: di, shared: e.shared, mapFn: e.mapFn}
		m.out[si] = append(m.out[si], ei)
		m.in[di] = append(m.in[di], ei)
	}
	m.resolvers = make([]Resolver, len(f.comps))
	for r, res := range f.resolvers {
		m.resolvers[f.idx[r]] = res
	}

	// Forest + morphism verification (multi-source rejection, M1 validity preservation,
	// shared-only well-formedness) before the acyclicity check.
	if err := f.verify(subOf); err != nil {
		return nil, report, err
	}
	// Cross-registry CC (C1): every target event commutes with every source-driven change of the
	// target's shared component. Repaired CC (C2): every pair of target events covered by the
	// target's CC commutes with the repair between them. Both run per target, in one pass.
	imgs, err := f.verifyCrossOrder(m.comps)
	if err != nil {
		return nil, report, err
	}

	// The shared variables of each target (union across its incoming morphisms) — the
	// components reset to bottom before Kleene iteration in the cyclic case.
	m.sharedVar = make([][]int, len(f.comps))
	for _, e := range f.edges {
		di := f.idx[e.dst]
		for _, v := range e.shared {
			if !containsInt(m.sharedVar[di], v.index) {
				m.sharedVar[di] = append(m.sharedVar[di], v.index)
			}
		}
	}

	topo, err := m.topoSort()
	if err != nil {
		// A cycle. Allowed only under AllowMonotoneCycles, and only if repair is monotone.
		if !f.allowCycles {
			if path := f.cyclePath(); path != "" {
				err = fmt.Errorf("%w: %s (use AllowMonotoneCycles if the repair is monotone, "+
					"or call DiagnoseCycle to see whether the loop converges)", err, path)
			}
			if len(f.monotoneSubs) > 0 {
				err = fmt.Errorf("%w; embedded sub-federation(s) %q opted in to AllowMonotoneCycles, but the "+
					"opt-in does not carry over: call AllowMonotoneCycles on %q itself to allow the cycle",
					err, f.monotoneSubs, f.name)
			}
			return nil, report, err
		}
		m.cyclic = true
		if err := f.verifyMonotone(); err != nil {
			return nil, report, err
		}
		m.kleeneCap = m.monotoneChainBound()
	} else {
		m.topo = topo
	}

	// Distributed projection merging (XU): reported, and required only under
	// RequireProjectionSafe, since Build's guarantee is the FedMachine model.
	proj := f.verifyProjectionMerge(imgs, m.cyclic)
	report.ProjectionSafe = proj.safe
	report.ProjectionLine = proj.line
	report.ProjectionWitnesses = proj.witnesses
	if f.requireProjection {
		if err := proj.err(f.name); err != nil {
			return nil, report, err
		}
	}

	report.Runtime = f.certRuntime(m, certSnaps, subOf)
	report.shape = fedShape{cyclic: m.cyclic, multiSource: f.multiSourceTargets(),
		requireProjection: f.requireProjection, certified: len(f.certified)}
	report.Assurance = fedAssurance
	report.Checks = append(f.checksRun(m.cyclic), proj.line)
	return m, report, nil
}

// checksRun lists the federation-level checks build ran, for FedReport.Checks.
func (f *Federation) checksRun(cyclic bool) []string {
	single, resolved := 0, len(f.resolvers)
	for _, e := range f.edges {
		if _, ok := f.resolvers[e.dst]; !ok {
			single++
		}
	}
	checks := []string{
		"names: component, variable and event names distinct",
		"shared variables: every Shared() variable belongs to its morphism's target",
		fmt.Sprintf("M1: %d single-source morphism(s) write only Shared() variables, preserve target validity, and are source-determined", single),
		fmt.Sprintf("R1/R2: %d resolver(s) write only shared variables, are source-determined, and preserve target validity", resolved),
		"cross-registry CC (C1): every target event commutes with every source-driven change of its shared component",
		"repaired CC (C2): every target event pair its own CC covers commutes with the morphism repair between the two events",
	}
	if cyclic {
		checks = append(checks,
			"monotone cycles: every morphism and resolver is monotone (AllowMonotoneCycles); normal form by Kleene iteration",
			"event order on the cycle: certified by the per-target C1 and C2 checks above, comparing locals after the final "+
				"overwrite, with each target's image set taken over every valid source state (cyc_check_gc_lfp, "+
				"normalization-confluence FederationEventsCyclesCheck.v)")
		inDeg := map[*Registry]int{}
		multi := 0
		for _, e := range f.edges {
			if inDeg[e.dst]++; inDeg[e.dst] == 2 {
				multi++
			}
		}
		if multi > 0 {
			checks = append(checks, fmt.Sprintf("event order on the cycle, %d multi-source target(s): C1 and C2 checked "+
				"against the joint image set of all incoming edges (the resolver's image R over every combination of valid "+
				"source states, not each edge's own image), which is C1cyc for the whole shared part directly; the per-edge "+
				"route (multi_edge_c1, multi_edge_gc, FederationEventsCyclesMulti.v) is not relied on", multi))
		}
	} else {
		checks = append(checks, "acyclicity: the network has a topological order")
	}
	if len(f.certified) > 0 {
		checks = append(checks, fmt.Sprintf("certificates: %d certified embed(s) match their digest, tables re-checked, seam writes only input ports", len(f.certified)))
	}
	return checks
}

// multiSourceTargets names the targets with more than one incoming morphism, in component
// order.
func (f *Federation) multiSourceTargets() []string {
	inDeg := map[*Registry]int{}
	for _, e := range f.edges {
		inDeg[e.dst]++
	}
	var out []string
	for _, r := range f.comps {
		if inDeg[r] > 1 {
			out = append(out, r.name)
		}
	}
	return out
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// monotoneChainBound bounds the number of Kleene rounds: on a finite lattice a strictly
// ascending chain of the shared components can rise at most (sum of shared-var domain sizes)
// times, so this many rounds always reaches the fixed point. Plus slack.
func (m *FedMachine) monotoneChainBound() int {
	total := 2
	for j, vars := range m.sharedVar {
		for _, vi := range vars {
			total += m.comps[j].vars[vi].domain
		}
	}
	return total
}

// firstValidState returns the registry's lowest-packed valid state and true, or a zero State and
// false if the registry has no valid state. Cheaper than validStates() (it stops at the first hit
// rather than materializing the whole list); used as a source-determined morphism's representative
// target, since the shared image is independent of which valid target is chosen (verified at Build).
func (r *Registry) firstValidState() (State, bool) {
	packedCount := 1 << r.totalBits
	for i := 0; i < packedCount; i++ {
		if !r.isValidEncoding(uint64(i)) {
			continue
		}
		s := State{packed: uint64(i), vars: r.vars}
		if r.allInvariantsHold(s) {
			return s, true
		}
	}
	return State{vars: r.vars}, false
}

// representativeTarget returns a valid target state against which to evaluate a source-determined
// morphism or resolver. Source-determinacy (verified at Build) makes the shared image independent of
// which target is chosen, so any valid state works; a valid one keeps the morphism evaluated within
// its contract, unlike the raw zero encoding which may itself be an invalid state. Falls back to the
// zero state only when the target has no valid state, which Build rejects for any edge target.
func representativeTarget(r *Registry) State {
	if s, ok := r.firstValidState(); ok {
		return s
	}
	return State{vars: r.vars}
}

// validStates enumerates the registry's valid states (valid encoding + all invariants hold).
// Bounded by the same ≤20-bit ceiling Build enforces per component.
func (r *Registry) validStates() []State {
	packedCount := 1 << r.totalBits
	var out []State
	for i := 0; i < packedCount; i++ {
		if !r.isValidEncoding(uint64(i)) {
			continue
		}
		s := State{packed: uint64(i), vars: r.vars}
		if r.allInvariantsHold(s) {
			out = append(out, s)
		}
	}
	return out
}
