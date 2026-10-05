package gsm

import (
	"errors"
	"fmt"
	"strings"
)

// ErrProjectionNotCertified is wrapped by every error Build returns because a federation that
// called RequireProjectionSafe is not certified for distributed projection merging: a
// *ProjectionOrderError (XU fails) or a structural reason (a cyclic network, a multi-source
// target, a check space over the cap). Test with errors.Is.
var ErrProjectionNotCertified = errors.New("gsm: distributed projection merging not certified")

// RequireProjectionSafe makes Build fail unless distributed projection merging is certified
// (FedReport.ProjectionSafe): every node runs its own component Machine, applies local events
// and merges its sources' projections (SharedProjection, MergeProjection, MergeProjectionAfter)
// whenever they arrive, and the federation must still converge.
//
// Build's default guarantee is the FedMachine model, where the morphism repair runs after every
// event (FedMachine.Apply); there C1 and C2 are exact and are all Build requires. Projection
// merging interleaves repair with local events as separate steps, so a node can apply an event
// to a state whose shared component a local event has just overwritten and the next projection
// has not yet repaired. That model is proved convergent under the stronger condition XU (C1 at
// every valid target state, not only those whose shared component is an image), with each
// component's own CC, in an acyclic federation: dist_interleavings_converge in
// normalization-confluence coq/FederationEvents.v (and fed_thm_fed_convergence_corrected in
// FederationGRS.v). C1 and C2 alone do not suffice there (fed_grs_c1_c2_insufficient).
//
// Build always computes XU and reports it (FedReport.ProjectionSafe, ProjectionWitnesses, and a
// Checks line); with this opt-in an uncertified federation is a build error instead: a
// *ProjectionOrderError naming the target, event, state and the two diverging results when XU
// fails, or an error naming the structural reason. Both wrap ErrProjectionNotCertified.
//
// Cyclic networks (AllowMonotoneCycles) are always reported as not certified, and with this
// opt-in they fail Build. That is required, not conservative: projection nodes have no shared
// reset, so after a raise-then-clear event a feedback loop can hold itself at a fixed point
// larger than the least one the FedMachine computes, and no propagation leaves it, even when
// cyclic C1, C2, monotonicity and FedMachine convergence all hold (dist_cyc_ghost in
// normalization-confluence coq/DistributedCycles.v). Two deployments are certified on a cycle,
// and gsm implements neither as an API. Reset epochs, the general fix: at a barrier every node
// resets its shared values to bottom, then the nodes propagate to quiescence with no events
// inside the epoch; after the epoch every node holds the FedMachine state for the events so far
// (epoch_conv_iff, lens_epoch), and a staggered reset re-creates the ghost, so the reset must be
// a barrier (dist_cyc_epoch_fix). No resets: given cyclic C1 and C2 over a value set covering the
// reachable shared values, the deployment agrees with the FedMachine exactly when it flushes and
// has no reachable ghost (lens_noreset_iff, lens_noreset_fair_iff; flush_agree_iff,
// fair_agree_iff in normalization-confluence coq/DistributedCyclesExact.v). A fair run flushes iff
// it reaches a sound state (fair_flush_sound_iff); no reachable ghost holds iff the start and
// every post-event state are ghost-free (noghost_event_iff), and when every reachable state is
// sound that is staying at or below the least fixed point, checkable per event
// (noghost_soundr_iff, lowr_post_iff). The cheap sufficient checks are inflationary events, per
// event (each raises locals in an order the morphisms are monotone in and never raises a shared
// value: infl_evsound, evlow_lowr), or a unique fixed point of the repair for every assignment of
// the locals, a global check that rules out ghosts (uniq_agree, unique_or_low_noghost; flushing
// still needs its own argument); otherwise both conditions take a global reachability search. A
// clear event on an unpinned feedback loop cannot be certified without resets (ghost_exact). See
// normalization-confluence coq/docs/distributed.md. The FedMachine is unaffected: normalizeCyclic
// resets every shared value to bottom before its Kleene iteration, so a deployment that runs one
// FedMachine (for example over a shared, totally ordered log) always gets the least fixed point.
//
// The opt-in belongs to the federation it is called on; embedding a sub-federation that opted
// in does not opt the parent in.
func (f *Federation) RequireProjectionSafe() *Federation {
	f.requireProjection = true
	return f
}

// ProjectionOrderError reports a failure of XU, the condition under which distributed
// projection merging converges: a target event whose result, after the next merge, depends on
// whether the node merged the projection before applying it.
//
// Read it as: a target node is in State, which is valid but whose shared component need not
// be an image of any source state (a local event may have written it, or the node started
// there). Image is the projection of source state Source, and Merged is State with Image merged
// (MergeProjection). Merging, applying Event, and merging again gives MergeFirst; applying
// Event to State and then merging gives EventFirst. In the FedMachine model the repair runs
// after every event, so a FedMachine never applies an event at such a State, and Build's C1 and
// C2 are unaffected; a distributed node can, and then two nodes that see the same events but
// merge at different times can diverge.
//
// The check is static, over every valid target state, so the witness can be a state a given
// deployment never reaches (for example one that starts consistent and has no local event that
// writes a shared variable).
type ProjectionOrderError struct {
	Federation string
	Morphism   string // "morphism src→dst", or "resolver for \"dst\"" on a multi-source target
	Target     string // target registry name
	Event      string // target event
	State      State  // valid target state (shared component not necessarily an image)
	Source     string // source normal form(s) whose image is merged
	Merged     State  // State with the image merged
	MergeFirst State  // merge, Event, merge
	EventFirst State  // Event, merge
}

func (e *ProjectionOrderError) Error() string {
	return fmt.Sprintf("gsm: federation %q: distributed projection merging is not certified (XU) for event %q of %q "+
		"across %s: at valid target state %s, merging the projection of source %s first (giving %s), then %q, then "+
		"merging again gives %s, but %q first and then merging gives %s; a node that applies %q before the "+
		"projection arrives can end elsewhere than one that merged first (the FedMachine model, which Build's C1 and "+
		"C2 certify, is unaffected; a static check: the state is valid, but a given deployment may never reach it)",
		e.Federation, e.Event, e.Target, e.Morphism, e.State, e.Source, e.Merged, e.Event, e.MergeFirst,
		e.Event, e.EventFirst, e.Event)
}

// Unwrap returns ErrProjectionNotCertified.
func (e *ProjectionOrderError) Unwrap() error { return ErrProjectionNotCertified }

// projectionResult is what verifyProjectionMerge found.
type projectionResult struct {
	safe      bool
	line      string                  // the FedReport.Checks / Projection line
	witnesses []*ProjectionOrderError // the first XU failure on each failing target
	reasons   []string                // why it is not certified, structural reasons first
}

// projectionCheckPrefix starts the FedReport.Checks line for the projection check.
const projectionCheckPrefix = "distributed projection merging (SharedProjection + MergeProjection): "

// verifyProjectionMerge checks XU of normalization-confluence coq/FederationEvents.v on every
// target of an acyclic federation:
//
//	ow(ρ_B(e(ow(b, v))), v) = ow(ρ_B(e(b)), v)
//
// for every image v in Img (the morphism's or resolver's images over every valid source state
// or source combination, as verifyCrossOrder computed them), every event e of the target, and
// EVERY valid target state b. C1 is the same equation restricted to b whose shared component is
// in Img; XU drops that restriction, because a distributed node may apply an event to a state
// whose shared component a local event has overwritten before the next projection arrives. Unlike
// C1, XU is checked even when Img has a single element: the overwrite still differs from the
// state the local event left. Sources need no check (their repair is the identity), and each
// component's own CC, the other hypothesis of dist_interleavings_converge, is what Registry.Build
// checks.
//
// Cost per target: |events| x |valid(target)| x (|Img| + 1) machine table lookups, reusing the
// images C1 computed (no closure calls). A target where |Img| x |valid(target)| exceeds
// maxStateSpace is not checked and is reported as not certified.
//
// The result also records the reasons XU on its own does not certify gsm's projection API: a
// cyclic network (the theorem uses the topological order) and a multi-source target
// (SharedProjection sends one edge's Map image, not the resolver's merge over all sources, which
// is the propagation step the theorem covers). XU is still computed for a multi-source target,
// against the resolver's images, and its witness reported.
func (f *Federation) verifyProjectionMerge(imgs []*targetImages, cyclic bool) projectionResult {
	var res projectionResult
	if cyclic {
		res.reasons = append(res.reasons, "the network is cyclic, and the distributed-propagation theorem "+
			"(dist_interleavings_converge) covers acyclic federations only: on a monotone cycle, nodes that "+
			"merge projections without a shared reset can settle on a larger fixed point than the FedMachine's "+
			"least one (dist_cyc_ghost in normalization-confluence coq/DistributedCycles.v); see "+
			"RequireProjectionSafe for the certified options (barrier reset epochs, or inflationary events or "+
			"a unique fixed point)")
		return res.finish()
	}
	var multi []string
	for _, ti := range imgs {
		if ti == nil {
			continue
		}
		if ti.resolved {
			multi = append(multi, ti.target.name)
		}
		if len(ti.valid) > 0 && len(ti.images) > maxStateSpace/len(ti.valid) {
			res.reasons = append(res.reasons, fmt.Sprintf("XU not checked on %q: %d images x %d valid states exceeds %d",
				ti.target.name, len(ti.images), len(ti.valid), maxStateSpace))
			continue
		}
		if w := f.xuWitness(ti); w != nil {
			res.witnesses = append(res.witnesses, w)
		}
	}
	if len(multi) > 0 {
		res.reasons = append(res.reasons, fmt.Sprintf("multi-source target(s) %q: SharedProjection sends one edge's "+
			"Map image, not the resolver's merge over all sources, which is the propagation step the theorem covers", multi))
	}
	return res.finish()
}

// xuWitness returns the first XU failure on one target (events in declaration order, valid
// states in packed order, images in discovery order), or nil if XU holds there.
func (f *Federation) xuWitness(ti *targetImages) *ProjectionOrderError {
	tm := ti.tm
	for _, ev := range tm.Events() {
		for _, b := range ti.valid {
			after := tm.Apply(b, ev) // ρ_B(e(b)), once per (event, state)
			for _, v := range ti.images {
				merged := ti.ow(b, v)
				if merged.packed == b.packed {
					continue // b is consistent with v: both sides are ow(ρ_B(e(b)), v)
				}
				eventFirst := ti.ow(after, v)
				mergeFirst := ti.ow(tm.Apply(merged, ev), v)
				if mergeFirst.packed != eventFirst.packed {
					return &ProjectionOrderError{
						Federation: f.name,
						Morphism:   ti.morphism,
						Target:     ti.target.name,
						Event:      ev,
						State:      b,
						Source:     ti.witness[v],
						Merged:     merged,
						MergeFirst: mergeFirst,
						EventFirst: eventFirst,
					}
				}
			}
		}
	}
	return nil
}

// finish sets safe and the report line from the witnesses and reasons.
func (res projectionResult) finish() projectionResult {
	if len(res.witnesses) == 0 && len(res.reasons) == 0 {
		res.safe = true
		res.line = projectionCheckPrefix + "certified (XU): every target event, at every valid target state, gives " +
			"the same state after the next merge whether or not the projection was merged before it, so every " +
			"interleaving of local events and merges converges once propagation completes (dist_interleavings_converge)"
		return res
	}
	var why []string
	for _, w := range res.witnesses {
		why = append(why, fmt.Sprintf("XU fails on %q: event %q at valid state %s, with the projection of source %s: "+
			"merge first gives %s, event first gives %s", w.Target, w.Event, w.State, w.Source, w.MergeFirst, w.EventFirst))
	}
	why = append(why, res.reasons...)
	res.line = projectionCheckPrefix + "not certified: " + strings.Join(why, "; ") +
		" (Build's C1 and C2 certify the FedMachine model only; see RequireProjectionSafe)"
	return res
}

// err is the RequireProjectionSafe build error for an uncertified result: the first XU witness
// when there is one, else the structural reasons.
func (res projectionResult) err(fed string) error {
	if res.safe {
		return nil
	}
	if len(res.witnesses) > 0 {
		return res.witnesses[0]
	}
	return fmt.Errorf("%w: federation %q (RequireProjectionSafe): %s", ErrProjectionNotCertified, fed,
		strings.Join(res.reasons, "; "))
}
