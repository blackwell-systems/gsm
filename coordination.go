package gsm

import (
	"fmt"
	"sort"
)

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
//
// gsm does not check the coordination itself. The returned report records each removed edge
// on its target component (Report.Coordinated, printed as "Coordinated input"): something
// outside gsm must serialize writes to those shared variables, and each write must leave the
// target valid (apply it, then Normalize).
//
// A point names the morphisms it coordinates by Src, Dst and shared variable set (Shared, in
// any order), as CoordinationPlan returns them, and removes every morphism matching all three
// (parallel morphisms between two registries are separate points). A point that matches none
// is an error, which says so when a Src->Dst morphism exists but shares other variables,
// rather than coordinate something other than what the caller named.
func (f *Federation) BuildCoordinated(plan []CoordinationPoint) (*FedMachine, *FedReport, error) {
	remove := map[int]bool{}
	for _, cp := range plan {
		// A point removes every morphism it names by Src, Dst and shared variable set (there
		// may be parallel morphisms between two registries, each its own point). The set is
		// what is coordinated, so a point that matches no morphism on all three is an error,
		// rather than a silent no-op or the removal of something else.
		found := false
		var sameEnds [][]string // the shared sets of the Src->Dst morphisms, for the error
		for ei, e := range f.edges {
			if e.src.name != cp.Src || e.dst.name != cp.Dst {
				continue
			}
			names := make([]string, len(e.shared))
			for k, v := range e.shared {
				names[k] = v.name
			}
			if !sameNameSet(names, cp.Shared) {
				sameEnds = append(sameEnds, sortedNames(names))
				continue
			}
			remove[ei] = true
			found = true
		}
		if found {
			continue
		}
		rep := &FedReport{Name: f.name, Edges: len(f.edges)}
		if len(sameEnds) > 0 {
			return nil, rep, fmt.Errorf("gsm: coordination point %s: no morphism %s→%s shares %v (the %s→%s morphisms share %v)",
				cp, cp.Src, cp.Dst, sortedNames(cp.Shared), cp.Src, cp.Dst, sameEnds)
		}
		return nil, rep, fmt.Errorf("gsm: coordination point %s names no morphism of federation %q", cp, f.name)
	}
	if len(remove) == 0 {
		return f.Build()
	}
	m, rep, err := f.withoutEdges(remove).Build()
	recordCoordinated(rep, f.edges, remove)
	return m, rep, err
}

// recordCoordinated notes each removed edge on its target component's report
// (Report.Coordinated), so the report names what the coordination mechanism must do.
func recordCoordinated(rep *FedReport, edges []edgeDef, remove map[int]bool) {
	if rep == nil {
		return
	}
	for ei, e := range edges {
		if !remove[ei] {
			continue
		}
		cp := CoordinationPoint{Src: e.src.name, Dst: e.dst.name}
		for _, v := range e.shared {
			cp.Shared = append(cp.Shared, v.name)
		}
		for _, c := range rep.Components {
			if c != nil && c.Name == e.dst.name {
				c.Coordinated = append(c.Coordinated, cp)
			}
		}
	}
}

// sortedNames returns a sorted copy of names.
func sortedNames(names []string) []string {
	out := append([]string{}, names...)
	sort.Strings(out)
	return out
}

// sameNameSet reports whether a and b hold the same names (as sets, so order and
// repetition do not matter).
func sameNameSet(a, b []string) bool {
	set := func(xs []string) map[string]bool {
		m := make(map[string]bool, len(xs))
		for _, x := range xs {
			m[x] = true
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	for x := range sa {
		if !sb[x] {
			return false
		}
	}
	return true
}

// clone returns a copy of the federation's wiring (components, morphisms, resolvers, cycle
// opt-in, certified embeds) that later changes to f do not reach. The registries, morphism
// closures, and certificates are shared.
func (f *Federation) clone() *Federation {
	return f.withoutEdges(nil)
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

		monotoneSubs: f.monotoneSubs,
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
