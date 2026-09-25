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
// repair is extra compensation. Convergence therefore follows from the single-registry
// theorem for any acyclic network in which every single-source morphism preserves validity
// under shared-component overwrite (M1) and every multi-source target has a Resolver
// satisfying source-determinacy (R1) and validity-preservation (R2). The federated normal
// form is *constructive* (Corollary 8.10): normalize each source independently, then
// propagate shared components through morphisms/resolvers in topological order. We never
// materialize the product machine, so federation sidesteps the single-registry ceiling.
//
// Build enforces the theorems' preconditions: components converge (WFC + CC), the network is
// acyclic, single-source morphisms satisfy M1, and resolvers satisfy R1/R2 — all by finite
// enumeration. A FedMachine exists only if the whole network is proven convergent.
type Federation struct {
	name        string
	comps       []*Registry
	idx         map[*Registry]int
	edges       []edgeDef
	resolvers   map[*Registry]Resolver
	allowCycles bool
	certified   []*certifiedEmbed // sub-federations embedded on certificate (see certificate.go)
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
// pure function of a name-keyed source map — gives order-independence.
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
func (f *Federation) Resolve(target *Registry, r Resolver) *Federation {
	f.register(target)
	f.resolvers[target] = r
	return f
}

// AllowMonotoneCycles permits cyclic morphism networks. By default the network must be
// acyclic (the paper's tree/DAG theorems). With this opt-in, Build instead requires every
// morphism/resolver to be MONOTONE with respect to the componentwise order on variable
// values, and computes the federated normal form by Kleene iteration to the least fixed
// point. This is the paper's "Monotone Convergence Despite Cycles" theorem: on ordered
// (lattice) shared domains, a monotone repair operator converges — order-independently, by
// chaotic iteration — even when the constraint graph has cycles. Non-monotone cyclic
// networks (e.g. the negation counterexample) are still rejected.
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
// rejects it unless AllowMonotoneCycles is set and the repair is monotone.
func (f *Federation) Embed(sub *Federation) *Federation {
	for _, r := range sub.comps {
		f.register(r)
	}
	f.edges = append(f.edges, sub.edges...)
	for r, res := range sub.resolvers {
		f.resolvers[r] = res
	}
	if sub.allowCycles {
		f.allowCycles = true
	}
	return f
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
// morphism) and converge via the target's own compensation.
func (mb *MorphismBuilder) Shared(dstVars ...Var) *MorphismBuilder {
	mb.e.shared = append(mb.e.shared, dstVars...)
	return mb
}

// Map sets the morphism image function. Given the source's normal form and the current
// target state, it returns the target with ONLY its shared component overwritten to the
// morphism image. (A later milestone verifies at Build time that the function touches
// nothing outside Shared() and preserves target validity — the M1 condition.)
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
}

func (r *FedReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Federation: %s\n", r.Name)
	fmt.Fprintf(&b, "  Components: %d\n", len(r.Components))
	fmt.Fprintf(&b, "  Morphisms:  %d\n\n", r.Edges)
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
}

// FedState is a compact federated state: one component State per registry.
type FedState struct {
	states []State
}

// Build verifies the federation converges and returns a constructive federated machine.
// It refuses any network that the paper proves cannot converge:
//
//   - each component must satisfy WFC + CC (via Registry.Build);
//   - the network must be a tree/forest — no cycles (Prop 8.13) and at most one incoming
//     morphism per registry (multi-source is out of scope, Remark 8.15);
//   - each morphism must satisfy M1, validity preservation under shared-component overwrite
//     (Prop 8.14), and its Map must touch only the declared Shared() variables.
//
// This is the federated analogue of gsm's single-registry contract: a FedMachine only
// exists if convergence is guaranteed.
func (f *Federation) Build() (*FedMachine, *FedReport, error) {
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
	if err := f.validateCertificates(subOf); err != nil {
		return nil, report, err
	}

	for i, r := range f.comps {
		if sid, ok := subOf[r]; ok {
			// Certified component: build the runtime Machine (Phases 1-2) but trust the
			// certificate for CC rather than re-enumerating it.
			cm, cr, err := r.build(false)
			if err != nil {
				return nil, report, fmt.Errorf("gsm: certified component %q does not build: %w", r.name, err)
			}
			m.comps[i] = cm
			if certRep := f.certified[sid].reportFor(r.name); certRep != nil {
				report.Components = append(report.Components, certRep)
			} else {
				report.Components = append(report.Components, cr)
			}
			continue
		}
		cm, cr, err := r.Build()
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
				return nil, report, fmt.Errorf("%w: %s (use AllowMonotoneCycles if the repair is monotone, "+
					"or call DiagnoseCycle to see whether the loop converges)", err, path)
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

	return m, report, nil
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

// verify enforces the structural conditions federated convergence requires. Tree-shaped
// targets are checked per morphism (M1, the paper's proven case); multi-source targets are
// checked against their declared Resolver (the multi-source case, R1/R2 verified exhaustively).
func (f *Federation) verify(subOf map[*Registry]int) error {
	// Distinct component names: name-keyed replay (FedMachine.ApplyNamed) would be ambiguous
	// otherwise.
	seen := make(map[string]bool, len(f.comps))
	for _, r := range f.comps {
		if seen[r.name] {
			return fmt.Errorf("gsm: duplicate component registry name %q in federation %q", r.name, f.name)
		}
		seen[r.name] = true
	}

	// Group incoming edges per target.
	inEdges := make([][]edgeDef, len(f.comps))
	for _, e := range f.edges {
		di := f.idx[e.dst]
		inEdges[di] = append(inEdges[di], e)
	}

	// A target with more than one source needs a Resolver; one with a Resolver but no source
	// is meaningless. (Single-source targets use their morphism directly.)
	for ti, edges := range inEdges {
		target := f.comps[ti]
		_, resolved := f.resolvers[target]
		if len(edges) > 1 && !resolved {
			return fmt.Errorf("gsm: registry %q has %d incoming morphisms but no Resolver — a multi-source "+
				"target must declare Resolve(%q, ...) to merge its sources deterministically (Remark 8.15)",
				target.name, len(edges), target.name)
		}
	}
	for ti := range f.comps {
		if _, resolved := f.resolvers[f.comps[ti]]; resolved && len(inEdges[ti]) == 0 {
			return fmt.Errorf("gsm: Resolve declared for %q, which has no incoming morphisms", f.comps[ti].name)
		}
	}

	// Single-source targets: per-morphism M1 + well-formedness + source-determinacy. Edges
	// internal to a certified sub-federation are trusted by its certificate and skipped.
	for _, e := range f.edges {
		if internalEdge(subOf, e) {
			continue
		}
		if _, resolved := f.resolvers[e.dst]; resolved {
			continue // resolved targets are verified against their Resolver below
		}
		if err := f.verifyEdge(e); err != nil {
			return err
		}
	}

	// Multi-source (resolved) targets: exhaustively verify the Resolver over every combination
	// of valid source states. Deterministic order in ti keeps error reporting stable. A target
	// fully inside a certified sub is trusted and skipped; but one that also receives an external
	// (seam) morphism into an input port is re-verified, so the external source's R2 is covered.
	for ti := range f.comps {
		target := f.comps[ti]
		if tid, internal := subOf[target]; internal && !hasSeamIncoming(subOf, tid, inEdges[ti]) {
			continue
		}
		if resolver, resolved := f.resolvers[target]; resolved {
			if err := f.verifyResolved(target, resolver, inEdges[ti]); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyEdge checks a single morphism: M1 validity-preservation, shared-only well-formedness,
// and source-determinacy, by enumeration over valid source×target states.
func (f *Federation) verifyEdge(e edgeDef) error {
	srcValid := e.src.validStates()
	dstValid := e.dst.validStates()

	// A target with no valid state cannot preserve validity under any source image, so M1 is
	// unsatisfiable rather than vacuously true: reject it instead of passing an unverified edge.
	if len(dstValid) == 0 {
		return fmt.Errorf("gsm: morphism %s→%s: target %q has no valid state, so no source image can "+
			"preserve target validity (M1 is unsatisfiable)", e.src.name, e.dst.name, e.dst.name)
	}

	// General-path cost is |valid(src)|·|valid(dst)| per edge. When a target's validity
	// decomposes into independent shared/local parts, Remark 8.2 reduces this to
	// |valid(src)| checks — a future fast path. For now, guard rather than hang.
	if len(srcValid) > 0 && len(dstValid) > maxStateSpace/len(srcValid) {
		return fmt.Errorf("gsm: morphism %s→%s: M1 verification space (%d×%d) exceeds %d; "+
			"the shared/local fast path (Remark 8.2) is not yet implemented",
			e.src.name, e.dst.name, len(srcValid), len(dstValid), maxStateSpace)
	}

	sharedIdx := make(map[int]bool, len(e.shared))
	for _, v := range e.shared {
		sharedIdx[v.index] = true
	}

	for _, sa := range srcValid {
		var refShared map[int]uint64 // shared values from the first target, for this source
		for di, sb := range dstValid {
			sb2 := e.mapFn(sa, sb)
			// Well-formedness: Map must overwrite only Shared() variables.
			for _, v := range e.dst.vars {
				if !sharedIdx[v.index] && sb2.getRaw(v) != sb.getRaw(v) {
					return fmt.Errorf("gsm: morphism %s→%s Map modifies non-shared variable %q; "+
						"Map may only overwrite variables declared in Shared()", e.src.name, e.dst.name, v.name)
				}
			}
			// Source-determinacy: the shared image must depend only on the source, not on the
			// target's local state — otherwise ϕ isn't a function ΣA→SB (Def 8.1) and the shared
			// projection sent between distributed nodes would be ill-defined.
			cur := make(map[int]uint64, len(e.shared))
			for _, v := range e.shared {
				cur[v.index] = sb2.getRaw(v)
			}
			if di == 0 {
				refShared = cur
			} else {
				for idx, val := range cur {
					if refShared[idx] != val {
						return fmt.Errorf("gsm: morphism %s→%s image depends on the target's local state; "+
							"ϕ must be a function of the source alone (the Map's Shared() output may not read the target)",
							e.src.name, e.dst.name)
					}
				}
			}
			// M1 (Def 8.1): overwriting a valid target's shared component with the morphism
			// image of a valid source must preserve target validity. Otherwise federated
			// compensation oscillates (Prop 8.14).
			if !e.dst.allInvariantsHold(sb2) {
				return fmt.Errorf("gsm: morphism %s→%s violates M1 (validity preservation under overwrite): "+
					"source %s produces a shared component making target invalid at %s — "+
					"federated compensation would oscillate (§8.4, Prop 8.14)",
					e.src.name, e.dst.name, sa, sb2)
			}
		}
	}
	return nil
}

// verifyResolved exhaustively verifies a multi-source target's Resolver against the hypotheses
// of the paper's Federated Convergence with Resolution theorem: for every combination of valid
// source states and every valid target state, the merge must write only shared variables,
// preserve target validity (R2), and depend only on the sources, not the target's local state
// (R1). Establishing the theorem's preconditions by finite enumeration is the same discipline
// gsm applies to single-registry CC — the theorem then delivers convergence.
func (f *Federation) verifyResolved(target *Registry, resolver Resolver, edges []edgeDef) error {
	// Distinct sources, in edge order; union of the shared components they control.
	var sources []*Registry
	sharedIdx := make(map[int]bool)
	sourceSeen := make(map[*Registry]bool)
	for _, e := range edges {
		if !sourceSeen[e.src] {
			sourceSeen[e.src] = true
			sources = append(sources, e.src)
		}
		for _, v := range e.shared {
			sharedIdx[v.index] = true
		}
	}

	dstValid := target.validStates()
	// A resolved target with no valid state cannot preserve validity under any merge, so R2 is
	// unsatisfiable rather than vacuously true: reject it instead of passing an unverified resolver.
	if len(dstValid) == 0 {
		return fmt.Errorf("gsm: resolver for %q: the target has no valid state, so no merge can preserve "+
			"target validity (R2 is unsatisfiable)", target.name)
	}
	srcValids := make([][]State, len(sources))
	total := len(dstValid)
	for i, s := range sources {
		srcValids[i] = s.validStates()
		if len(srcValids[i]) == 0 {
			return nil // a source with no valid states makes the check vacuous
		}
		if total > maxStateSpace/len(srcValids[i]) {
			return fmt.Errorf("gsm: resolver for %q: verification space exceeds %d (too many source/target "+
				"combinations across %d sources)", target.name, maxStateSpace, len(sources))
		}
		total *= len(srcValids[i])
	}

	// Enumerate every source combination via a mixed-radix counter.
	idx := make([]int, len(sources))
	for {
		combo := make(map[string]State, len(sources))
		for k, s := range sources {
			combo[s.name] = srcValids[k][idx[k]]
		}

		var refShared map[int]uint64
		for di, dst := range dstValid {
			merged := resolver(dst, combo)
			// Well-formedness: the resolver may write only shared variables.
			for _, v := range target.vars {
				if !sharedIdx[v.index] && merged.getRaw(v) != dst.getRaw(v) {
					return fmt.Errorf("gsm: resolver for %q writes non-shared variable %q; "+
						"a resolver may only set variables declared Shared() on the incoming morphisms", target.name, v.name)
				}
			}
			// Source-determinacy: for fixed sources, the merged shared value is independent of
			// the target's (local) state.
			cur := make(map[int]uint64, len(sharedIdx))
			for vi := range sharedIdx {
				cur[vi] = merged.getRaw(target.vars[vi])
			}
			if di == 0 {
				refShared = cur
			} else {
				for vi, val := range cur {
					if refShared[vi] != val {
						return fmt.Errorf("gsm: resolver for %q depends on the target's local state; "+
							"the merge must be a function of the sources alone", target.name)
					}
				}
			}
			// Validity preservation: the merged state must satisfy the target's invariants.
			if !target.allInvariantsHold(merged) {
				return fmt.Errorf("gsm: resolver for %q can produce an invalid target state %s from a valid "+
					"source combination — federated compensation would not converge", target.name, merged)
			}
		}

		// Advance the mixed-radix counter.
		k := len(sources) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(srcValids[k]) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			break
		}
	}
	return nil
}

// monotoneGuard bounds the source-combination space for the (all-pairs) monotonicity check.
const monotoneGuard = 1024

// verifyMonotone checks that every target's repair is monotone with respect to the
// componentwise order on variable values — the hypothesis of the Monotone Convergence Despite
// Cycles theorem. Source-determinacy (already verified) lets us evaluate each target's shared
// image against a fixed target state, so monotonicity reduces to: over all pairs of valid
// source combinations P ⊑ P', the shared image is ⊑-ordered too. A non-monotone repair (e.g.
// the negation counterexample) is rejected.
func (f *Federation) verifyMonotone() error {
	inEdges := make([][]edgeDef, len(f.comps))
	for _, e := range f.edges {
		inEdges[f.idx[e.dst]] = append(inEdges[f.idx[e.dst]], e)
	}
	for ti, edges := range inEdges {
		if len(edges) == 0 {
			continue // source registry: no repair to check
		}
		target := f.comps[ti]

		// Distinct sources (edge order) and the shared variables they control.
		var sources []*Registry
		seenSrc := map[*Registry]bool{}
		var sharedVars []Var
		sharedSeen := map[int]bool{}
		for _, e := range edges {
			if !seenSrc[e.src] {
				seenSrc[e.src] = true
				sources = append(sources, e.src)
			}
			for _, v := range e.shared {
				if !sharedSeen[v.index] {
					sharedSeen[v.index] = true
					sharedVars = append(sharedVars, v)
				}
			}
		}

		srcValids := make([][]State, len(sources))
		n := 1
		for i, s := range sources {
			srcValids[i] = s.validStates()
			if len(srcValids[i]) == 0 {
				n = 0
				break
			}
			if n > monotoneGuard/len(srcValids[i]) {
				return fmt.Errorf("gsm: monotonicity check for %q: source space exceeds %d combinations", target.name, monotoneGuard)
			}
			n *= len(srcValids[i])
		}
		if n == 0 {
			continue
		}

		points := cartesianStates(srcValids)
		resolver := f.resolvers[target]
		dst0 := representativeTarget(target) // fixed valid target; shared image is source-determined
		// shared image (raw values of shared vars) for a source combination.
		image := func(combo []State) []uint64 {
			var out State
			if resolver != nil {
				m := make(map[string]State, len(sources))
				for i, s := range sources {
					m[s.name] = combo[i]
				}
				out = resolver(dst0, m)
			} else {
				out = edges[0].mapFn(combo[0], dst0)
			}
			raw := make([]uint64, len(sharedVars))
			for i, v := range sharedVars {
				raw[i] = out.getRaw(v)
			}
			return raw
		}

		for a := range points {
			for b := range points {
				if pointsLE(points[a], points[b]) && !rawLE(image(points[a]), image(points[b])) {
					return fmt.Errorf("gsm: repair for %q is not monotone — cyclic federations require "+
						"monotone morphisms/resolvers (a non-monotone repair, e.g. negation, cannot converge "+
						"on cycles; see the Monotone Convergence theorem). Use an acyclic network instead", target.name)
				}
			}
		}
	}
	return nil
}

// cartesianStates returns every combination picking one state from each set.
func cartesianStates(sets [][]State) [][]State {
	out := [][]State{{}}
	for _, set := range sets {
		var next [][]State
		for _, prefix := range out {
			for _, s := range set {
				combo := append(append([]State(nil), prefix...), s)
				next = append(next, combo)
			}
		}
		out = next
	}
	return out
}

// pointsLE is the componentwise order on aligned source-state tuples.
func pointsLE(a, b []State) bool {
	for i := range a {
		for _, v := range a[i].vars {
			if a[i].getRaw(v) > b[i].getRaw(v) {
				return false
			}
		}
	}
	return true
}

func rawLE(a, b []uint64) bool {
	for i := range a {
		if a[i] > b[i] {
			return false
		}
	}
	return true
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

// topoSort returns component indices in source-first order (Kahn's algorithm). A leftover
// node means the network has a cycle, which the paper proves cannot converge (Prop 8.13).
func (m *FedMachine) topoSort() ([]int, error) {
	n := len(m.comps)
	indeg := make([]int, n)
	for _, e := range m.edges {
		indeg[e.dst]++
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	order := make([]int, 0, n)
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		order = append(order, i)
		for _, ei := range m.out[i] {
			d := m.edges[ei].dst
			indeg[d]--
			if indeg[d] == 0 {
				queue = append(queue, d)
			}
		}
	}
	if len(order) != n {
		return nil, fmt.Errorf("gsm: morphism network has a cycle — cyclic federations cannot converge (see §8.4, Prop 8.13)")
	}
	return order, nil
}

// Name returns the federation's name.
func (m *FedMachine) Name() string { return m.name }

// NewState returns the zero federated state (each component at its zero state).
func (m *FedMachine) NewState() FedState {
	states := make([]State, len(m.comps))
	for i, c := range m.comps {
		states[i] = c.NewState()
	}
	return FedState{states: states}
}

// Of returns the component state for registry r.
func (m *FedMachine) Of(fs FedState, r *Registry) State {
	return fs.states[m.idx[r]]
}

// clone returns a FedState with an independent backing slice.
func (fs FedState) clone() FedState {
	cp := make([]State, len(fs.states))
	copy(cp, fs.states)
	return FedState{states: cp}
}

// Normalize applies the two-phase constructive operator ρ_Fed (Def 8.5, Cor 8.10):
//
//	Phase 1 (local normalization): normalize every component independently.
//	Phase 2 (morphism repair): in topological order, overwrite each target's shared
//	        component with the morphism image of its (finalized) source. Each edge fires
//	        once; M1 guarantees this cannot reintroduce local invalidity, so no component
//	        is re-normalized after repair (Lemma 8.7).
func (m *FedMachine) Normalize(fs FedState) FedState {
	out := fs.clone()
	// Phase 1.
	for i, c := range m.comps {
		out.states[i] = c.Normalize(out.states[i])
	}
	if m.cyclic {
		return m.normalizeCyclic(out)
	}
	// Phase 2: visit components source-first, setting each target's shared component exactly
	// once. Because sources precede their targets in topological order, every source is
	// finalized before the target that consumes it — single or multi.
	for _, j := range m.topo {
		if len(m.in[j]) == 0 {
			continue // a source registry has no shared component to set
		}
		out.states[j] = m.repair(out, j)
	}
	return out
}

// repair recomputes target j's state from its sources (single-source morphism or resolver).
func (m *FedMachine) repair(fs FedState, j int) State {
	if r := m.resolvers[j]; r != nil {
		return r(fs.states[j], m.sourcesOf(fs, j))
	}
	e := m.edges[m.in[j][0]] // exactly one incoming (multi-source without a resolver is rejected)
	return e.mapFn(fs.states[e.src], fs.states[j])
}

// normalizeCyclic computes the federated normal form on a cyclic (but monotone) network by
// Kleene iteration: reset every shared component to bottom, then apply repair until a fixed
// point. Monotonicity (verified at Build) guarantees this ascending iteration converges to
// the least fixed point, order-independently — the Monotone Convergence Despite Cycles result.
func (m *FedMachine) normalizeCyclic(out FedState) FedState {
	for j, vars := range m.sharedVar {
		for _, vi := range vars {
			out.states[j] = out.states[j].setRaw(m.comps[j].vars[vi], 0) // ⊥ = componentwise minimum
		}
	}
	for round := 0; round < m.kleeneCap; round++ {
		changed := false
		for j := range m.comps {
			if len(m.in[j]) == 0 {
				continue
			}
			if next := m.repair(out, j); next.ID() != out.states[j].ID() {
				out.states[j] = next
				changed = true
			}
		}
		if !changed {
			return out // fixed point
		}
	}
	return out // safety net; monotonicity guarantees convergence within kleeneCap
}

// sourcesOf gathers the (finalized) states of a target's source registries, keyed by source
// registry name, for its resolver.
func (m *FedMachine) sourcesOf(fs FedState, j int) map[string]State {
	sources := make(map[string]State, len(m.in[j]))
	for _, ei := range m.in[j] {
		src := m.edges[ei].src
		sources[m.comps[src].name] = fs.states[src]
	}
	return sources
}

// Apply processes a federated event targeting registry r, then returns the federated
// normal form. ρ_Fed is one-shot, so a single Normalize after the local step suffices.
//
// Note the authority argument in action: if r is a non-source, applying an event that
// touches r's shared component is overwritten by Phase 2 morphism repair — the source
// registry is authoritative. Effects on r's local component survive. (§8.5.)
func (m *FedMachine) Apply(fs FedState, r *Registry, event string) FedState {
	i := m.idx[r]
	next := fs.clone()
	next.states[i] = m.comps[i].Apply(next.states[i], event)
	return m.Normalize(next)
}

// Registries returns the component registry names, in declaration order.
func (m *FedMachine) Registries() []string {
	names := make([]string, len(m.comps))
	for i, c := range m.comps {
		names[i] = c.name
	}
	return names
}

// ApplyNamed is Apply keyed by registry name rather than pointer — the form needed for
// event-sourced replay, where a durable log holds (registry, event) strings, not live
// *Registry handles. Returns an error (rather than panicking) for an unknown registry or
// event, so a stale or corrupt log fails gracefully on reconstruction.
func (m *FedMachine) ApplyNamed(fs FedState, registry, event string) (FedState, error) {
	r, ok := m.byName[registry]
	if !ok {
		return fs, fmt.Errorf("gsm: unknown registry %q in federation %q", registry, m.name)
	}
	comp := m.comps[m.idx[r]]
	if _, ok := comp.events[event]; !ok {
		return fs, fmt.Errorf("gsm: registry %q has no event %q", registry, event)
	}
	return m.Apply(fs, r, event), nil
}

// Component returns the built single-registry Machine for r. In a distributed deployment
// each node runs just its own component's Machine, applies local events to it, and exchanges
// shared projections with its tree neighbours — no node needs the FedMachine or full state.
func (m *FedMachine) Component(r *Registry) *Machine {
	i, ok := m.idx[r]
	if !ok {
		return nil
	}
	return m.comps[i]
}

// Projection is the shared-component message ϕ_ij(σ_i) that a source registry sends to a
// target along their morphism: the concrete values the target's shared variables must take,
// derived purely from the source's state. It is small and serializable, so a distributed
// federation exchanges these along tree edges instead of shipping full federated state
// (the constructive normal form, Corollary 8.10).
type Projection struct {
	From, To string            // source and target registry names
	Shared   map[string]uint64 // target shared-variable name → raw value
}

// SharedProjection computes ϕ_ij(σ_i) — the message the source `src` sends its child `dst`
// along their morphism, given the source's current state. Because Build verifies the image
// depends only on the source (source-determinacy), the result is well-defined without the
// target's state. Returns an error if there is no morphism src→dst.
func (m *FedMachine) SharedProjection(srcState State, src, dst *Registry) (Projection, error) {
	si, ok := m.idx[src]
	if !ok {
		return Projection{}, fmt.Errorf("gsm: %q is not part of federation %q", src.name, m.name)
	}
	di, ok := m.idx[dst]
	if !ok {
		return Projection{}, fmt.Errorf("gsm: %q is not part of federation %q", dst.name, m.name)
	}
	var e *fedEdge
	for k := range m.edges {
		if m.edges[k].src == si && m.edges[k].dst == di {
			e = &m.edges[k]
			break
		}
	}
	if e == nil {
		return Projection{}, fmt.Errorf("gsm: no morphism %q→%q", src.name, dst.name)
	}
	// Apply the morphism to a representative valid target (the target machine's normalized zero
	// state); source-determinacy (checked at Build) guarantees the shared values are independent of
	// which valid target we use, and a normalized state stays within the morphism's contract.
	dstRep := m.comps[di].Normalize(m.comps[di].NewState())
	projected := e.mapFn(srcState, dstRep)
	shared := make(map[string]uint64, len(e.shared))
	for _, v := range e.shared {
		shared[v.name] = projected.getRaw(v)
	}
	return Projection{From: src.name, To: dst.name, Shared: shared}, nil
}

// IsValid reports whether the federated state satisfies every local invariant and every
// morphism invariant µ_ij: ϕ_ij(σ_i) = π_sh(σ_j) (Def 8.4).
func (m *FedMachine) IsValid(fs FedState) bool {
	for i, c := range m.comps {
		if !c.IsValid(fs.states[i]) {
			return false
		}
	}
	for j := range m.comps {
		if len(m.in[j]) == 0 {
			continue
		}
		// The morphism/merge invariant holds iff recomputing the target's shared component is a
		// no-op — it already equals the (single) morphism image or the resolver's merge.
		var want State
		if r := m.resolvers[j]; r != nil {
			want = r(fs.states[j], m.sourcesOf(fs, j))
		} else {
			e := m.edges[m.in[j][0]]
			want = e.mapFn(fs.states[e.src], fs.states[e.dst])
		}
		if want.ID() != fs.states[j].ID() {
			return false
		}
	}
	return true
}
