package gsm

import (
	"fmt"
)

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
