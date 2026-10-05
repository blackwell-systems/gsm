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
// checkSharedVars rejects a morphism whose Shared() variable is not its target's: the
// target must have a variable at that index equal to it by value (sameVar). Freeness and
// the tables address shared variables by index and name, so a Var of another registry
// would otherwise stand for whichever variable sits at its index here (or none).
func (f *Federation) checkSharedVars() error {
	for _, e := range f.edges {
		for _, v := range e.shared {
			if v.index >= len(e.dst.vars) || !sameVar(e.dst.vars[v.index], v) {
				return fmt.Errorf("gsm: morphism %s→%s: Shared() variable %q is not a variable of registry %q",
					e.src.name, e.dst.name, v.name, e.dst.name)
			}
		}
	}
	return nil
}

// describe names the morphism's Map, for domainCheck.imageError.
func (e edgeDef) describe() string {
	return fmt.Sprintf("morphism %s→%s Map", e.src.name, e.dst.name)
}

// resolverName names target's Resolver, for domainCheck.imageError.
func resolverName(target string) func() string {
	return func() string { return fmt.Sprintf("resolver for %q", target) }
}

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
	dom := newDomainCheck(e.dst.vars)

	for _, sa := range srcValid {
		var refShared map[int]uint64 // shared values from the first target, for this source
		for di, sb := range dstValid {
			sb2 := e.mapFn(sa, sb)
			if err := dom.imageError(e.describe, e.dst.name, sb, sb2); err != nil {
				return err
			}
			// Well-formedness: Map must overwrite only Shared() variables.
			for _, v := range dom.vars {
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
	dom := newDomainCheck(target.vars)
	return forEachCombo(srcValids, func(cs []State) error {
		combo := make(map[string]State, len(sources))
		for k, s := range sources {
			combo[s.name] = cs[k]
		}

		var refShared map[int]uint64
		for di, dst := range dstValid {
			merged := resolver(dst, combo)
			if err := dom.imageError(resolverName(target.name), target.name, dst, merged); err != nil {
				return err
			}
			// Well-formedness: the resolver may write only shared variables.
			for _, v := range dom.vars {
				if !sharedIdx[v.index] && merged.getRaw(v) != dst.getRaw(v) {
					return fmt.Errorf("gsm: resolver for %q writes non-shared variable %q; "+
						"a resolver may only set variables declared Shared() on the incoming morphisms", target.name, v.name)
				}
			}
			// Source-determinacy: for fixed sources, the merged shared value is independent of
			// the target's (local) state.
			cur := make(map[int]uint64, len(sharedIdx))
			for vi := range sharedIdx {
				cur[vi] = merged.getRaw(dom.vars[vi])
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
// the negation counterexample) is rejected. The iteration also evaluates repair on states
// that are not valid, so verifyMonotoneVisited then repeats the checks over those (see
// federation_monotone.go).
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
		dom := newDomainCheck(target.vars)
		// shared image (raw values of shared vars) for a source combination. The images are
		// computed once per combination and checked to be states of the target: an embedded
		// sub's closures are not run by verify, so this may be the first time they are.
		images := make([][]uint64, len(points))
		for k, combo := range points {
			var out State
			var err error
			if resolver != nil {
				m := make(map[string]State, len(sources))
				for i, s := range sources {
					m[s.name] = combo[i]
				}
				out = resolver(dst0, m)
				err = dom.imageError(resolverName(target.name), target.name, dst0, out)
			} else {
				out = edges[0].mapFn(combo[0], dst0)
				err = dom.imageError(edges[0].describe, target.name, dst0, out)
			}
			if err != nil {
				return err
			}
			raw := make([]uint64, len(sharedVars))
			for i, v := range sharedVars {
				raw[i] = out.getRaw(v)
			}
			images[k] = raw
		}

		for a := range points {
			for b := range points {
				if pointsLE(points[a], points[b]) && !rawLE(images[a], images[b]) {
					return fmt.Errorf("gsm: repair for %q is not monotone — cyclic federations require "+
						"monotone morphisms/resolvers (a non-monotone repair, e.g. negation, cannot converge "+
						"on cycles; see the Monotone Convergence theorem). Use an acyclic network instead", target.name)
				}
			}
		}
	}
	return f.verifyMonotoneVisited()
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

// CrossOrderError reports two event orders that a federation delivers to different
// federated normal forms: an event of a target registry and a change of the target's
// shared component caused by its source(s). It is the federated counterpart of a
// registry's CCFailure, for the pair of events that sits on two different registries.
//
// Read it as: the target is in State, whose shared component is the image of From (a
// normal form of the source, or a combination of source normal forms for a resolved
// target). The source moves to To (by any of its events, or by a change propagated to it
// from further upstream). Delivering that change first and then Event gives
// SourceFirst; delivering Event first and then the change gives EventFirst.
type CrossOrderError struct {
	Federation  string
	Morphism    string // "src→dst", or "resolver for \"dst\"" on a multi-source target
	Target      string // target registry name
	Event       string // target event
	State       State  // target state (valid, shared component consistent with From)
	From        string // source normal form(s) that produced State's shared component
	To          string // source normal form(s) after the source change
	SourceFirst State  // source change delivered first, then Event
	EventFirst  State  // Event delivered first, then the source change
}

func (e *CrossOrderError) Error() string {
	return fmt.Sprintf("gsm: federation %q: event %q of %q does not commute with a source change across %s "+
		"(cross-registry CC): at target state %s, whose shared component comes from source %s, when the source "+
		"moves to %s, delivering the source change first gives %s but delivering %q first gives %s; the "+
		"target event's guard, effect, or the target's repair reads a morphism-controlled (Shared) variable",
		e.Federation, e.Event, e.Target, e.Morphism, e.State, e.From, e.To, e.SourceFirst, e.Event, e.EventFirst)
}

// verifyCrossOrder checks that every target event commutes with every change of the
// target's shared component that its source(s) can cause: cross-registry CC. Component
// Build checks CC only between events of one registry, and M1/R1/R2 say nothing about a
// target event that reads a shared variable, so without this a federation can deliver
// "source event, target event" and "target event, source event" to different states.
//
// The runtime (FedMachine.Apply, acyclic case) applies a target event as
// b ↦ ow(ρ_B(e(b)), v) and a source change as b ↦ ow(b, v'), where ow(b, v) overwrites b's
// shared component with the image v, v is the current image of the source(s), and ρ_B(e(·))
// is the component machine's Apply (step then normalize). A source change cannot depend on
// the target, and source-determinacy (checked by verifyEdge/verifyResolved) makes
// ow(ow(x, v), v') = ow(x, v'). So the two orders from a consistent state b (shared = v)
// end at the same source state and differ only in the target:
//
//	source first: ow(ρ_B(e(ow(b, v'))), v')
//	event first:  ow(ρ_B(e(b)), v')
//
// Build requires these to be equal for every image v' in Img (the images of the morphism or
// resolver over every valid source state or source combination), every event e of the
// target, and every valid target state b whose shared component is in Img (the target states
// a federated normal form can hold). Because Img is quantified as a whole, the check covers
// a source change from any cause, so a source event, an event further upstream propagated
// along a chain, and a change to any source of a resolved target are all covered by the one
// condition on each target. Pairs of events on the same target are covered by its own CC
// plus C2, which verifyRepairedCC checks in the same pass; events on unrelated registries
// touch disjoint state. This condition is C1 of FederationEvents.v.
//
// Cost per target: |Img| x |{b valid : shared(b) in Img}| x |events| machine lookups, after
// |valid(src)| (or the source-combination count, for a resolver) closure calls to build Img.
// For a single-source target |Img| <= |valid(src)|, so this is within the M1 enumeration
// bound verifyEdge already enforces.
//
// The same checks (C1 and C2), unchanged, certify event order on AllowMonotoneCycles networks.
// The argument above is for the acyclic sweep; on a cycle the reason is different and is
// mechanized in normalization-confluence coq/FederationEventsCyclesCheck.v (cyc_check_gc for any
// normalizer whose shared part is a function of the locals, cyc_check_gc_lfp for normalizeCyclic's
// Kleene iteration). normalizeCyclic resets every controlled variable to bottom before the
// sweeps, so an event's shared writes are erased and only its local outcome survives
// (cyc_check_step). With H_j the set of shared values the repair can write into target j from
// valid states, the theorem's two hypotheses are
//
//	C1cyc: for every event e of j, every valid (x, h) with h in H_j and every h' in H_j,
//	       the locals of ρ_j(e(x, h')) equal the locals of ρ_j(e(x, h));
//	C2cyc: for every pair (a, b) j's CC covers and every valid (x, h) with h in H_j,
//	       the locals of ρ_j(a(locals of ρ_j(b(x, h)), h)) and of ρ_j(b(locals of ρ_j(a(x, h)), h)) agree;
//
// and together they imply GC (every interleaving of independent events converges) from every
// normal form. The checks here are exactly these:
//
//   - Img is H_j. It is the image of the morphism over every valid source state (the source's
//     shared part included, since on a cycle a source can itself be a target), or, for a
//     multi-source target, of the resolver over every combination of valid source states. A
//     multi-source target is therefore checked against the joint image of all its incoming
//     edges, which is the theorem's per-registry H_j directly: no lemma combining per-edge
//     checks into the whole-target condition is used. Img contains every normal form's shared
//     part, because normal forms are valid (verifyMonotoneVisited) and each target's shared
//     part at the least fixed point is the image of its sources there (Hs_img in the theorem).
//   - consistent is the valid target states with shared part in Img, the theorem's (x, h).
//   - ow(·, v2) at the end of both sides of C1, and ow(·, v) at the end of both sides of C2, fix
//     the controlled bits, so comparing the packed states compares the locals only, after the
//     final overwrite, as C1cyc and C2cyc do. The controlled bits (mask) are the variables
//     normalizeCyclic resets.
//   - The checked pairs are the target's own CC pairs (all pairs, or the declared Independent
//     ones), the theorem's I.
//
// The remaining hypotheses of cyc_check_gc_lfp (monotone repair and valid images on every
// visited state, component steps that end valid, phase 1 fixing valid states) are what
// verifyMonotone, verifyMonotoneVisited and Registry.Build establish. Neither C1cyc nor C2cyc
// can be dropped: check_rejects_latch (an event that copies a shared flag into a local fails
// C1) and c1_localcc_insufficient (C1 and each registry's own CC hold, C2 fails, and two
// orders diverge) are regression tests in federation_cycle_events_test.go.
func (f *Federation) verifyCrossOrder(comps []*Machine) error {
	inEdges := make([][]edgeDef, len(f.comps))
	for _, e := range f.edges {
		di := f.idx[e.dst]
		inEdges[di] = append(inEdges[di], e)
	}
	for ti, edges := range inEdges {
		if len(edges) == 0 {
			continue
		}
		if err := f.verifyCrossOrderTarget(f.comps[ti], comps[ti], edges); err != nil {
			return err
		}
	}
	return nil
}

func (f *Federation) verifyCrossOrderTarget(target *Registry, tm *Machine, edges []edgeDef) error {
	resolver, resolved := f.resolvers[target]
	sources, sharedVars := resolverInputs(edges)
	morphism := fmt.Sprintf("morphism %s→%s", edges[0].src.name, target.name)
	if resolved {
		morphism = fmt.Sprintf("resolver for %q", target.name)
	}

	var mask uint64
	for _, v := range sharedVars {
		mask |= uint64((1<<v.bits)-1) << v.offset
	}

	// Img: the shared images the source(s) can produce, keyed by the packed shared bits, each
	// with a witness source normal form (or combination) for the report.
	rep := representativeTarget(target)
	dom := newDomainCheck(target.vars)
	witness := map[uint64]string{}
	var images []uint64
	addImage := func(out State, what func() string, src string) error {
		if err := dom.imageError(what, target.name, rep, out); err != nil {
			return err
		}
		img := out.packed & mask
		if _, ok := witness[img]; !ok {
			witness[img] = src
			images = append(images, img)
		}
		return nil
	}
	if resolved {
		srcValids := make([][]State, len(sources))
		total := 1
		for i, s := range sources {
			srcValids[i] = s.validStates()
			if len(srcValids[i]) == 0 {
				return nil // no valid source combination: nothing to propagate
			}
			if total > maxStateSpace/len(srcValids[i]) {
				return fmt.Errorf("gsm: resolver for %q: cross-registry order check space exceeds %d source combinations",
					target.name, maxStateSpace)
			}
			total *= len(srcValids[i])
		}
		err := forEachCombo(srcValids, func(cs []State) error {
			combo := make(map[string]State, len(sources))
			desc := "{"
			for k, s := range sources {
				combo[s.name] = cs[k]
				if k > 0 {
					desc += ", "
				}
				desc += fmt.Sprintf("%s: %s", s.name, cs[k])
			}
			return addImage(resolver(rep, combo), resolverName(target.name), desc+"}")
		})
		if err != nil {
			return err
		}
	} else {
		e := edges[0]
		for _, sa := range e.src.validStates() {
			if err := addImage(e.mapFn(sa, rep), e.describe, sa.String()); err != nil {
				return err
			}
		}
	}
	if len(images) == 0 {
		return nil // no valid source state: nothing is ever propagated
	}

	inImg := make(map[uint64]bool, len(images))
	for _, img := range images {
		inImg[img] = true
	}
	var consistent []State
	for _, b := range target.validStates() {
		if inImg[b.packed&mask] {
			consistent = append(consistent, State{packed: b.packed, vars: tm.vars})
		}
	}
	ow := func(s State, img uint64) State { return State{packed: (s.packed &^ mask) | img, vars: tm.vars} }

	// C2 runs even when the shared component has a single image: the repair between two target
	// events can erase what the first one wrote into a shared variable although no source moves.
	if err := f.verifyRepairedCC(target, tm, morphism, consistent, mask, witness, ow); err != nil {
		return err
	}
	if len(images) < 2 {
		return nil // the shared component never changes, so no source change can race with an event
	}

	events := tm.Events()
	if len(consistent) > 0 && len(images) > maxStateSpace/len(consistent) {
		return fmt.Errorf("gsm: %s: cross-registry order check space (%d images x %d target states) exceeds %d",
			morphism, len(images), len(consistent), maxStateSpace)
	}

	for _, ev := range events {
		for _, b := range consistent {
			after := tm.Apply(b, ev) // ρ_B(e(b)), computed once per (event, state)
			for _, v2 := range images {
				eventFirst := ow(after, v2)
				sourceFirst := ow(tm.Apply(ow(b, v2), ev), v2)
				if sourceFirst.packed != eventFirst.packed {
					return &CrossOrderError{
						Federation:  f.name,
						Morphism:    morphism,
						Target:      target.name,
						Event:       ev,
						State:       b,
						From:        witness[b.packed&mask],
						To:          witness[v2],
						SourceFirst: sourceFirst,
						EventFirst:  eventFirst,
					}
				}
			}
		}
	}
	return nil
}

// SameTargetOrderError reports two events of one target registry that its own CC check
// accepts but that do not commute in the federation, because the morphism (or resolver)
// repair runs between them: the federated condition C2 ("repaired CC") fails.
//
// Read it as: the target is in State, whose shared component is the image of Source (a
// normal form of the source, or a combination of source normal forms for a resolved
// target), and no source moves. FedMachine.Apply applies a target event as
// b ↦ ow(ρ_B(e(b)), v), so delivering First and then Second gives FirstThenSecond, and
// delivering Second and then First gives SecondThenFirst. The two differ, typically because
// one event writes a shared variable, the repair overwrites it, and the other event reads it.
//
// The check is static: it quantifies over every valid source state and every valid target
// state consistent with it, not only over states a run reaches. So a rejection is sound
// (the witness is a valid, morphism-consistent federated state from which the two orders
// diverge whenever that state can occur) but can be conservative for a federation that
// never reaches the witness.
type SameTargetOrderError struct {
	Federation      string
	Morphism        string // "morphism src→dst", or "resolver for \"dst\"" on a multi-source target
	Target          string // target registry name
	First           string // the event delivered first on the FirstThenSecond side
	Second          string // the other event
	State           State  // target state (valid, shared component consistent with Source)
	Source          string // source normal form(s) that produced State's shared component
	FirstThenSecond State  // First, repair, Second, repair
	SecondThenFirst State  // Second, repair, First, repair
}

func (e *SameTargetOrderError) Error() string {
	return fmt.Sprintf("gsm: federation %q: events %q and %q of %q commute on %q alone (CC) but not with the "+
		"repair across %s between them (repaired CC, C2): at target state %s, whose shared component comes from "+
		"source %s, delivering %q then %q gives %s but %q then %q gives %s, so the federation may diverge on "+
		"these two orders; typically one event writes a morphism-controlled (Shared) variable that the repair "+
		"overwrites before the other event reads it (a static check over every valid source state: the witness "+
		"is a valid consistent state, which a given run may never reach)",
		e.Federation, e.First, e.Second, e.Target, e.Target, e.Morphism, e.State, e.Source,
		e.First, e.Second, e.FirstThenSecond, e.Second, e.First, e.SecondThenFirst)
}

// verifyRepairedCC checks C2 (FederationEvents.v): every pair of target events the target's
// own CC check covers (all pairs, or the declared Independent pairs) commutes when the
// morphism repair runs after each event, with the sources fixed:
//
//	ow(ρ_B(e2(ow(ρ_B(e1(b)), v))), v) = ow(ρ_B(e1(ow(ρ_B(e2(b)), v))), v)
//
// for every image v of a valid source state (or source combination) and every valid target
// state b whose shared component is v (consistent lists exactly these b). The target's own
// CC gives e1(e2(b)) = e2(e1(b)) without the repair, and C1 (verifyCrossOrder) says an event
// does not care whether the source moved before it; neither implies C2 (c2_counterexample),
// and C1 + C2 together make every interleaving of federated-independent events converge in
// an acyclic federation (fed_events_commute) and on a monotone cycle (cyc_check_gc_lfp; see
// verifyCrossOrder). A pair outside the checked set is not declared
// independent, so the federation, like the component, makes no promise about its order.
//
// Cost per target: 4 x |pairs| x |consistent| machine table lookups, where |consistent| is
// at most the target's valid-state count. That is within a constant of the target's own CC
// check (|pairs| x |valid| lookups), which Build already ran under the maxStateSpace cap,
// so no separate cap is needed.
func (f *Federation) verifyRepairedCC(target *Registry, tm *Machine, morphism string, consistent []State,
	mask uint64, witness map[uint64]string, ow func(State, uint64) State) error {
	names := tm.Events()
	for _, b := range consistent {
		v := b.packed & mask
		for _, p := range tm.ccPairs {
			e1, e2 := names[p[0]], names[p[1]]
			x12 := ow(tm.Apply(ow(tm.Apply(b, e1), v), e2), v)
			x21 := ow(tm.Apply(ow(tm.Apply(b, e2), v), e1), v)
			if x12.packed != x21.packed {
				return &SameTargetOrderError{
					Federation:      f.name,
					Morphism:        morphism,
					Target:          target.name,
					First:           e1,
					Second:          e2,
					State:           b,
					Source:          witness[v],
					FirstThenSecond: x12,
					SecondThenFirst: x21,
				}
			}
		}
	}
	return nil
}
