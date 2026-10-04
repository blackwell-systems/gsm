package gsm

import (
	"fmt"
)

// Visited-domain verification for AllowMonotoneCycles.
//
// FedMachine.normalizeCyclic does not stay inside the valid states. After Phase 1 every
// component is valid; the iteration then resets every controlled (shared) variable to raw 0
// and repairs targets until nothing changes. Throughout, each component keeps the non-shared
// ("local") part of its Phase-1 normal form, while its shared variables range over their whole
// domain. So the states a Map or Resolver is actually evaluated on are
//
//	visited(r) = { v with r's shared variables overwritten by any in-domain values : v valid }
//
// (for a component no morphism writes, visited(r) is just its valid states). With the locals L
// of every component fixed, the iteration is chaotic iteration of the repair operator F_L on the
// finite lattice of shared-value assignments, ordered componentwise, from its bottom (all raw
// 0). The hypotheses normalization-confluence uses for that (coq/Federation.v: F monotone on the
// whole poset with bottom; coq/Chaotic.v: up_incr, up_sound, up_below, all_fixed, which follow
// from F_L monotone for coordinate updates) quantify over every point of that lattice, invalid
// ones included. verifyMonotone's original check enumerates valid states only, which misses
// exactly the points the iteration starts from.
//
// verifyMonotoneVisited therefore checks, for every target, over every combination of visited
// source states and every visited target state:
//
//   - the image is a state of the target and writes only the target's shared variables;
//   - source-determinacy: the shared image does not depend on the target state, so F_L's
//     coordinate for the target is a function of the sources alone;
//   - validity: the target with the image written is valid. Every target is repaired in each
//     round, so at the fixed point every target equals such an image; this makes the least
//     fixed point a valid federated state (validity of the lfp is not part of the Coq lemmas,
//     which are about convergence; this is the conservative sufficient condition gsm uses);
//   - monotonicity of F_L for each fixed L: raising any one source shared variable by one step
//     never lowers any shared image value. On a product of chains this cover-step check is
//     equivalent to monotonicity over all ordered pairs.
//
// Given these, chaotic iteration from bottom reaches the unique least fixed point of F_L in any
// visiting order (Chaotic.v chaotic_reaches_lfp), so the order in which morphisms were declared
// (which fixes component indices and hence the visiting order) cannot change the result.
func (f *Federation) verifyMonotoneVisited() error {
	shared := f.controlledVars()
	visited := make([][]State, len(f.comps))
	visitedOf := func(i int) ([]State, error) {
		if visited[i] == nil {
			vs, err := visitedStates(f.comps[i], shared[i])
			if err != nil {
				return nil, err
			}
			visited[i] = vs
		}
		return visited[i], nil
	}

	inEdges := make([][]edgeDef, len(f.comps))
	for _, e := range f.edges {
		inEdges[f.idx[e.dst]] = append(inEdges[f.idx[e.dst]], e)
	}
	for ti, edges := range inEdges {
		if len(edges) == 0 {
			continue
		}
		target := f.comps[ti]
		sources, sharedVars := resolverInputs(edges)
		resolver := f.resolvers[target]

		dstDom, err := visitedOf(ti)
		if err != nil {
			return err
		}
		if len(dstDom) == 0 {
			continue // no valid target state: verify has already rejected the edge
		}
		srcDoms := make([][]State, len(sources))
		srcPos := make([]map[uint64]int, len(sources)) // packed state -> index in srcDoms[i]
		total := len(dstDom)
		points := 1
		empty := false
		for i, s := range sources {
			d, verr := visitedOf(f.idx[s])
			if verr != nil {
				return verr
			}
			if len(d) == 0 {
				empty = true
				break
			}
			if total > maxStateSpace/len(d) {
				return fmt.Errorf("gsm: monotone-cycle check for %q: the states the Kleene iteration can visit "+
					"(every shared value under every valid local part) exceed %d source/target combinations; "+
					"shrink the shared variable ranges or the number of sources", target.name, maxStateSpace)
			}
			total *= len(d)
			points *= len(d)
			srcDoms[i] = d
			srcPos[i] = make(map[uint64]int, len(d))
			for k, st := range d {
				srcPos[i][st.packed] = k
			}
		}
		if empty {
			continue // a source with no valid state never reaches the iteration
		}

		describe := func() string { return resolverName(target.name)() }
		if resolver == nil {
			describe = edges[0].describe
		}
		image := func(combo []State, dst State) State {
			if resolver != nil {
				m := make(map[string]State, len(sources))
				for i, s := range sources {
					m[s.name] = combo[i]
				}
				return resolver(dst, m)
			}
			return edges[0].mapFn(combo[0], dst)
		}

		sharedIdx := make(map[int]bool, len(sharedVars))
		for _, v := range sharedVars {
			sharedIdx[v.index] = true
		}
		dom := newDomainCheck(target.vars)
		images := make([][]uint64, 0, points)
		err = forEachCombo(srcDoms, func(combo []State) error {
			var ref []uint64
			for di, dst := range dstDom {
				out := image(combo, dst)
				if ierr := dom.imageError(describe, target.name, dst, out); ierr != nil {
					return ierr
				}
				for _, v := range dom.vars {
					if !sharedIdx[v.index] && out.getRaw(v) != dst.getRaw(v) {
						return fmt.Errorf("gsm: %s modifies non-shared variable %q on target state %s, a state the "+
							"monotone-cycle iteration visits; it may only overwrite variables declared in Shared()",
							describe(), v.name, dst)
					}
				}
				raw := make([]uint64, len(sharedVars))
				for i, v := range sharedVars {
					raw[i] = out.getRaw(v)
				}
				if di == 0 {
					ref = raw
				} else if !rawEqual(ref, raw) {
					return fmt.Errorf("gsm: %s depends on the target's state %s for sources %s; on a monotone cycle "+
						"the image must be a function of the sources alone on every state the iteration visits, "+
						"including ones whose shared variables are still rising", describe(), dst, statesString(combo))
				}
				if !target.allInvariantsHold(out) {
					return fmt.Errorf("gsm: %s makes target %q invalid (%s) from sources %s, a combination the "+
						"monotone-cycle iteration can visit (shared variables are reset to their minimum and climb); "+
						"the fixed point could then be an invalid federated state, so the cycle is rejected",
						describe(), target.name, out, statesString(combo))
				}
			}
			images = append(images, ref)
			return nil
		})
		if err != nil {
			return err
		}

		// Monotonicity of F_L: forEachCombo enumerates points in mixed-radix order with the last
		// source fastest, so point index = sum_i idx_i * stride_i.
		stride := make([]int, len(sources))
		acc := 1
		for i := len(sources) - 1; i >= 0; i-- {
			stride[i] = acc
			acc *= len(srcDoms[i])
		}
		idx := make([]int, len(sources))
		for p := 0; p < points; p++ {
			rem := p
			for i := range sources {
				idx[i] = rem / stride[i]
				rem %= stride[i]
			}
			for i, s := range sources {
				cur := srcDoms[i][idx[i]]
				for _, vi := range shared[f.idx[s]] {
					v := s.vars[vi]
					r := cur.getRaw(v)
					if r+1 >= uint64(v.domain) {
						continue
					}
					k, ok := srcPos[i][cur.setRaw(v, r+1).packed]
					if !ok {
						continue // unreachable: visited states include every shared value
					}
					q := p + (k-idx[i])*stride[i]
					if !rawLE(images[p], images[q]) {
						return fmt.Errorf("gsm: repair for %q is not monotone on the states the monotone-cycle iteration "+
							"visits: raising %s.%s from %s to %s lowers the image (%s). The iteration resets shared "+
							"variables to their minimum and evaluates morphisms on the intermediate (possibly invalid) "+
							"states, so a non-monotone step there can make the result depend on the order morphisms "+
							"were declared. Make the Map/Resolver monotone on every shared value, or use an acyclic network",
							target.name, s.name, v.name, statesString(srcDoms[i][idx[i]:idx[i]+1]),
							statesString(srcDoms[i][k:k+1]), describe())
					}
				}
			}
		}
	}
	return nil
}

// controlledVars returns, per component index, the variable indices some morphism writes (the
// union of Shared() over its incoming edges): the variables normalizeCyclic resets to bottom.
func (f *Federation) controlledVars() [][]int {
	out := make([][]int, len(f.comps))
	for _, e := range f.edges {
		di := f.idx[e.dst]
		for _, v := range e.shared {
			if !containsInt(out[di], v.index) {
				out[di] = append(out[di], v.index)
			}
		}
	}
	return out
}

// visitedStates enumerates the states of r that monotone-cycle iteration can evaluate a closure
// on: each valid state's non-shared part combined with every in-domain value of the shared
// variables (indices in shared). With no shared variables this is the valid states.
func visitedStates(r *Registry, shared []int) ([]State, error) {
	valid := r.validStates()
	if len(shared) == 0 {
		return valid, nil
	}
	combos := 1
	for _, vi := range shared {
		d := r.vars[vi].domain
		if combos > maxStateSpace/d {
			return nil, fmt.Errorf("gsm: monotone-cycle check for %q: its shared variables have more than %d "+
				"value combinations", r.name, maxStateSpace)
		}
		combos *= d
	}
	seen := map[uint64]bool{}
	var bases []State
	for _, s := range valid {
		b := s
		for _, vi := range shared {
			b = b.setRaw(r.vars[vi], 0)
		}
		if !seen[b.packed] {
			seen[b.packed] = true
			bases = append(bases, b)
		}
	}
	if len(bases) > 0 && combos > maxStateSpace/len(bases) {
		return nil, fmt.Errorf("gsm: monotone-cycle check for %q: the states the Kleene iteration can visit "+
			"exceed %d", r.name, maxStateSpace)
	}
	out := make([]State, 0, len(bases)*combos)
	for _, b := range bases {
		s := b
		for {
			out = append(out, s)
			// Odometer over the shared variables' raw values.
			k := 0
			for ; k < len(shared); k++ {
				v := r.vars[shared[k]]
				if raw := s.getRaw(v) + 1; raw < uint64(v.domain) {
					s = s.setRaw(v, raw)
					break
				}
				s = s.setRaw(v, 0)
			}
			if k == len(shared) {
				break
			}
		}
	}
	return out, nil
}

func rawEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func statesString(ss []State) string {
	s := "("
	for i, st := range ss {
		if i > 0 {
			s += ", "
		}
		s += st.String()
	}
	return s + ")"
}
