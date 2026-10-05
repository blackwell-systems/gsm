package gsm

import (
	"fmt"
	"strings"
)

// CycleDiagnostic explains a convergence obstruction on a cyclic morphism network. When Build
// rejects a cycle, this identifies the specific loop, iterates the loop's repair from the zero
// seed, and reports whether that repair settles or oscillates. The theory: a consistent global
// state around a cycle (a section) is a fixed point of the loop repair, and one exists iff SOME seed
// reaches a fixed point (normalization-confluence coq/CohomologyGeneral.v, thm_obstruction_reachable
// and c15_exact_refuter). One seed that oscillates proves nothing about the others unless the loop
// acts freely (c15_definitive_claim_false: a swap of {0,1} that fixes 2 orbits from 0, yet 2 is a
// section). So when the zero seed does not settle, DiagnoseCycle also checks every seed: every
// combination of valid states of the cycle's components, for being a fixed point of one round of
// the loop repair (a fixed point is its own seed, and every fixed point is such a combination).
//
// Converges reports whether the loop repair reached a fixed point FROM THE ZERO SEED. A true value
// proves a consistent state exists (c15_convergent_result_sound), not that every seed settles
// (other local states may still oscillate); the value of the diagnostic in that case is naming
// the cycle. A false value alone is not an obstruction: read Obstructed, which is true only when
// every seed was checked (AllSeeds) and none is a fixed point (no SectionExists).
type CycleDiagnostic struct {
	Cycle     []string   // component names in loop order: Cycle[0] -> Cycle[1] -> ... -> Cycle[0]
	Shared    [][]string // shared variable names on each cycle edge (aligned with Cycle)
	Converges bool       // whether the loop repair reached a fixed point from the zero seed
	Orbit     []string   // if !Converges, the zero seed's repeating sequence of shared-carrier configurations

	// AllSeeds reports, when !Converges, whether every seed was checked: every combination of
	// valid states of the cycle's components (Seeds of them). It is false when that product
	// exceeds maxDiagnoseSeeds, and then a false Converges says only that the zero seed does
	// not settle.
	AllSeeds bool
	Seeds    int // the number of seed combinations checked (0 when Converges)

	// SectionExists reports that the cycle has a consistent state (a fixed point of the loop
	// repair): from the zero seed when Converges, or from the seed search. Section is the
	// shared carrier of the consistent state the seed search found, when the zero seed did not
	// settle.
	SectionExists bool
	Section       string
}

// Obstructed reports whether the diagnostic proves the cycle has no consistent state: the zero
// seed did not settle, every seed was checked, and none is a fixed point of the loop repair. Only
// then is the cycle definitively unable to converge without coordination.
func (d *CycleDiagnostic) Obstructed() bool {
	return !d.Converges && d.AllSeeds && !d.SectionExists
}

// String renders the diagnostic as a readable explanation.
func (d *CycleDiagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cycle %s", strings.Join(append(append([]string{}, d.Cycle...), d.Cycle[0]), " -> "))
	if d.Converges {
		fmt.Fprintf(&b, ": loop repair settles from the zero seed, so a consistent state exists "+
			"(other seeds may still oscillate; cycle named for reference)")
		return b.String()
	}
	fmt.Fprintf(&b, ": loop repair does not settle from the zero seed; the shared carrier orbits")
	if len(d.Orbit) > 0 {
		fmt.Fprintf(&b, " [%s]", strings.Join(d.Orbit, " -> "))
	}
	switch {
	case d.SectionExists:
		fmt.Fprintf(&b, "; but another seed is a consistent state [%s], so this is not an obstruction", d.Section)
	case d.AllSeeds:
		fmt.Fprintf(&b, "; no seed reaches a fixed point (all %d checked), so the cycle has no consistent state", d.Seeds)
	default:
		fmt.Fprintf(&b, "; other seeds not checked (more than %d), so a consistent state may still exist", maxDiagnoseSeeds)
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
// converges from the zero seed and, when it does not, whether any seed does (see CycleDiagnostic).
// Returns (nil, nil) if the network is acyclic. It is a cycle-local analysis (it isolates the
// cycle's components and morphisms), intended to explain why a cyclic Build was rejected, not to
// re-decide convergence. An obstruction it reports (Obstructed) is one of this cycle alone, so the
// network has no consistent state either.
//
// Like Build, it analyzes the federation as it was when called: it works on a frozen copy of
// the wiring, and rejects a component registry changed while it runs (by a morphism closure
// that declares on it, say), reporting that change over any error it caused.
func (f *Federation) DiagnoseCycle() (*CycleDiagnostic, error) {
	g, before := f.frozen()
	d, err := g.diagnoseCycle()
	if cerr := checkComponentsUnchanged(g.comps, before); cerr != nil {
		return nil, cerr
	}
	return d, err
}

// diagnoseCycle is DiagnoseCycle on f itself; DiagnoseCycle calls it on a frozen copy.
func (f *Federation) diagnoseCycle() (*CycleDiagnostic, error) {
	if err := f.checkSharedVars(); err != nil {
		return nil, err
	}
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
	var orbit []string
	for round := 0; round < cap; round++ {
		key := stateKey()
		if first, ok := seen[key]; ok {
			orbit = trace[first:]
			break
		}
		seen[key] = len(trace)
		trace = append(trace, carrier())
		orbit = trace

		changed, err := f.loopRound(cyc, edges, mach, state)
		if err != nil {
			return nil, err
		}
		if !changed {
			return &CycleDiagnostic{Cycle: names, Shared: shared, Converges: true, SectionExists: true}, nil
		}
	}

	// The zero seed did not settle (it repeated, or did not settle within the bound). That is not
	// an obstruction by itself (c15_definitive_claim_false): look for a fixed point among all seeds.
	d := &CycleDiagnostic{Cycle: names, Shared: shared, Orbit: orbit}
	valid := make([][]State, k)
	seeds := 1
	for i, ci := range cyc {
		valid[i] = mach[ci].validStates()
		if len(valid[i]) == 0 {
			// A component with no valid state cannot hold a consistent state.
			d.AllSeeds, d.Seeds = true, 0
			return d, nil
		}
		if seeds > maxDiagnoseSeeds/len(valid[i]) {
			return d, nil // too many seeds to check: AllSeeds stays false
		}
		seeds *= len(valid[i])
	}
	d.AllSeeds, d.Seeds = true, seeds
	pick := make([]int, k)
	for {
		for i, ci := range cyc {
			state[ci] = valid[i][pick[i]]
		}
		changed, err := f.loopRound(cyc, edges, mach, state)
		if err != nil {
			return nil, err
		}
		if !changed { // the round left state as picked: a consistent state
			d.SectionExists, d.Section = true, carrier()
			return d, nil
		}
		// Next combination (odometer over the components' valid states).
		i := k - 1
		for ; i >= 0; i-- {
			pick[i]++
			if pick[i] < len(valid[i]) {
				break
			}
			pick[i] = 0
		}
		if i < 0 {
			return d, nil // every seed checked, none is a fixed point: obstructed
		}
	}
}

// loopRound runs one round of the loop repair on state in place: for each cycle edge in loop
// order, the target becomes the normal form of the edge's image. It reports whether any
// component changed; a round that changes nothing is a fixed point (every edge is consistent).
func (f *Federation) loopRound(cyc []int, edges []edgeDef, mach map[int]*Machine, state map[int]State) (bool, error) {
	k := len(cyc)
	changed := false
	for i := 0; i < k; i++ {
		dst := cyc[(i+1)%k]
		img := edges[i].mapFn(state[cyc[i]], state[dst])
		if err := mach[dst].dom.imageError(edges[i].describe, f.comps[dst].name, state[dst], img); err != nil {
			return false, err
		}
		next := mach[dst].Normalize(img)
		if next.ID() != state[dst].ID() {
			changed = true
		}
		state[dst] = next
	}
	return changed, nil
}

// validStates returns the valid states of a table machine (in-domain encodings that are their
// own normal form), in ascending order.
func (m *Machine) validStates() []State {
	var out []State
	for p := range m.nf {
		if m.valid[p] && m.nf[p] == uint64(p) {
			out = append(out, State{packed: uint64(p), vars: m.vars})
		}
	}
	return out
}

// maxCarrierRounds bounds the loop-repair iteration per cycle node before giving up. The carrier
// space is finite, so a deterministic iteration settles or repeats well within this.
const maxCarrierRounds = 256

// maxDiagnoseSeeds bounds the seed search of DiagnoseCycle: the number of combinations of the
// cycle components' valid states it checks for a fixed point (one loop round each).
const maxDiagnoseSeeds = 1 << 20
