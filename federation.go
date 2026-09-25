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
