package gsm

import (
	"fmt"
)

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
	sources, sharedVars := resolverInputs(edges)
	sharedIdx := make(map[int]bool, len(sharedVars))
	for _, v := range sharedVars {
		sharedIdx[v.index] = true
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

	// Enumerate every source combination and, for each, every valid target state.
	return forEachCombo(srcValids, func(cs []State) error {
		combo := make(map[string]State, len(sources))
		for k, s := range sources {
			combo[s.name] = cs[k]
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
		return nil
	})
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
		sources, sharedVars := resolverInputs(edges)

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

// resolverInputs collects the distinct source registries (in edge order) and the union of shared
// variables written across a resolved target's incoming edges. Every path that enumerates a
// resolver's source combinations (verifyResolved, verifyMonotone, extractResolverTable) needs the
// same two, so they share this rather than re-deriving it three ways.
func resolverInputs(edges []edgeDef) (sources []*Registry, sharedVars []Var) {
	seenSrc := map[*Registry]bool{}
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
	return sources, sharedVars
}

// forEachCombo enumerates every combination that picks one state from each set in srcValids via a
// mixed-radix counter, invoking fn with the current pick (a slice aligned with srcValids, reused
// across calls, so fn must not retain it). fn returning an error stops the enumeration. Callers must
// ensure each set is non-empty (an empty set would make the space vacuous, which they handle before
// calling). It is the shared odometer behind verifyResolved and extractResolverTable.
func forEachCombo(srcValids [][]State, fn func(combo []State) error) error {
	idx := make([]int, len(srcValids))
	combo := make([]State, len(srcValids))
	for {
		for k := range srcValids {
			combo[k] = srcValids[k][idx[k]]
		}
		if err := fn(combo); err != nil {
			return err
		}
		k := len(srcValids) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(srcValids[k]) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			return nil
		}
	}
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
