package gsm

import (
	"errors"
	"fmt"
)

// ErrStaleProjection is returned (wrapped) by MergeProjectionAfter for a projection whose
// Version is not newer than the last one applied from the same edge. Test with errors.Is.
var ErrStaleProjection = errors.New("gsm: stale projection")

// MergeProjectionAfter is MergeProjection with a freshness and provenance check, for a
// distributed target node whose transport can reorder or redeliver projections. last is the
// Version of the last projection this node applied from p.From (0 if none); the caller keeps it
// per source and, on success, records p.Version as the new last.
//
// It returns s unchanged and an error when:
//   - p.Version is 0 (unversioned: its order cannot be checked; use MergeProjection to opt out);
//   - p.Version <= last (stale or duplicate; the error wraps ErrStaleProjection), so an older
//     projection delivered after a newer one cannot overwrite it;
//   - p.To is set and does not name this machine (a projection meant for another target);
//   - or any of MergeProjection's checks fails.
//
// The merge preserves local validity by M1 only when s is valid and p is the image of a valid
// source state along a single-source morphism (as SharedProjection computes it); the values are
// checked against the variables' domains, not against the morphism's image set. Freshness keeps
// an older projection from overwriting a newer one; it does not make a deployment converge.
// Projection-based deployment is proved convergent under XU (dist_interleavings_converge in
// normalization-confluence coq/FederationEvents.v), which Build reports in
// FedReport.ProjectionSafe and requires only under Federation.RequireProjectionSafe; Build's C1
// and C2 certify the FedMachine model only. See MergeProjection.
//
// Cyclic networks: freshness does not prevent a ghost. On a monotone cycle a feedback loop can
// hold itself at a fixed point larger than the FedMachine's least one even when every projection
// is fresh (dist_cyc_ghost in normalization-confluence coq/DistributedCycles.v), so Build reports
// a cyclic federation as not certified. Barrier reset epochs are the general fix (every node
// resets its shared values to bottom, then propagation to quiescence with no events inside the
// epoch: epoch_conv_iff, lens_epoch). Without resets, a deployment agrees with the FedMachine
// exactly when it flushes and has no reachable ghost (lens_noreset_iff, lens_noreset_fair_iff in
// coq/DistributedCyclesExact.v); the cheap sufficient checks are inflationary events, per event
// (infl_evsound, evlow_lowr), or a unique fixed point for every assignment of the locals, a global
// check (uniq_agree, unique_or_low_noghost), and otherwise it is a global reachability search. gsm
// implements neither route. The FedMachine is unaffected, since it resets the shared values to bottom
// before each Kleene iteration. See Federation.RequireProjectionSafe.
func (m *Machine) MergeProjectionAfter(s State, p Projection, last uint64) (State, error) {
	if p.To != "" && p.To != m.name {
		return s, fmt.Errorf("gsm: MergeProjectionAfter: projection %s→%s is addressed to %q, not machine %q",
			p.From, p.To, p.To, m.name)
	}
	if p.Version == 0 {
		return s, fmt.Errorf("gsm: MergeProjectionAfter: projection %s→%s has no Version; stamp a strictly "+
			"increasing Version at the source, or use MergeProjection to merge without an order check", p.From, p.To)
	}
	if p.Version <= last {
		return s, fmt.Errorf("%w: %s→%s version %d is not newer than the last applied version %d",
			ErrStaleProjection, p.From, p.To, p.Version, last)
	}
	return m.MergeProjection(s, p)
}
