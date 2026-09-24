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
// theorem once the network is tree-shaped and every morphism preserves validity under
// shared-component overwrite (M1). Critically, the federated normal form is *constructive*
// (Corollary 8.10): normalize each source independently, then propagate shared components
// through morphisms in topological order. We never materialize the product machine, so
// federation sidesteps the single-registry state-space ceiling entirely.
//
// M0 scope: the constructive operator (Normalize/Apply/IsValid) over tree-shaped networks.
// The build-time necessity checks (M1 validity preservation, acyclicity, multi-source
// rejection per Remark 8.15) land in the next milestone; Build here performs only a basic
// acyclicity check so the operator has a valid topological order to run against.
type Federation struct {
	name      string
	comps     []*Registry
	idx       map[*Registry]int
	edges     []edgeDef
	resolvers map[*Registry]Resolver
}

// A Resolver merges the morphism images of a target's incoming edges into its shared
// component when the target has more than one source (multi-source / DAG). It receives the
// target's current state and each source's finalized state keyed by source registry name,
// and returns the target with its shared variables set.
//
// Multi-source support extends beyond the paper's tree-only convergence theorem (Thm 8.9);
// the paper leaves conflict resolution open (Remark 8.15) because the merge is domain logic,
// not math. gsm makes it safe the same way it handles single registries: rather than appeal
// to a general theorem, Build EXHAUSTIVELY VERIFIES, for the specific federation, that the
// resolver (a) writes only shared variables, (b) preserves target validity for every reachable
// combination of valid source states, and (c) is a function of the sources alone (independent
// of the target's local state). A resolver that reads the target's local component, can produce
// an invalid target, or touches non-shared variables is rejected at build. Determinism — the
// resolver is a pure function of a name-keyed source map — gives order-independence.
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
	topo      []int      // component indices in topological (source-first) order
	out       [][]int    // out[i] = edge indices for morphisms with src == i
	in        [][]int    // in[j] = edge indices for morphisms with dst == j
	resolvers []Resolver // resolvers[j] merges a multi-source target's incoming edges (nil if none)
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
	for i, r := range f.comps {
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
	if err := f.verify(); err != nil {
		return nil, report, err
	}

	topo, err := m.topoSort()
	if err != nil {
		return nil, report, err
	}
	m.topo = topo

	return m, report, nil
}

// verify enforces the structural conditions federated convergence requires. Tree-shaped
// targets are checked per morphism (M1, the paper's proven case); multi-source targets are
// checked against their declared Resolver (the beyond-paper case, verified exhaustively).
func (f *Federation) verify() error {
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

	// Single-source targets: per-morphism M1 + well-formedness + source-determinacy.
	for _, e := range f.edges {
		if _, resolved := f.resolvers[e.dst]; resolved {
			continue // resolved targets are verified against their Resolver below
		}
		if err := f.verifyEdge(e); err != nil {
			return err
		}
	}

	// Multi-source (resolved) targets: exhaustively verify the Resolver over every combination
	// of valid source states. Deterministic order in ti keeps error reporting stable.
	for ti := range f.comps {
		target := f.comps[ti]
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

// verifyResolved exhaustively verifies a multi-source target's Resolver: for every combination
// of valid source states and every valid target state, the merge must write only shared
// variables, preserve target validity, and depend only on the sources (not the target's local
// state). This is the beyond-paper multi-source guarantee, established by enumeration rather
// than by a general theorem — the same discipline gsm applies to single-registry CC.
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
	// Phase 2: visit components source-first, setting each target's shared component exactly
	// once. Because sources precede their targets in topological order, every source is
	// finalized before the target that consumes it — single or multi.
	for _, j := range m.topo {
		if len(m.in[j]) == 0 {
			continue // a source registry has no shared component to set
		}
		if r := m.resolvers[j]; r != nil {
			out.states[j] = r(out.states[j], m.sourcesOf(out, j)) // multi-source merge
		} else {
			// exactly one incoming edge (multi-source without a resolver is rejected at Build)
			e := m.edges[m.in[j][0]]
			out.states[j] = e.mapFn(out.states[e.src], out.states[j])
		}
	}
	return out
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
	// Apply the morphism to a representative (zero) target; source-determinacy (checked at
	// Build) guarantees the shared values are independent of which target we use.
	projected := e.mapFn(srcState, m.comps[di].NewState())
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
