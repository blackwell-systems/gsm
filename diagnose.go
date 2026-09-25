package gsm

import (
	"fmt"
	"strings"
)

// CycleDiagnostic explains a convergence obstruction on a cyclic morphism network. When Build
// rejects a cycle, this identifies the specific loop and, by iterating the loop's repair from the
// zero seed, reports whether that repair settles or oscillates. The theory: a global convergent
// assignment around a cycle is a fixed point of the loop composite g (the cycle's morphisms
// composed on the shared subspace); no reachable fixed point means no consistent global state, and
// the orbit is the witness. See CATEGORICAL-STRUCTURE.md in the papers repo.
//
// Converges reports whether the loop repair reached a fixed point FROM THE ZERO SEED. A false value
// is a definitive obstruction witness: an orbit exists, so the cycle cannot converge. A true value
// means this seed settles and does not by itself prove global convergence (other local states may
// still oscillate); the value of the diagnostic in that case is naming the cycle.
type CycleDiagnostic struct {
	Cycle     []string   // component names in loop order: Cycle[0] -> Cycle[1] -> ... -> Cycle[0]
	Shared    [][]string // shared variable names on each cycle edge (aligned with Cycle)
	Converges bool       // whether the loop repair reached a fixed point from the zero seed
	Orbit     []string   // if !Converges, the repeating sequence of shared-carrier configurations
}

// String renders the diagnostic as a readable explanation.
func (d *CycleDiagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cycle %s", strings.Join(append(append([]string{}, d.Cycle...), d.Cycle[0]), " -> "))
	if d.Converges {
		fmt.Fprintf(&b, ": loop repair settles from the zero seed (cycle named for reference)")
		return b.String()
	}
	fmt.Fprintf(&b, ": loop repair does not converge (no reachable fixed point); the shared carrier orbits")
	if len(d.Orbit) > 0 {
		fmt.Fprintf(&b, " [%s]", strings.Join(d.Orbit, " -> "))
	}
	return b.String()
}

// findCycle returns one directed cycle in the morphism graph as component indices in loop order
// (c0 -> c1 -> ... -> c_{k-1} -> c0), or nil if the network is acyclic.
func (f *Federation) findCycle() []int {
	n := len(f.comps)
	adj := make([][]int, n)
	for _, e := range f.edges {
		adj[f.idx[e.src]] = append(adj[f.idx[e.src]], f.idx[e.dst])
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, n)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
	}
	var cycle []int
	var dfs func(u int) bool
	dfs = func(u int) bool {
		color[u] = gray
		for _, v := range adj[u] {
			switch color[v] {
			case white:
				parent[v] = u
				if dfs(v) {
					return true
				}
			case gray:
				// Back edge u -> v: reconstruct the loop v -> ... -> u -> v.
				path := []int{}
				for x := u; x != v; x = parent[x] {
					path = append(path, x)
				}
				path = append(path, v) // [u, ..., v]
				for l, r := 0, len(path)-1; l < r; l, r = l+1, r-1 {
					path[l], path[r] = path[r], path[l] // reverse to edge order [v, ..., u]
				}
				cycle = path
				return true
			}
		}
		color[u] = black
		return false
	}
	for i := 0; i < n; i++ {
		if color[i] == white && dfs(i) {
			return cycle
		}
	}
	return nil
}

// findEdge returns the morphism from component index src to dst, if one exists.
func (f *Federation) findEdge(src, dst int) (edgeDef, bool) {
	for _, e := range f.edges {
		if f.idx[e.src] == src && f.idx[e.dst] == dst {
			return e, true
		}
	}
	return edgeDef{}, false
}

// cyclePath renders findCycle's result as "a -> b -> ... -> a", or "" if acyclic.
func (f *Federation) cyclePath() string {
	cyc := f.findCycle()
	if cyc == nil {
		return ""
	}
	names := make([]string, 0, len(cyc)+1)
	for _, ci := range cyc {
		names = append(names, f.comps[ci].name)
	}
	names = append(names, f.comps[cyc[0]].name)
	return strings.Join(names, " -> ")
}

// DiagnoseCycle finds a directed cycle in the morphism network and analyzes whether its loop repair
// converges from the zero seed. Returns (nil, nil) if the network is acyclic. It is a cycle-local
// analysis (it isolates the cycle's components and morphisms), intended to explain why a cyclic
// Build was rejected, not to re-decide convergence.
func (f *Federation) DiagnoseCycle() (*CycleDiagnostic, error) {
	cyc := f.findCycle()
	if cyc == nil {
		return nil, nil
	}
	k := len(cyc)
	names := make([]string, k)
	for i, ci := range cyc {
		names[i] = f.comps[ci].name
	}

	edges := make([]edgeDef, k)
	shared := make([][]string, k)
	for i := 0; i < k; i++ {
		src, dst := cyc[i], cyc[(i+1)%k]
		e, ok := f.findEdge(src, dst)
		if !ok {
			return nil, fmt.Errorf("gsm: diagnose: no morphism %s->%s in the detected cycle", f.comps[src].name, f.comps[dst].name)
		}
		edges[i] = e
		for _, v := range e.shared {
			shared[i] = append(shared[i], v.name)
		}
	}

	// Build the cycle components' machines (skip CC; only normalization is needed).
	mach := make(map[int]*Machine, k)
	for _, ci := range cyc {
		m, _, err := f.comps[ci].build(false)
		if err != nil {
			return nil, fmt.Errorf("gsm: diagnose: component %q does not build: %w", f.comps[ci].name, err)
		}
		mach[ci] = m
	}

	// Iterate the loop repair from the zero seed, tracking the shared carrier per round. On a finite
	// space the deterministic iteration either reaches a fixed point or repeats (an orbit).
	state := make(map[int]State, k)
	for _, ci := range cyc {
		state[ci] = mach[ci].Normalize(mach[ci].NewState())
	}
	carrier := func() string {
		parts := make([]string, k)
		for i := 0; i < k; i++ {
			dst := cyc[(i+1)%k]
			var vs []string
			for _, v := range edges[i].shared {
				vs = append(vs, fmt.Sprintf("%s.%s=%d", f.comps[dst].name, v.name, state[dst].getRaw(v)))
			}
			parts[i] = strings.Join(vs, ",")
		}
		return strings.Join(parts, " | ")
	}
	// Orbit detection keys on the full state IDs of every cycle component, not the human-readable
	// carrier string: the carrier shows only shared vars and could collide (two distinct full states
	// with the same shared projection, or a var-name/value pair colliding across the "=,|" delimiters),
	// which would report a false orbit. The packed state IDs are exact and delimiter-safe.
	stateKey := func() string {
		var b strings.Builder
		for _, ci := range cyc {
			fmt.Fprintf(&b, "%d/", state[ci].ID())
		}
		return b.String()
	}

	seen := map[string]int{}
	var trace []string
	cap := k*maxCarrierRounds + 1
	for round := 0; round < cap; round++ {
		key := stateKey()
		if first, ok := seen[key]; ok {
			return &CycleDiagnostic{Cycle: names, Shared: shared, Converges: false, Orbit: trace[first:]}, nil
		}
		seen[key] = len(trace)
		trace = append(trace, carrier())

		changed := false
		for i := 0; i < k; i++ {
			dst := cyc[(i+1)%k]
			next := mach[dst].Normalize(edges[i].mapFn(state[cyc[i]], state[dst]))
			if next.ID() != state[dst].ID() {
				changed = true
			}
			state[dst] = next
		}
		if !changed {
			return &CycleDiagnostic{Cycle: names, Shared: shared, Converges: true}, nil
		}
	}
	// Did not settle or repeat within the bound: report as non-convergent with the trace so far.
	return &CycleDiagnostic{Cycle: names, Shared: shared, Converges: false, Orbit: trace}, nil
}

// maxCarrierRounds bounds the loop-repair iteration per cycle node before giving up. The carrier
// space is finite, so a deterministic iteration settles or repeats well within this.
const maxCarrierRounds = 256
