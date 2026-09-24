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
	name  string
	comps []*Registry
	idx   map[*Registry]int
	edges []edgeDef
}

type edgeDef struct {
	src, dst *Registry
	shared   []Var
	mapFn    func(srcNF, dst State) State
}

// NewFederation creates an empty federation.
func NewFederation(name string) *Federation {
	return &Federation{name: name, idx: map[*Registry]int{}}
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
	shared   []Var
	mapFn    func(srcNF, dst State) State
}

// FedMachine is an immutable, constructive federated normalizer. It holds the component
// machines separately (never a product machine) and applies the two-phase operator ρ_Fed.
type FedMachine struct {
	name  string
	comps []*Machine
	regs  []*Registry
	idx   map[*Registry]int
	edges []fedEdge
	topo  []int   // component indices in topological (source-first) order
	out   [][]int // out[i] = indices into edges for morphisms with src == i
}

// FedState is a compact federated state: one component State per registry.
type FedState struct {
	states []State
}

// Build verifies each component (WFC + CC) and returns a constructive federated machine.
// M0 additionally checks that the morphism network is acyclic (a topological order must
// exist for the constructive operator). Full forest verification, M1 validity preservation,
// and multi-source rejection arrive in the next milestone.
func (f *Federation) Build() (*FedMachine, *FedReport, error) {
	m := &FedMachine{
		name:  f.name,
		comps: make([]*Machine, len(f.comps)),
		regs:  append([]*Registry(nil), f.comps...),
		idx:   map[*Registry]int{},
	}
	for r, i := range f.idx {
		m.idx[r] = i
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
	for ei, e := range f.edges {
		si, di := f.idx[e.src], f.idx[e.dst]
		m.edges[ei] = fedEdge{src: si, dst: di, shared: e.shared, mapFn: e.mapFn}
		m.out[si] = append(m.out[si], ei)
	}

	topo, err := m.topoSort()
	if err != nil {
		return nil, report, err
	}
	m.topo = topo

	return m, report, nil
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
	// Phase 2: visit components source-first; each is finalized (its incoming edge, if
	// any, already fired earlier in topo order) before its outgoing edges propagate.
	for _, i := range m.topo {
		for _, ei := range m.out[i] {
			e := m.edges[ei]
			out.states[e.dst] = e.mapFn(out.states[e.src], out.states[e.dst])
		}
	}
	return out
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

// IsValid reports whether the federated state satisfies every local invariant and every
// morphism invariant µ_ij: ϕ_ij(σ_i) = π_sh(σ_j) (Def 8.4).
func (m *FedMachine) IsValid(fs FedState) bool {
	for i, c := range m.comps {
		if !c.IsValid(fs.states[i]) {
			return false
		}
	}
	for _, e := range m.edges {
		// µ_ij holds iff overwriting the target's shared component with the morphism image
		// is a no-op (the shared component already equals ϕ_ij(σ_i)).
		if e.mapFn(fs.states[e.src], fs.states[e.dst]).ID() != fs.states[e.dst].ID() {
			return false
		}
	}
	return true
}
