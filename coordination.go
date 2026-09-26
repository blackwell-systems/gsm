package gsm

import "fmt"

// CoordinationPoint names a morphism edge whose target shared component should be placed under
// external coordination (a single writer, a lock, or a consensus round) so that the network's
// convergence obstruction is broken. Coordinating the point means the target's shared variables are
// set by that external mechanism rather than driven by this morphism.
type CoordinationPoint struct {
	Src, Dst string   // the morphism src -> dst
	Shared   []string // the shared variable names it controls
}

func (c CoordinationPoint) String() string {
	return fmt.Sprintf("%s->%s%v", c.Src, c.Dst, c.Shared)
}

// CoordinationPlan returns a set of morphism edges to coordinate so the network becomes acyclic and
// therefore convergent: externally serializing each returned shared component breaks every morphism
// cycle, so the remaining network converges coordination-free (the acyclic route, no holonomy). It
// returns nil for an acyclic network, which needs no coordination.
//
// Use it when a cyclic federation is not monotone, so Build rejects it: the plan says WHERE to put
// coordination, and BuildCoordinated accepts the federation given that coordination. The set is a
// feedback edge set found by depth-first search: it is a correct, polynomial coordination of size at
// most the number of independent cycles, but not necessarily the minimum. The exact minimum is the
// group feedback edge set problem, NP-hard in general (see CATEGORICAL-STRUCTURE.md in the papers
// repo); this plan is the always-correct upper bound.
func (f *Federation) CoordinationPlan() []CoordinationPoint {
	n := len(f.comps)
	type outEdge struct{ to, ei int }
	out := make([][]outEdge, n)
	for ei, e := range f.edges {
		out[f.idx[e.src]] = append(out[f.idx[e.src]], outEdge{to: f.idx[e.dst], ei: ei})
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, n)
	backSet := map[int]bool{} // edge indices that close a cycle (a feedback edge set)
	var dfs func(u int)
	dfs = func(u int) {
		color[u] = gray
		for _, oe := range out[u] {
			switch color[oe.to] {
			case white:
				dfs(oe.to)
			case gray:
				backSet[oe.ei] = true // back edge: removing it breaks this cycle
			}
		}
		color[u] = black
	}
	for i := 0; i < n; i++ {
		if color[i] == white {
			dfs(i)
		}
	}
	if len(backSet) == 0 {
		return nil
	}
	// Emit in ascending edge order for a deterministic plan.
	var plan []CoordinationPoint
	for ei := range f.edges {
		if !backSet[ei] {
			continue
		}
		e := f.edges[ei]
		cp := CoordinationPoint{Src: e.src.name, Dst: e.dst.name}
		for _, v := range e.shared {
			cp.Shared = append(cp.Shared, v.name)
		}
		plan = append(plan, cp)
	}
	return plan
}

// BuildCoordinated builds the federation given that the plan's morphism edges are externally
// coordinated: those edges are removed (their target shared components become external inputs, set by
// the coordination mechanism rather than by the morphism), and the remaining network is built as
// usual. When the plan is a CoordinationPlan (a feedback edge set), the residual is acyclic and
// converges, so this turns a rejected cyclic federation into an accepted one that converges given the
// named coordination. An empty plan is exactly Build.
func (f *Federation) BuildCoordinated(plan []CoordinationPoint) (*FedMachine, *FedReport, error) {
	remove := map[int]bool{}
	for _, cp := range plan {
		for ei, e := range f.edges {
			if e.src.name == cp.Src && e.dst.name == cp.Dst {
				remove[ei] = true
			}
		}
	}
	if len(remove) == 0 {
		return f.Build()
	}
	return f.withoutEdges(remove).Build()
}

// withoutEdges returns a shallow copy of the federation with the given edge indices removed. The
// component registries and resolvers are shared; only the edge set is filtered.
func (f *Federation) withoutEdges(remove map[int]bool) *Federation {
	g := &Federation{
		name:        f.name,
		idx:         map[*Registry]int{},
		resolvers:   map[*Registry]Resolver{},
		allowCycles: f.allowCycles,
		certified:   f.certified,
	}
	for _, r := range f.comps {
		g.register(r)
	}
	for ei, e := range f.edges {
		if remove[ei] {
			continue
		}
		g.edges = append(g.edges, e)
	}
	for r, res := range f.resolvers {
		g.resolvers[r] = res
	}
	return g
}
