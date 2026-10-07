package gsm

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Migration maps a state of the old configuration to a state of the new one. CheckMigration
// passes the old state and blank, the new registry's zero state, and the migration returns
// a state of the new registry, normally blank with variables set from old (blank.SetInt,
// blank.Set, blank.SetBool). It must be deterministic: CheckMigration evaluates it once per
// old state it reaches. The result need not be valid: the switch normalizes it under the new
// registry, as the deployment must (see CheckMigration).
type Migration func(old, blank State) State

// MigrationOutcome classifies a configuration change. The zero value is MigrationUnknown.
type MigrationOutcome int

const (
	// MigrationUnknown: not certified either way, because the search for a MigrationAmodM
	// witness stopped at the size limit before it was exhaustive (MigrationReport.SearchStopped).
	// An exhausted search decides the outcome (normalization-confluence det_classify_complete:
	// on finite instances every change is safe online, safe behind a barrier or unsafe), so
	// Unknown means only that the limit was hit.
	MigrationUnknown MigrationOutcome = iota
	// MigrationSafeOnline: the switch may happen at any time, with events of the old
	// configuration still in flight; every run converges.
	MigrationSafeOnline
	// MigrationSafeBehindBarrier: every run that drains the old configuration's events
	// before switching converges, and some run that switches with an event in flight does
	// not (MigrationReport.LiveWitness). When the old configuration does not converge on its
	// own, this is certified by an exhausted MigrationAmodM search with no witness.
	MigrationSafeBehindBarrier
	// MigrationUnsafe: two runs that both switch at a barrier diverge
	// (MigrationReport.BarrierWitness).
	MigrationUnsafe
)

// MigrationDelivery is the delivery class CheckMigration checks a change under: how the
// events submitted before and after the switch reach a replica. The zero value is
// MigrationExactlyOnce. Set it with MigrationDeliveryClass.
type MigrationDelivery int

const (
	// MigrationExactlyOnce: every submitted event is applied exactly once. The order is free
	// when neither registry declares Independent pairs. When either does, only declared pairs
	// are reordered (declared independence, normalization-confluence live_declared_exact and
	// barrier_declared_exact): a pair of from's events as from declares it, a pair of to's
	// events as to declares it, and an event of from still in flight at the switch with an
	// event e of to as to declares its translation with e (MigrationInFlightIndependent
	// declares more). Every undeclared pair reaches every replica in one fixed order.
	MigrationExactlyOnce MigrationDelivery = iota
	// MigrationAtLeastOnce: every submitted event is applied at least once, in any order; a
	// redelivery may arrive at any time, after the switch included (an event of from
	// redelivered after it is applied as its translation). Each submission is one message,
	// and an event may be submitted any number of times. Normalization-confluence
	// live_free_alo_exact and barrier_free_alo_exact. Declared Independent pairs with
	// at-least-once delivery across a switch are not covered, so refused.
	MigrationAtLeastOnce
)

// String returns the delivery class as the report prints it.
func (d MigrationDelivery) String() string {
	if d == MigrationAtLeastOnce {
		return "at-least-once"
	}
	return "exactly-once"
}

// String returns the outcome as the report prints it.
func (o MigrationOutcome) String() string {
	switch o {
	case MigrationSafeOnline:
		return "SAFE ONLINE"
	case MigrationSafeBehindBarrier:
		return "SAFE BEHIND A BARRIER"
	case MigrationUnsafe:
		return "UNSAFE"
	}
	return "UNKNOWN"
}

// The keys of MigrationCondition, named after the conditions of normalization-confluence
// coq/Reconfiguration.v, section Det.
const (
	// MigrationPermBStart (online): every order of new-configuration events from the
	// migrated start reaches one state (PermB at M s0). Checked exactly: every pair of
	// new events commutes at every state the new configuration reaches from it.
	MigrationPermBStart = "PermB-start"
	// MigrationDS1 (online): an in-flight event commutes with the switch (DS1): at every
	// state t the old configuration reaches, migrating after the event equals applying the
	// event's translation after migrating, M(Apply_old(t, e)) = Apply_new(M t, tau e).
	MigrationDS1 = "DS1"
	// MigrationPermBEvery (barrier): every order of new events from every migrated reachable
	// state reaches one state (PermB at M t for every reachable t).
	MigrationPermBEvery = "PermB-every"
	// MigrationPermA (barrier): every order of old events from the start reaches one state
	// (PermA): every pair of old events commutes at every state the old configuration reaches.
	MigrationPermA = "PermA"
	// MigrationFaithful (barrier): the migration is injective on the states the old
	// configuration reaches, which makes PermA exact for the barrier outcome.
	MigrationFaithful = "Faithful"
	// MigrationAmodM (barrier): two orders of the same old events never reach states the
	// migration tells apart. Evaluated only when the change is not safe online, PermB-every
	// holds and PermA fails, by an exhaustive search of the pair closure for a witness (under
	// Faithful, a failure of PermA is one). Exact both ways: a witness refutes it, and an
	// exhausted search with none certifies it (amodm_closure_exact, gsm_closure_exact,
	// amodm_witness_exact). Not decided only when the search stops at the size limit.
	MigrationAmodM = "AmodM"

	// The conditions of at-least-once delivery (MigrationAtLeastOnce), named after
	// normalization-confluence coq/ReconfigurationDelivery.v, section FreeALO.

	// MigrationIdemStart (online): every event of to is idempotent at every state to
	// reaches from the migrated start (IdemND over the combined alphabet, live_free_alo_exact):
	// a redelivery, of a native event or of an in-flight event's translation, changes nothing.
	MigrationIdemStart = "Idem-start"
	// MigrationIdemEvery (barrier): every event of to is idempotent at every state to reaches
	// from every migrated reachable state (barrier_free_alo_exact).
	MigrationIdemEvery = "Idem-every"
	// MigrationAbsorbS (barrier): a duplicate straddling the switch is absorbed (AbsorbFree,
	// barrier_free_alo_exact): for every state t from reaches by a run that applied an event
	// e, and every state z to reaches from the image of t, Apply_new(z, tau e) = z.
	MigrationAbsorbS = "AbsorbS"
	// MigrationAmodA (barrier): two runs of from that apply the same set of events, each any
	// number of times, reach states the migration sends to one state (AmodFree,
	// barrier_free_alo_exact). Evaluated only when the change is not safe online and the other
	// barrier conditions hold, by a search over pairs of a state and the set of events
	// applied; not decided only when that search stops at the size limit.
	MigrationAmodA = "AmodA"
)

// MigrationCondition is one condition CheckMigration evaluated.
type MigrationCondition struct {
	Key       string // MigrationPermBStart, MigrationDS1, ...
	Mode      string // "online" or "barrier"
	Desc      string // what it says, in words
	Evaluated bool   // false when it was not needed (MigrationAmodM only)
	Decided   bool   // false when evaluated without a result (MigrationAmodM with the search stopped at the limit)
	Holds     bool   // meaningful when Decided
	Detail    string // the scope checked, or where it fails
}

// MigrationRun is one run of the switch: Before are the events of the old configuration
// applied under it, in order, then the switch migrates the state and normalizes it, then
// After are the events applied under the new configuration, in order. InFlight are the old
// events still in flight at the switch (or, under at-least-once delivery, redelivered after
// it); each is applied after it as its translation, InFlight[k] at After[InFlightAt[k]].
// The witnesses of free delivery apply the translations first: InFlightAt is then 0, 1, ...
type MigrationRun struct {
	Before     []string
	InFlight   []string
	After      []string
	InFlightAt []int
}

// render prints the run; redelivered: its in-flight events are redeliveries of events
// applied before a barrier switch (at-least-once delivery).
func (r MigrationRun) render(from, to string, redelivered bool) string {
	part := func(evs []string, reg string) string {
		if len(evs) == 0 {
			return "nothing under " + reg
		}
		return strings.Join(evs, ", ") + " under " + reg
	}
	sw := "switch"
	if len(r.InFlight) > 0 {
		sw = "switch with " + strings.Join(r.InFlight, ", ") + " in flight"
		if redelivered {
			sw = "switch, " + strings.Join(r.InFlight, ", ") + " redelivered after it"
		}
	}
	// The translations are marked when native events are applied after the switch too.
	after := r.After
	if len(r.InFlight) > 0 && len(r.After) > len(r.InFlight) {
		after = append([]string(nil), r.After...)
		for k, i := range r.InFlightAt {
			if i >= 0 && i < len(after) && k < len(r.InFlight) {
				mark := "in-flight"
				if redelivered {
					mark = "redelivered"
				}
				after[i] = fmt.Sprintf("%s (%s %s)", after[i], mark, r.InFlight[k])
			}
		}
	}
	return part(r.Before, from) + ", " + sw + ", " + part(after, to)
}

// MigrationWitness is a divergence: two runs of the switch from one start, with the same
// events, that end in different states of the new configuration. Both runs are replayed
// before the report is returned.
type MigrationWitness struct {
	Condition        string // the key of the failing condition
	Start            State  // the start, a state of the old registry
	Run1, Run2       MigrationRun
	Result1, Result2 State // the two final states, states of the new registry
	// Barrier: both runs switch at a barrier, every event submitted under the old
	// configuration applied first. Under at-least-once delivery such a run may still receive,
	// after the switch, a redelivery of an event applied before it (InFlight then lists it).
	Barrier bool
	// Redelivery (at-least-once delivery only): the two runs deliver the same set of events,
	// an event that occurs more than once being redelivered, rather than the same events the
	// same number of times.
	Redelivery bool
}

// MigrationCollision is two states the old configuration reaches that the migration (then
// normalization under the new registry) sends to one state: the migration is not faithful.
type MigrationCollision struct {
	State1, State2 State // states of the old registry
	Image          State // their common image, a state of the new registry
}

// MigrationReport is CheckMigration's result.
type MigrationReport struct {
	From, To string           // the two registries' names
	Outcome  MigrationOutcome // see MigrationOutcome
	Reason   string           // why, in one line

	// Delivery is the delivery class checked (MigrationDeliveryClass). Declared is true when
	// a registry declares Independent pairs, so only declared pairs are reordered.
	Delivery MigrationDelivery
	Declared bool

	Starts     []State // the starts checked (states of From)
	StatesFrom int     // states of From reachable from the starts
	StatesTo   int     // states of To reachable from the migrated reachable states

	// Conditions lists every condition evaluated, online first, with the first failure
	// over all starts. Failed is the key of the condition that decided a negative
	// outcome (empty when SafeOnline).
	Conditions []MigrationCondition
	Failed     string

	// Faithful: the migration is injective on the states From reaches from every start;
	// Collision is a counterexample when it is not.
	Faithful  bool
	Collision *MigrationCollision

	// LiveWitness shows why the change is not safe online (nil when it is): two runs, at
	// least one switching with an event in flight, that diverge. BarrierWitness shows why
	// it is unsafe (nil unless Outcome is MigrationUnsafe): two runs that both switch at a
	// barrier and diverge.
	LiveWitness    *MigrationWitness
	BarrierWitness *MigrationWitness

	// Theorems cites the normalization-confluence theorems behind the outcome (in
	// coq/Reconfiguration.v, coq/ReconfigurationClosure.v and coq/Trace.v under free delivery,
	// coq/ReconfigurationDelivery.v under declared independence or at-least-once delivery);
	// docs/theory.md §11.11 maps each to its use.
	Theorems []string

	// SearchStopped is non-empty when the search for a MigrationAmodM witness stopped at the
	// state limit before it was exhaustive; the outcome is then MigrationUnknown.
	SearchStopped string

	// Regime is the regime report for the change (see RegimeSummary): what the outcome
	// guarantees (each line with its theorems), what the deployment must provide, and what
	// is not covered. Set by CheckMigration; String prints it last.
	Regime *RegimeSummary
}

// Condition returns the condition with key k, and whether it was evaluated.
func (r *MigrationReport) Condition(k string) (MigrationCondition, bool) {
	for _, c := range r.Conditions {
		if c.Key == k {
			return c, c.Evaluated
		}
	}
	return MigrationCondition{}, false
}

// String renders the report.
func (r *MigrationReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Migration %s -> %s: %s (%s)\n", r.From, r.To, r.Outcome, r.Reason)
	fmt.Fprintf(&b, "  Model: gsm's runtime: each Apply repairs before it returns; the switch migrates the state "+
		"and normalizes it under %s; %s\n", r.To, r.deliveryText())
	starts := make([]string, len(r.Starts))
	for i, s := range r.Starts {
		starts[i] = s.String()
	}
	fmt.Fprintf(&b, "  Checked from %s; %s of %s reachable, %s of %s reachable after the switch\n",
		strings.Join(starts, ", "), count(r.StatesFrom, "state"), r.From, count(r.StatesTo, "state"), r.To)
	for _, mode := range []string{"online", "barrier"} {
		if mode == "online" {
			b.WriteString("  Online (switch at any time, events in flight):\n")
		} else {
			b.WriteString("  Behind a barrier (drain, then switch):\n")
		}
		for _, c := range r.Conditions {
			if c.Mode != mode || !c.Evaluated {
				continue
			}
			res := "PASS"
			switch {
			case !c.Decided:
				res = "NOT DECIDED"
			case !c.Holds:
				res = "FAIL"
			}
			if c.Key == MigrationFaithful {
				res = map[bool]string{true: "yes", false: "no"}[c.Holds]
			}
			fmt.Fprintf(&b, "    %s [%s]: %s (%s)\n", c.Desc, c.Key, res, c.Detail)
		}
	}
	if w := r.LiveWitness; w != nil {
		fmt.Fprintf(&b, "  Why not online (%s), from %s:\n%s", w.Condition, w.Start, w.render(r.From, r.To))
	}
	if w := r.BarrierWitness; w != nil {
		fmt.Fprintf(&b, "  Why unsafe (%s), from %s:\n%s", w.Condition, w.Start, w.render(r.From, r.To))
	}
	if r.SearchStopped != "" {
		fmt.Fprintf(&b, "  Search: %s\n", r.SearchStopped)
	}
	files := "coq/Reconfiguration.v, coq/ReconfigurationClosure.v, coq/Trace.v"
	if r.Declared || r.Delivery == MigrationAtLeastOnce {
		files = "coq/ReconfigurationDelivery.v"
	}
	fmt.Fprintf(&b, "  Theorems: %s (normalization-confluence %s)\n", strings.Join(r.Theorems, ", "), files)
	if r.Regime != nil {
		b.WriteString(indentBlock(r.Regime.String(), "  "))
	}
	return b.String()
}

// deliveryText describes the delivery class in one clause.
func (r *MigrationReport) deliveryText() string {
	switch {
	case r.Delivery == MigrationAtLeastOnce:
		return "each event is applied at least once, in any order, under the configuration current when it is applied " +
			"(at-least-once delivery)"
	case r.Declared:
		return "each event is applied once, under the configuration current when it is applied; only declared pairs " +
			"are reordered (declared independence)"
	}
	return "each event is applied once, under the configuration current when it is applied"
}

func (w *MigrationWitness) render(from, to string) string {
	out := fmt.Sprintf("    run 1: %s: %s\n    run 2: %s: %s\n",
		w.Run1.render(from, to, w.Barrier), w.Result1, w.Run2.render(from, to, w.Barrier), w.Result2)
	if w.Redelivery {
		out += "    (at-least-once: both runs deliver the same events; a repeated event is a redelivery)\n"
	}
	return out
}

// MigrationOption configures CheckMigration.
type MigrationOption func(*migrationOptions)

type migrationOptions struct {
	starts   []State
	limit    int
	delivery MigrationDelivery
	cross    [][2]string // MigrationInFlightIndependent declarations
}

// MigrationDeliveryClass checks the change under delivery class d instead of the default,
// MigrationExactlyOnce. Use the class the deployment provides: a change safe under
// exactly-once delivery can diverge when an event is redelivered.
func MigrationDeliveryClass(d MigrationDelivery) MigrationOption {
	return func(o *migrationOptions) { o.delivery = d }
}

// MigrationInFlightIndependent declares that an event fromEvent of the old registry still
// in flight at the switch and an event toEvent of the new registry may be applied in either
// order (a cross pair of declared independence), in addition to the cross pairs the new
// registry's own declarations give. A parameterized event named as a whole declares every
// instance. It has an effect only when a registry declares Independent pairs; without
// declarations every pair is reordered already.
func MigrationInFlightIndependent(fromEvent, toEvent string) MigrationOption {
	return func(o *migrationOptions) { o.cross = append(o.cross, [2]string{fromEvent, toEvent}) }
}

// MigrationFrom checks the change from each of the given states of the old registry
// instead of its zero state: a state restored from storage, or a running system's current
// state (the change is then checked for the events still to come). Each must be a state of
// the old registry.
func MigrationFrom(starts ...State) MigrationOption {
	return func(o *migrationOptions) { o.starts = append(o.starts, starts...) }
}

// migrationLimit caps the states CheckMigration enumerates on each side (tests only).
func migrationLimit(n int) MigrationOption {
	return func(o *migrationOptions) { o.limit = n }
}

// CheckMigration classifies a change from the configuration from to the configuration to,
// before it is deployed, as safe online, safe only behind a barrier, or unsafe with a
// witness.
//
// The model is gsm's runtime (docs/deployment.md, "Changing a running system"). A replica
// applies events of from with from's Apply semantics (an invalid input other than the zero
// state is normalized first, and every Apply repairs before it returns), then switches once:
// it migrates its state with migrate and normalizes the result under to. After the switch it
// applies events of to; an event of from still in flight at the switch is applied as its
// translation, events[name] (an event of from missing from events translates to the event of
// to with the same name). By default each event is applied once, in any order (free
// delivery); see MigrationDelivery for declared independence and at-least-once delivery. A
// live switch may happen at any point; a barrier switch only once every event submitted
// under from has been applied. The change is safe online when every run converges: the same
// events from the same start reach one state, wherever each replica switched.
//
// CheckMigration enumerates the states from reaches from each start (default: from's zero
// state; see MigrationFrom), and the states to reaches from their images, and decides:
//
//   - Safe online iff the new configuration converges from the migrated start (PermB-start:
//     every pair of to's events commutes at every state to reaches from it) and every
//     in-flight event commutes with the switch (DS1: at every reachable state t and event e,
//     migrating after e equals applying e's translation after migrating). Exact, both ways:
//     normalization-confluence det_live_exact, with run_tequiv and perm_tequiv_total for the
//     commutation form. Neither from's own convergence nor injectivity is needed.
//   - Otherwise the report carries a live witness, and the barrier is checked. It is safe
//     behind a barrier if the new configuration converges from every migrated reachable state
//     (PermB-every) and from converges from the start (PermA): det_barrier_exact, sufficient
//     always, and exact when the migration is injective on the reachable states
//     (det_barrier_faithful).
//   - When PermB-every holds and PermA fails, the barrier outcome turns on whether the
//     migration absorbs from's divergence (AmodM). CheckMigration searches the pair closure:
//     the pairs of states reached by applying two events in both orders at a reachable state,
//     closed under applying the same event to both. Safe behind a barrier iff the migration
//     sends both states of every pair to one state: det_barrier_closure_exact,
//     amodm_closure_exact, gsm_closure_exact (the pruned search computes that closure) and
//     amodm_witness_exact (an exhausted search with no witness is a certificate).
//   - Unsafe when two barrier runs diverge, with that witness: PermB-every fails, or PermA
//     fails and two orders of the same events reach states the migration tells apart.
//   - Unknown only when that search stops at the size limit before it is exhaustive. On
//     finite instances the outcome is otherwise always decided (det_classify_complete).
//
// Under declared independence (a registry declares Independent pairs; MigrationExactlyOnce)
// the same conditions are checked for the declared pairs only (normalization-confluence
// live_declared_exact, barrier_declared_exact): PermB-start for the declared pairs of the
// combined alphabet (from's pairs translated, to's pairs, and the cross pairs of an in-flight
// event with an event of to), PermB-every for to's declared pairs, and the AmodM closure
// seeded with from's declared pairs only (closureI_swap_exact, closureI_witness_exact). DS1
// is unchanged.
//
// Under MigrationAtLeastOnce (live_free_alo_exact, barrier_free_alo_exact): safe online iff
// PermB-start, Idem-start (every event of to idempotent at every state to reaches from the
// migrated start) and DS1 hold; otherwise safe behind a barrier iff PermB-every, Idem-every,
// AbsorbS (an event applied before the switch and redelivered after it changes nothing) and
// AmodA (runs of from with the same set of events migrate to one state) hold; otherwise
// unsafe, with a witness naming the failing condition.
//
// Every witness is replayed before the report is returned. The registries need not pass
// Build: CheckMigration checks the states runs reach, not every state, so a from that
// diverges or a to that fails CC elsewhere is fine. Each rule must return a state of its
// registry, and repair must terminate on every state a run reaches.
//
// Not covered, so refused: a registry declared with Abstract, Independent pairs under
// at-least-once delivery, more than 64 bits of state, and an invariant without a Repair.
// Causal delivery across the switch is not supported: gsm has no happens-before
// declaration. Federations, collections and projection deployments (propagation in flight)
// are not covered by this function. It returns an error when a side reaches more than 2^20
// states.
func CheckMigration(from, to *Registry, migrate Migration, events map[string]string, opts ...MigrationOption) (*MigrationReport, error) {
	o := migrationOptions{limit: maxStateSpace}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	c, err := newMigCheck(from, to, migrate, events, o)
	if err != nil {
		return nil, err
	}
	return c.check()
}

// migSide is one configuration's runtime step, Machine.Apply's semantics computed from the
// registry's rules on demand.
type migSide struct {
	r   *Registry
	run checkedRules
	nf  map[uint64]uint64
	stp []map[uint64]uint64
}

func newMigSide(r *Registry) *migSide {
	sd := &migSide{r: r, run: r.checked(), nf: map[uint64]uint64{}, stp: make([]map[uint64]uint64, len(r.events))}
	for i := range sd.stp {
		sd.stp[i] = map[uint64]uint64{}
	}
	return sd
}

func (sd *migSide) state(p uint64) State { return State{packed: p, vars: sd.r.vars} }

// normalize repairs p until every invariant holds (Machine.Normalize).
func (sd *migSide) normalize(p uint64) (uint64, error) {
	if q, ok := sd.nf[p]; ok {
		return q, nil
	}
	s := sd.state(p)
	seen := map[uint64]bool{p: true}
	for !sd.r.allInvariantsHold(s) {
		next, err := sd.run.applyFirstRepair(s)
		if err != nil {
			return 0, err
		}
		s = sd.state(next.packed)
		if seen[s.packed] {
			return 0, fmt.Errorf("gsm: CheckMigration: registry %q: repair does not terminate from %s (WFC fails "+
				"at a state a run reaches)", sd.r.name, sd.state(p))
		}
		seen[s.packed] = true
	}
	sd.nf[p] = s.packed
	return s.packed, nil
}

// step is Machine.Apply: normalize an input other than the zero state, apply the event (a
// no-op when its guard is false), and repair.
func (sd *migSide) step(e int, p uint64) (uint64, error) {
	if q, ok := sd.stp[e][p]; ok {
		return q, nil
	}
	in := p
	if in != 0 {
		var err error
		if in, err = sd.normalize(in); err != nil {
			return 0, err
		}
	}
	after, err := sd.run.applyEvent(sd.r.events[e], sd.state(in))
	if err != nil {
		return 0, err
	}
	out, err := sd.normalize(after.packed)
	if err != nil {
		return 0, err
	}
	sd.stp[e][p] = out
	return out, nil
}

func (sd *migSide) runAll(p uint64, evs []int) (uint64, error) {
	var err error
	for _, e := range evs {
		if p, err = sd.step(e, p); err != nil {
			return 0, err
		}
	}
	return p, nil
}

func (sd *migSide) names(evs []int) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = sd.r.events[e].name
	}
	return out
}

type migCheck struct {
	a, b    *migSide
	migrate Migration
	tau     []int // tau[e]: the to-event an in-flight from-event e becomes
	starts  []uint64
	limit   int
	img     map[uint64]uint64 // M = normalize_to o migrate, memoized
	bdom    *domainCheck

	alo      bool       // MigrationAtLeastOnce
	declared bool       // a registry declares Independent pairs: only declared pairs are reordered
	declA    [][2]int   // from's declared pairs (i < j); every pair when from declares none
	declB    [][2]int   // to's declared pairs (i < j); every pair when to declares none
	live     []migXPair // the declared pairs of the combined alphabet, one per pair of to's events
}

// migXEv is an event of the combined alphabet: an event of from in flight at the switch
// (old, applied under to as its translation) or an event of to.
type migXEv struct {
	ev  int
	old bool
}

// migXPair is a declared pair of the combined alphabet.
type migXPair struct{ x, y migXEv }

// bev is the event of to that x is applied as.
func (c *migCheck) bev(x migXEv) int {
	if x.old {
		return c.tau[x.ev]
	}
	return x.ev
}

// declaredPairs returns r's declared pairs, each once with i < j, self-pairs dropped (an
// event always commutes with itself); every pair when r declares none.
func declaredPairs(r *Registry) [][2]int {
	n := len(r.events)
	var out [][2]int
	if r.allIndependent {
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				out = append(out, [2]int{i, j})
			}
		}
		return out
	}
	seen := map[[2]int]bool{}
	for _, p := range r.independent {
		i, j := p[0], p[1]
		if i == j {
			continue
		}
		if i > j {
			i, j = j, i
		}
		if !seen[[2]int{i, j}] {
			seen[[2]int{i, j}] = true
			out = append(out, [2]int{i, j})
		}
	}
	sortPairs(out)
	return out
}

func sortPairs(ps [][2]int) {
	sort.Slice(ps, func(i, j int) bool {
		return ps[i][0] < ps[j][0] || ps[i][0] == ps[j][0] && ps[i][1] < ps[j][1]
	})
}

// eventsNamed resolves name in r: an event, or a parameterized event as a whole (every
// instance).
func eventsNamed(r *Registry, name string) []int {
	for i, ev := range r.events {
		if ev.name == name {
			return []int{i}
		}
	}
	var out []int
	if fi := r.familyByName(name); fi >= 0 {
		for i, ev := range r.events {
			if ev.family == fi+1 {
				out = append(out, i)
			}
		}
	}
	return out
}

func newMigCheck(from, to *Registry, migrate Migration, events map[string]string, o migrationOptions) (*migCheck, error) {
	if from == nil || to == nil {
		return nil, errors.New("gsm: CheckMigration: nil registry")
	}
	if migrate == nil {
		return nil, errors.New("gsm: CheckMigration: nil migration")
	}
	for _, r := range []*Registry{from, to} {
		if err := r.checkNames(); err != nil {
			return nil, err
		}
		if r.abs != nil {
			return nil, fmt.Errorf("gsm: CheckMigration: registry %q is declared with Abstract; the migration check "+
				"enumerates concrete states and has no combined theorem with abstraction (not covered)", r.name)
		}
		if !r.allIndependent && o.delivery == MigrationAtLeastOnce {
			return nil, fmt.Errorf("gsm: CheckMigration: registry %q declares Independent pairs; declared independence "+
				"combined with at-least-once delivery across the switch is not covered (normalization-confluence "+
				"coq/ReconfigurationDelivery.v proves each class on its own; check under MigrationExactlyOnce, or "+
				"without the declarations)", r.name)
		}
		if r.totalBits > 64 {
			return nil, fmt.Errorf("gsm: CheckMigration: registry %q needs %d bits of state, and State holds at most 64",
				r.name, r.totalBits)
		}
		for _, inv := range r.invariants {
			if inv.repair == nil {
				return nil, fmt.Errorf("gsm: CheckMigration: registry %q: invariant %q has no Repair", r.name, inv.name)
			}
		}
	}
	switch o.delivery {
	case MigrationExactlyOnce, MigrationAtLeastOnce:
	default:
		return nil, fmt.Errorf("gsm: CheckMigration: unknown delivery class %d", int(o.delivery))
	}
	c := &migCheck{a: newMigSide(from), b: newMigSide(to), migrate: migrate, limit: o.limit,
		img: map[uint64]uint64{}, bdom: newDomainCheck(to.vars), alo: o.delivery == MigrationAtLeastOnce,
		declared: !from.allIndependent || !to.allIndependent}
	toIdx := make(map[string]int, len(to.events))
	for i, ev := range to.events {
		toIdx[ev.name] = i
	}
	fromIdx := make(map[string]bool, len(from.events))
	for _, ev := range from.events {
		fromIdx[ev.name] = true
	}
	events, err := expandFamilyMap(from, to, events)
	if err != nil {
		return nil, err
	}
	for k := range events {
		if !fromIdx[k] {
			return nil, fmt.Errorf("gsm: CheckMigration: event map names %q, which is not an event of %q", k, from.name)
		}
	}
	c.tau = make([]int, len(from.events))
	for i, ev := range from.events {
		name, ok := events[ev.name]
		if !ok {
			name = ev.name
		}
		j, ok := toIdx[name]
		if !ok {
			return nil, fmt.Errorf("gsm: CheckMigration: event %q of %q translates to %q, which is not an event of %q "+
				"(map every event of %q in events, or declare an event of that name in %q)",
				ev.name, from.name, name, to.name, from.name, to.name)
		}
		c.tau[i] = j
	}
	if len(o.starts) == 0 {
		c.starts = []uint64{0}
	}
	adom := newDomainCheck(from.vars)
	for _, s := range o.starts {
		if err := adom.notStateOf(s); err != nil {
			return nil, fmt.Errorf("gsm: CheckMigration: start %s is not a state of %q: %v", s, from.name, err)
		}
		c.starts = append(c.starts, s.packed)
	}
	if c.alo && len(o.cross) > 0 {
		return nil, errors.New("gsm: CheckMigration: MigrationInFlightIndependent declares a cross pair, and declared " +
			"independence combined with at-least-once delivery across the switch is not covered")
	}
	return c, c.declarePairs(o.cross)
}

// declarePairs sets the declared pairs of each registry and of the combined alphabet
// (normalization-confluence live_declared_exact's IX): from's pairs, as in-flight events;
// to's pairs; and an in-flight event a with an event b of to when to declares tau a with b,
// or MigrationInFlightIndependent declares them. live keeps one entry per unordered pair of
// to's events, preferring to's own pair (its witness needs no event in flight), then a
// cross pair, then a pair of from's events.
func (c *migCheck) declarePairs(extra [][2]string) error {
	from, to := c.a.r, c.b.r
	c.declA, c.declB = declaredPairs(from), declaredPairs(to)
	inB := map[[2]int]bool{}
	for _, p := range c.declB {
		inB[p] = true
	}
	ord := func(i, j int) [2]int {
		if i > j {
			return [2]int{j, i}
		}
		return [2]int{i, j}
	}
	crossSet := map[[2]int]bool{}
	var cross [][2]int
	addCross := func(a, b int) {
		if !crossSet[[2]int{a, b}] {
			crossSet[[2]int{a, b}] = true
			cross = append(cross, [2]int{a, b})
		}
	}
	for a := range from.events {
		for b := range to.events {
			if to.allIndependent || inB[ord(c.tau[a], b)] {
				addCross(a, b)
			}
		}
	}
	for _, d := range extra {
		as, bs := eventsNamed(from, d[0]), eventsNamed(to, d[1])
		if len(as) == 0 {
			return fmt.Errorf("gsm: CheckMigration: MigrationInFlightIndependent names %q, which is not an event of %q",
				d[0], from.name)
		}
		if len(bs) == 0 {
			return fmt.Errorf("gsm: CheckMigration: MigrationInFlightIndependent names %q, which is not an event of %q",
				d[1], to.name)
		}
		for _, a := range as {
			for _, b := range bs {
				addCross(a, b)
			}
		}
	}
	seen := map[[2]int]bool{}
	add := func(x, y migXEv) {
		k := ord(c.bev(x), c.bev(y))
		if k[0] == k[1] || seen[k] {
			return // one event of to, applied twice: the two orders agree
		}
		seen[k] = true
		c.live = append(c.live, migXPair{x, y})
	}
	for _, p := range c.declB {
		add(migXEv{ev: p[0]}, migXEv{ev: p[1]})
	}
	for _, p := range cross {
		add(migXEv{ev: p[0], old: true}, migXEv{ev: p[1]})
	}
	for _, p := range c.declA {
		add(migXEv{ev: p[0], old: true}, migXEv{ev: p[1], old: true})
	}
	return nil
}

// image is M t: migrate, then normalize under to.
func (c *migCheck) image(t uint64) (uint64, error) {
	if x, ok := c.img[t]; ok {
		return x, nil
	}
	blank := c.b.state(0)
	out := c.migrate(c.a.state(t), blank)
	if err := c.bdom.notStateOf(out); err != nil {
		return 0, fmt.Errorf("gsm: CheckMigration: the migration of %s returned %s, which is not a state of %q: %v",
			c.a.state(t), out, c.b.r.name, err)
	}
	x, err := c.b.normalize(out.packed)
	if err != nil {
		return 0, err
	}
	c.img[t] = x
	return x, nil
}

// migXRun is a run of the switch: before under from, the switch, then after under to, where
// an old entry of after is an event of from in flight at the switch (or redelivered after
// it), applied as its translation.
type migXRun struct {
	before []int
	after  []migXEv
}

// native lists events of to as entries of a run's after part.
func native(evs ...int) []migXEv {
	out := make([]migXEv, len(evs))
	for i, e := range evs {
		out[i] = migXEv{ev: e}
	}
	return out
}

// cat concatenates parts of a run into a fresh slice.
func cat[T any](parts ...[]T) []T {
	var out []T
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// replay runs r from start s0: before under from, the switch, after under to.
func (c *migCheck) replay(s0 uint64, r migXRun) (uint64, error) {
	t, err := c.a.runAll(s0, r.before)
	if err != nil {
		return 0, err
	}
	x, err := c.image(t)
	if err != nil {
		return 0, err
	}
	for _, e := range r.after {
		if x, err = c.b.step(c.bev(e), x); err != nil {
			return 0, err
		}
	}
	return x, nil
}

// events splits r's events: those of from (applied before the switch or in flight) and the
// native events of to.
func (r migXRun) events() (old, nat []int) {
	old = append(old, r.before...)
	for _, e := range r.after {
		if e.old {
			old = append(old, e.ev)
		} else {
			nat = append(nat, e.ev)
		}
	}
	return old, nat
}

func (c *migCheck) toRun(r migXRun) MigrationRun {
	out := MigrationRun{Before: c.a.names(r.before)}
	for i, e := range r.after {
		out.After = append(out.After, c.b.r.events[c.bev(e)].name)
		if e.old {
			out.InFlight = append(out.InFlight, c.a.r.events[e.ev].name)
			out.InFlightAt = append(out.InFlightAt, i)
		}
	}
	return out
}

// witness builds a witness for two runs from s0, replays both, and checks they diverge and
// are runs of the same events: under exactly-once delivery the same events of from
// (submitted before the switch) and the same native events of to, each the same number of
// times; under at-least-once delivery the same events, each any number of times.
func (c *migCheck) witness(cond string, barrier bool, s0 uint64, run1, run2 migXRun) (*MigrationWitness, error) {
	r1, err := c.replay(s0, run1)
	if err != nil {
		return nil, err
	}
	r2, err := c.replay(s0, run2)
	if err != nil {
		return nil, err
	}
	if r1 == r2 {
		return nil, fmt.Errorf("gsm: CheckMigration: internal error: the %s witness does not diverge on replay", cond)
	}
	o1, n1 := run1.events()
	o2, n2 := run2.events()
	same := sameMultiset(o1, o2) && sameMultiset(n1, n2)
	redelivery := false
	if !same && c.alo {
		same, redelivery = sameSet(o1, o2) && sameSet(n1, n2), true
	}
	if !same {
		return nil, fmt.Errorf("gsm: CheckMigration: internal error: the %s witness runs have different events", cond)
	}
	return &MigrationWitness{
		Condition: cond, Start: c.a.state(s0), Barrier: barrier, Redelivery: redelivery,
		Run1: c.toRun(run1), Run2: c.toRun(run2),
		Result1: c.b.state(r1), Result2: c.b.state(r2),
	}, nil
}

func sameSet(x, y []int) bool {
	in := func(s []int) map[int]bool {
		m := map[int]bool{}
		for _, e := range s {
			m[e] = true
		}
		return m
	}
	mx, my := in(x), in(y)
	if len(mx) != len(my) {
		return false
	}
	for e := range mx {
		if !my[e] {
			return false
		}
	}
	return true
}

// count renders n things: "1 state", "2 states", "1,048,576 states".
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	if strings.HasPrefix(thing, "pair of ") {
		return groupDigits(fmt.Sprint(n)) + " pairs of " + strings.TrimPrefix(thing, "pair of ")
	}
	return groupDigits(fmt.Sprint(n)) + " " + thing + "s"
}

func sameMultiset(x, y []int) bool {
	if len(x) != len(y) {
		return false
	}
	n := map[int]int{}
	for _, e := range x {
		n[e]++
	}
	for _, e := range y {
		n[e]--
	}
	for _, k := range n {
		if k != 0 {
			return false
		}
	}
	return true
}

// migEdge records how a state was first reached: from state prev by event ev (prev = -1 at
// a root). For a state of to, root is the index of the from-state whose image it descends
// from.
type migEdge struct{ prev, ev, root int }

// migGraph is a breadth-first exploration of one side.
type migGraph struct {
	keys []uint64
	idx  map[uint64]int
	par  []migEdge
}

func newMigGraph() *migGraph { return &migGraph{idx: map[uint64]int{}} }

func (g *migGraph) add(k uint64, e migEdge) (int, bool) {
	if i, ok := g.idx[k]; ok {
		return i, false
	}
	g.idx[k] = len(g.keys)
	g.keys = append(g.keys, k)
	g.par = append(g.par, e)
	return len(g.keys) - 1, true
}

// path returns the events from the root to state i, and the root.
func (g *migGraph) path(i int) (evs []int, root int) {
	for g.par[i].prev >= 0 {
		evs = append(evs, g.par[i].ev)
		i = g.par[i].prev
	}
	for l, r := 0, len(evs)-1; l < r; l, r = l+1, r-1 {
		evs[l], evs[r] = evs[r], evs[l]
	}
	return evs, g.par[i].root
}

// explore closes g under every event of sd, from index from on.
func (c *migCheck) explore(g *migGraph, sd *migSide, from int) error {
	for i := from; i < len(g.keys); i++ {
		for e := range sd.r.events {
			n, err := sd.step(e, g.keys[i])
			if err != nil {
				return err
			}
			if _, fresh := g.add(n, migEdge{prev: i, ev: e, root: g.par[i].root}); fresh && len(g.keys) > c.limit {
				return fmt.Errorf("gsm: CheckMigration: registry %q reaches more than %s states from the states "+
					"checked; the migration check enumerates every state a run reaches (limit %s)", sd.r.name,
					groupDigits(fmt.Sprint(c.limit)), groupDigits(fmt.Sprint(c.limit)))
			}
		}
	}
	return nil
}

// startResult is the outcome from one start.
type startResult struct {
	s0                       uint64
	outcome                  MigrationOutcome
	permBStart, ds1          condResult
	permBEvery, permA, amodM condResult
	idemStart, idemEvery     condResult // at-least-once
	absorb, amodA            condResult // at-least-once
	faithful                 bool
	collision                *MigrationCollision
	live, bar                *MigrationWitness
	amodMEvaluated           bool
	absorbEvaluated          bool
	amodAEvaluated           bool
	closure                  bool // safe behind a barrier, certified by the exhausted AmodM search
	stopped                  string
	nA, nB, nBStart          int
	aStates, bStates         []uint64
}

type condResult struct {
	ok     bool
	detail string
}

func (c *migCheck) fromStart(s0 uint64) (*startResult, error) {
	res := &startResult{s0: s0}
	ga := newMigGraph()
	ga.add(s0, migEdge{prev: -1, root: 0})
	if err := c.explore(ga, c.a, 0); err != nil {
		return nil, err
	}
	res.nA, res.aStates = len(ga.keys), ga.keys
	from, to := c.a.r.name, c.b.r.name
	nEvA := len(c.a.r.events)

	// Images, and faithfulness: the migration injective on the reachable states.
	imgs := make([]uint64, len(ga.keys))
	byImage := map[uint64]int{}
	res.faithful = true
	for i, t := range ga.keys {
		x, err := c.image(t)
		if err != nil {
			return nil, err
		}
		imgs[i] = x
		if j, ok := byImage[x]; ok && res.faithful {
			res.faithful = false
			res.collision = &MigrationCollision{State1: c.a.state(ga.keys[j]), State2: c.a.state(t), Image: c.b.state(x)}
		} else if !ok {
			byImage[x] = i
		}
	}

	// The states of to: first those reachable from the migrated start (the live check),
	// then those reachable from every other migrated reachable state (the barrier check).
	gb := newMigGraph()
	gb.add(imgs[0], migEdge{prev: -1, root: 0})
	if err := c.explore(gb, c.b, 0); err != nil {
		return nil, err
	}
	res.nBStart = len(gb.keys)
	for i := 1; i < len(imgs); i++ {
		before := len(gb.keys)
		if _, fresh := gb.add(imgs[i], migEdge{prev: -1, root: i}); fresh {
			if err := c.explore(gb, c.b, before); err != nil {
				return nil, err
			}
		}
	}
	res.nB, res.bStates = len(gb.keys), gb.keys

	// PermB at the migrated start, and at every migrated reachable state: every pair of to's
	// events under free delivery; under declared independence, the declared pairs of the
	// combined alphabet at the start (live_declared_exact) and to's declared pairs at every
	// migrated state (barrier_declared_exact).
	if err := c.checkPermB(res, ga, gb, len(imgs)); err != nil {
		return nil, err
	}
	if c.alo {
		if err := c.checkIdem(res, ga, gb); err != nil {
			return nil, err
		}
	}

	// DS1: every in-flight event commutes with the switch, at every reachable state.
	res.ds1 = condResult{ok: true, detail: fmt.Sprintf("%s of %s at the %s it reaches",
		count(nEvA, "event"), from, count(res.nA, "state"))}
ds1:
	for i, t := range ga.keys {
		for e := 0; e < nEvA; e++ {
			n, err := c.a.step(e, t)
			if err != nil {
				return nil, err
			}
			lhs, err := c.image(n)
			if err != nil {
				return nil, err
			}
			rhs, err := c.b.step(c.tau[e], imgs[i])
			if err != nil {
				return nil, err
			}
			if lhs == rhs {
				continue
			}
			res.ds1 = condResult{detail: fmt.Sprintf("at %s, %s applied under %s then the switch gives %s; the switch "+
				"then %s under %s gives %s", c.a.state(t), c.a.r.events[e].name, from, c.b.state(lhs),
				c.b.r.events[c.tau[e]].name, to, c.b.state(rhs))}
			if res.live == nil {
				u, _ := ga.path(i)
				ue := append(append([]int(nil), u...), e)
				run1 := migXRun{before: ue}
				run2 := migXRun{before: u, after: []migXEv{{ev: e, old: true}}}
				if res.live, err = c.witness(MigrationDS1, false, s0, run1, run2); err != nil {
					return nil, err
				}
			}
			break ds1
		}
	}

	if c.alo {
		if err := c.classifyALO(res, ga); err != nil {
			return nil, err
		}
		return res, nil
	}

	// PermA: every pair of from's events commutes at every reachable state (under declared
	// independence, every declared pair). Keep every local failure: they seed the AmodM
	// search.
	type localFail struct {
		t      int
		e1, e2 int
		x, y   uint64
	}
	var fails []localFail
	pairsA := count(len(c.declA), "pair") + " of " + from + " events"
	if c.declared {
		pairsA = count(len(c.declA), "declared pair") + " of " + from
	}
	res.permA = condResult{ok: true, detail: fmt.Sprintf("%s at the %s it reaches", pairsA, count(res.nA, "state"))}
	for i, t := range ga.keys {
		for _, p := range c.declA {
			e1, e2 := p[0], p[1]
			x, err := c.a.runAll(t, []int{e1, e2})
			if err != nil {
				return nil, err
			}
			y, err := c.a.runAll(t, []int{e2, e1})
			if err != nil {
				return nil, err
			}
			if x == y {
				continue
			}
			if res.permA.ok {
				res.permA = condResult{detail: fmt.Sprintf("at %s, %s then %s gives %s, %s then %s gives %s",
					c.a.state(t), c.a.r.events[e1].name, c.a.r.events[e2].name, c.a.state(x),
					c.a.r.events[e2].name, c.a.r.events[e1].name, c.a.state(y))}
			}
			fails = append(fails, localFail{t: i, e1: e1, e2: e2, x: x, y: y})
		}
	}

	live := res.permBStart.ok && res.ds1.ok
	switch {
	case live:
		res.outcome = MigrationSafeOnline
	case !res.permBEvery.ok:
		res.outcome = MigrationUnsafe
	case res.permA.ok:
		res.outcome = MigrationSafeBehindBarrier
	default:
		// PermB-every holds (the case above) and PermA fails. By det_barrier_closure_exact the
		// barrier outcome is then exactly the closure condition: M x = M y for every pair
		// (x, y) of the pair closure, seeded with (e2(e1 t), e1(e2 t)) at every reachable t
		// and closed under applying the same event to both (amodm_closure_exact). The search
		// below is gsm_closure_exact's pruned form of that closure: one ordering per pair of
		// distinct events, seeds and successors with equal states skipped (equal states stay
		// equal and have equal images). By amodm_witness_exact it finds a witness iff one
		// exists, so an exhausted search with none certifies the barrier.
		//
		// Under declared independence the seeds are from's declared pairs only, and the
		// barrier outcome is ClosureI, the closure so seeded (barrier_declared_exact,
		// closureI_swap_exact, closureI_witness_exact). The same pruning applies: the
		// declared relation is symmetric, so a pair and its mirror are both in the closure
		// and separated together, and equal states stay equal.
		res.amodMEvaluated = true
		type pairNode struct {
			x, y uint64
			prev int // -1 at a seed
			ev   int
			seed int // index into fails
		}
		var nodes []pairNode
		seen := map[[2]uint64]bool{}
		found := -1
		for k, f := range fails {
			key := [2]uint64{f.x, f.y}
			if seen[key] {
				continue
			}
			seen[key] = true
			nodes = append(nodes, pairNode{x: f.x, y: f.y, prev: -1, seed: k})
		}
	search:
		for i := 0; i < len(nodes); i++ {
			ix, err := c.image(nodes[i].x)
			if err != nil {
				return nil, err
			}
			iy, err := c.image(nodes[i].y)
			if err != nil {
				return nil, err
			}
			if ix != iy {
				found = i
				break
			}
			for e := 0; e < nEvA; e++ {
				nx, err := c.a.step(e, nodes[i].x)
				if err != nil {
					return nil, err
				}
				ny, err := c.a.step(e, nodes[i].y)
				if err != nil {
					return nil, err
				}
				key := [2]uint64{nx, ny}
				if nx == ny || seen[key] {
					continue // equal states stay equal under the same events
				}
				seen[key] = true
				nodes = append(nodes, pairNode{x: nx, y: ny, prev: i, ev: e, seed: nodes[i].seed})
				if len(nodes) > c.limit {
					res.stopped = fmt.Sprintf("the search for two orders of the same events of %s that the migration "+
						"tells apart stopped at %s pairs of states, before it was exhaustive", from,
						groupDigits(fmt.Sprint(c.limit)))
					break search
				}
			}
		}
		if found >= 0 {
			var r []int
			for i := found; nodes[i].prev >= 0; i = nodes[i].prev {
				r = append(r, nodes[i].ev)
			}
			for l, h := 0, len(r)-1; l < h; l, h = l+1, h-1 {
				r[l], r[h] = r[h], r[l]
			}
			f := fails[nodes[found].seed]
			l, _ := ga.path(f.t)
			b1 := append(append(append([]int(nil), l...), f.e1, f.e2), r...)
			b2 := append(append(append([]int(nil), l...), f.e2, f.e1), r...)
			w, err := c.witness(MigrationAmodM, true, s0, migXRun{before: b1}, migXRun{before: b2})
			if err != nil {
				return nil, err
			}
			res.bar = w
			res.amodM = condResult{detail: fmt.Sprintf("two orders of the same %s events reach %s and %s, which "+
				"the migration sends to %s and %s", from, c.a.state(nodes[found].x), c.a.state(nodes[found].y),
				w.Result1, w.Result2)}
			res.outcome = MigrationUnsafe
		} else if res.stopped != "" {
			res.amodM = condResult{detail: res.stopped}
			res.outcome = MigrationUnknown
		} else {
			res.amodM = condResult{ok: true, detail: fmt.Sprintf("the closure is exhausted with no witness: in each "+
				"of the %s reached by applying two %s events in both orders at a reachable state and continuing "+
				"alike, both states migrate to one state", count(len(nodes), "pair of states"), from)}
			res.closure = true
			res.outcome = MigrationSafeBehindBarrier
		}
	}
	return res, nil
}

// pairFail returns the first pair of ps whose two orders, applied under to, differ at y.
func (c *migCheck) pairFail(ps []migXPair, y uint64) (p migXPair, r1, r2 uint64, failed bool, err error) {
	for _, p := range ps {
		x, err := c.b.runAll(y, []int{c.bev(p.x), c.bev(p.y)})
		if err != nil {
			return p, 0, 0, false, err
		}
		z, err := c.b.runAll(y, []int{c.bev(p.y), c.bev(p.x)})
		if err != nil {
			return p, 0, 0, false, err
		}
		if x != z {
			return p, x, z, true, nil
		}
	}
	return migXPair{}, 0, 0, false, nil
}

// xname names an entry of a run: the event of to, and for an in-flight event the event of
// from it translates.
func (c *migCheck) xname(x migXEv) string {
	if x.old {
		return fmt.Sprintf("%s (in-flight %s)", c.b.r.events[c.bev(x)].name, c.a.r.events[x.ev].name)
	}
	return c.b.r.events[x.ev].name
}

// checkPermB evaluates PermB-start and PermB-every from one start, with a witness for each
// failure: two orders of the failing pair after the path to the state.
func (c *migCheck) checkPermB(res *startResult, ga, gb *migGraph, nImgs int) error {
	to := c.b.r.name
	nat := make([]migXPair, len(c.declB))
	for i, p := range c.declB {
		nat[i] = migXPair{migXEv{ev: p[0]}, migXEv{ev: p[1]}}
	}
	startPairs := count(len(c.live), "pair") + " of " + to + " events"
	if c.declared {
		startPairs = count(len(c.live), "declared pair") + " of the combined alphabet"
	}
	everyPairs := count(len(nat), "pair") + " of " + to + " events"
	if c.declared {
		everyPairs = count(len(nat), "declared pair") + " of " + to
	}
	res.permBStart = condResult{ok: true, detail: fmt.Sprintf("%s at the %s %s reaches from the migrated start",
		startPairs, count(res.nBStart, "state"), to)}
	res.permBEvery = condResult{ok: true, detail: fmt.Sprintf("%s at the %s %s reaches from the %s",
		everyPairs, count(res.nB, "state"), to, count(nImgs, "migrated reachable state"))}
	describe := func(y uint64, p migXPair, r1, r2 uint64) string {
		return fmt.Sprintf("at %s, %s then %s gives %s, %s then %s gives %s", c.b.state(y),
			c.xname(p.x), c.xname(p.y), c.b.state(r1), c.xname(p.y), c.xname(p.x), c.b.state(r2))
	}
	for i, y := range gb.keys {
		if i < res.nBStart && res.permBStart.ok {
			p, r1, r2, failed, err := c.pairFail(c.live, y)
			if err != nil {
				return err
			}
			if failed {
				res.permBStart = condResult{detail: describe(y, p, r1, r2)}
				w, _ := gb.path(i)
				run1 := migXRun{after: cat(native(w...), []migXEv{p.x, p.y})}
				run2 := migXRun{after: cat(native(w...), []migXEv{p.y, p.x})}
				if res.live == nil {
					if res.live, err = c.witness(MigrationPermBStart, !p.x.old && !p.y.old, res.s0, run1, run2); err != nil {
						return err
					}
				}
			}
		}
		if res.permBEvery.ok {
			p, r1, r2, failed, err := c.pairFail(nat, y)
			if err != nil {
				return err
			}
			if failed {
				w, root := gb.path(i)
				u, _ := ga.path(root)
				res.permBEvery = condResult{detail: describe(y, p, r1, r2) +
					fmt.Sprintf(", after the barrier switch at %s", c.a.state(ga.keys[root]))}
				run1 := migXRun{before: u, after: cat(native(w...), []migXEv{p.x, p.y})}
				run2 := migXRun{before: u, after: cat(native(w...), []migXEv{p.y, p.x})}
				if res.bar, err = c.witness(MigrationPermBEvery, true, res.s0, run1, run2); err != nil {
					return err
				}
			}
		}
		if !res.permBEvery.ok && (!res.permBStart.ok || i >= res.nBStart) {
			break
		}
	}
	return nil
}

// checkIdem evaluates Idem-start and Idem-every (at-least-once delivery): every event of to
// is idempotent at every state to reaches from the migrated start, and from every migrated
// reachable state. A witness delivers the failing event once against twice.
func (c *migCheck) checkIdem(res *startResult, ga, gb *migGraph) error {
	to := c.b.r.name
	nEvB := len(c.b.r.events)
	res.idemStart = condResult{ok: true, detail: fmt.Sprintf("%s of %s at the %s it reaches from the migrated start",
		count(nEvB, "event"), to, count(res.nBStart, "state"))}
	res.idemEvery = condResult{ok: true, detail: fmt.Sprintf("%s of %s at the %s it reaches from the migrated "+
		"reachable states", count(nEvB, "event"), to, count(res.nB, "state"))}
	for i, y := range gb.keys {
		for b := 0; b < nEvB; b++ {
			once, err := c.b.step(b, y)
			if err != nil {
				return err
			}
			twice, err := c.b.step(b, once)
			if err != nil {
				return err
			}
			if once == twice {
				continue
			}
			detail := fmt.Sprintf("at %s, %s once gives %s, twice gives %s", c.b.state(y), c.b.r.events[b].name,
				c.b.state(once), c.b.state(twice))
			w, root := gb.path(i)
			u, _ := ga.path(root)
			if i < res.nBStart {
				res.idemStart = condResult{detail: detail}
				if res.live == nil {
					run1 := migXRun{after: native(cat(w, []int{b})...)}
					run2 := migXRun{after: native(cat(w, []int{b, b})...)}
					if res.live, err = c.witness(MigrationIdemStart, true, res.s0, run1, run2); err != nil {
						return err
					}
				}
			}
			res.idemEvery = condResult{detail: detail + fmt.Sprintf(", after the barrier switch at %s",
				c.a.state(ga.keys[root]))}
			if res.bar == nil {
				run1 := migXRun{before: u, after: native(cat(w, []int{b})...)}
				run2 := migXRun{before: u, after: native(cat(w, []int{b, b})...)}
				if res.bar, err = c.witness(MigrationIdemEvery, true, res.s0, run1, run2); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return nil
}

// classifyALO decides the outcome from one start under at-least-once delivery
// (live_free_alo_exact, barrier_free_alo_exact). PermB, Idem and DS1 are already evaluated.
//
// gsm's alphabet is the messages: each submission of an event is one message, and an event
// may be submitted any number of times. A duplicate-free sequence of messages can then
// reach every state a sequence of events reaches, so the theorems' commutation and
// idempotence after duplicate-free prefixes (CommND, IdemND) are the checks at every
// reachable state, and their AbsorbFree is the check at every state reached by a run that
// applied the event. AmodFree (runs with the same set of messages) is, over events, runs
// with the same set of events, each any number of times, which depends on the set applied:
// the search for it runs over pairs of a state and that set.
func (c *migCheck) classifyALO(res *startResult, ga *migGraph) error {
	if res.permBStart.ok && res.idemStart.ok && res.ds1.ok {
		res.outcome = MigrationSafeOnline
		return nil
	}
	if !res.permBEvery.ok || !res.idemEvery.ok {
		res.outcome = MigrationUnsafe
		return nil
	}
	res.absorbEvaluated = true
	if err := c.checkAbsorb(res, ga); err != nil {
		return err
	}
	if !res.absorb.ok {
		res.outcome = MigrationUnsafe
		return nil
	}
	res.amodAEvaluated = true
	return c.searchAmodA(res)
}

// checkAbsorb evaluates AbsorbS: for each event a of from, every state z that to reaches
// from the image of a state from reaches by a run that applied a satisfies
// Apply_new(z, tau a) = z. The states that applied a are those reached from a's successors
// of the reachable states; the states of to are those reached from their images.
func (c *migCheck) checkAbsorb(res *startResult, ga *migGraph) error {
	from, to := c.a.r.name, c.b.r.name
	checked := 0
	for a := range c.a.r.events {
		ra := newMigGraph()
		for i, t := range ga.keys {
			n, err := c.a.step(a, t)
			if err != nil {
				return err
			}
			ra.add(n, migEdge{prev: -1, root: i})
		}
		if err := c.explore(ra, c.a, 0); err != nil {
			return err
		}
		rb := newMigGraph()
		for j, t := range ra.keys {
			x, err := c.image(t)
			if err != nil {
				return err
			}
			rb.add(x, migEdge{prev: -1, root: j})
		}
		if err := c.explore(rb, c.b, 0); err != nil {
			return err
		}
		checked += len(rb.keys)
		ta := c.tau[a]
		for k, z := range rb.keys {
			n, err := c.b.step(ta, z)
			if err != nil {
				return err
			}
			if n == z {
				continue
			}
			w, j := rb.path(k)
			cont, i := ra.path(j)
			u, _ := ga.path(i)
			p := cat(u, []int{a}, cont)
			res.absorb = condResult{detail: fmt.Sprintf("at %s, reached under %s after a switch at %s, a redelivery of "+
				"%s, applied as %s, gives %s", c.b.state(z), to, c.a.state(ra.keys[j]), c.a.r.events[a].name,
				c.b.r.events[ta].name, c.b.state(n))}
			run1 := migXRun{before: p, after: native(w...)}
			run2 := migXRun{before: p, after: cat(native(w...), []migXEv{{ev: a, old: true}})}
			res.bar, err = c.witness(MigrationAbsorbS, true, res.s0, run1, run2)
			return err
		}
	}
	res.absorb = condResult{ok: true, detail: fmt.Sprintf("%s of %s, each at the states %s reaches after a switch "+
		"at a state that applied it (%s in all)", count(len(c.a.r.events), "event"), from, to, count(checked, "state"))}
	return nil
}

// searchAmodA evaluates AmodA: runs of from with the same set of events (each any number of
// times) migrate to one state. A breadth-first search over pairs (state, events applied)
// from (s0, none); two pairs with one set whose states migrate apart are a witness, and an
// exhausted search with none certifies it. A search stopped at the size limit leaves the
// outcome Unknown.
func (c *migCheck) searchAmodA(res *startResult) error {
	from := c.a.r.name
	nEvA := len(c.a.r.events)
	type node struct {
		t        uint64
		set      string
		prev, ev int
	}
	type key struct {
		t   uint64
		set string
	}
	nodes := []node{{t: res.s0, set: string(make([]byte, (nEvA+7)/8)), prev: -1}}
	seen := map[key]bool{{res.s0, nodes[0].set}: true}
	first := map[string]int{}
	path := func(i int) []int {
		var out []int
		for ; nodes[i].prev >= 0; i = nodes[i].prev {
			out = append(out, nodes[i].ev)
		}
		for l, h := 0, len(out)-1; l < h; l, h = l+1, h-1 {
			out[l], out[h] = out[h], out[l]
		}
		return out
	}
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		img, err := c.image(n.t)
		if err != nil {
			return err
		}
		j, ok := first[n.set]
		if !ok {
			first[n.set] = i
		} else {
			jimg, err := c.image(nodes[j].t)
			if err != nil {
				return err
			}
			if jimg != img {
				res.bar, err = c.witness(MigrationAmodA, true, res.s0, migXRun{before: path(j)}, migXRun{before: path(i)})
				if err != nil {
					return err
				}
				res.amodA = condResult{detail: fmt.Sprintf("two runs of %s with the same set of events reach %s and %s, "+
					"which the migration sends to %s and %s", from, c.a.state(nodes[j].t), c.a.state(n.t),
					c.b.state(jimg), c.b.state(img))}
				res.outcome = MigrationUnsafe
				return nil
			}
		}
		for e := 0; e < nEvA; e++ {
			nt, err := c.a.step(e, n.t)
			if err != nil {
				return err
			}
			set := []byte(n.set)
			set[e/8] |= 1 << uint(e%8)
			k := key{nt, string(set)}
			if seen[k] {
				continue
			}
			seen[k] = true
			nodes = append(nodes, node{t: nt, set: k.set, prev: i, ev: e})
			if len(nodes) > c.limit {
				res.stopped = fmt.Sprintf("the search for two runs of %s with the same set of events that the migration "+
					"tells apart stopped at %s pairs of a state and a set of events, before it was exhaustive", from,
					groupDigits(fmt.Sprint(c.limit)))
				res.amodA = condResult{detail: res.stopped}
				res.outcome = MigrationUnknown
				return nil
			}
		}
	}
	res.amodA = condResult{ok: true, detail: fmt.Sprintf("the search is exhausted with no witness: in each of the %s "+
		"of a state of %s and the set of events applied to reach it, runs with one set migrate to one state",
		count(len(nodes), "pair"), from)}
	res.outcome = MigrationSafeBehindBarrier
	return nil
}

// severity orders outcomes from best to worst, for combining starts.
func severity(o MigrationOutcome) int {
	switch o {
	case MigrationSafeOnline:
		return 0
	case MigrationSafeBehindBarrier:
		return 1
	case MigrationUnknown:
		return 2
	}
	return 3
}

func (c *migCheck) check() (*MigrationReport, error) {
	from, to := c.a.r, c.b.r
	rep := &MigrationReport{From: from.name, To: to.name, Faithful: true}
	var results []*startResult
	aSeen, bSeen := map[uint64]bool{}, map[uint64]bool{}
	for _, s0 := range c.starts {
		r, err := c.fromStart(s0)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
		rep.Starts = append(rep.Starts, c.a.state(s0))
		for _, k := range r.aStates {
			aSeen[k] = true
		}
		for _, k := range r.bStates {
			bSeen[k] = true
		}
	}
	rep.StatesFrom, rep.StatesTo = len(aSeen), len(bSeen)

	// The outcome is the worst over the starts; each condition holds iff it holds from
	// every start, and reports its first failure.
	worst := results[0]
	for _, r := range results[1:] {
		if severity(r.outcome) > severity(worst.outcome) {
			worst = r
		}
	}
	rep.Outcome = worst.outcome
	combine := func(get func(*startResult) condResult) condResult {
		for _, r := range results {
			if g := get(r); !g.ok {
				if len(results) > 1 {
					g.detail = fmt.Sprintf("from %s: %s", c.a.state(r.s0), g.detail)
				}
				return g
			}
		}
		g := get(results[0])
		if len(results) > 1 {
			g.detail = fmt.Sprintf("from each of %d starts", len(results))
		}
		return g
	}
	pbs := combine(func(r *startResult) condResult { return r.permBStart })
	ds1 := combine(func(r *startResult) condResult { return r.ds1 })
	pbe := combine(func(r *startResult) condResult { return r.permBEvery })
	pa := combine(func(r *startResult) condResult { return r.permA })
	for _, r := range results {
		if !r.faithful {
			rep.Faithful, rep.Collision = false, r.collision
			break
		}
	}
	faithDetail := "on the reachable states of " + from.name
	if c := rep.Collision; c != nil {
		faithDetail = fmt.Sprintf("%s and %s both migrate to %s", c.State1, c.State2, c.Image)
	}
	state := func(s0 uint64) string { return c.a.state(s0).String() }
	rep.Delivery, rep.Declared = MigrationExactlyOnce, c.declared
	if c.alo {
		rep.Delivery = MigrationAtLeastOnce
	}
	startDesc, everyDesc := to.name+" converges from the migrated start", to.name+" converges from every migrated reachable state"
	if c.declared {
		startDesc = "declared pairs commute from the migrated start, in-flight events included"
		everyDesc = to.name + "'s declared pairs commute from every migrated reachable state"
	}
	rep.Conditions = []MigrationCondition{
		{Key: MigrationPermBStart, Mode: "online", Desc: startDesc,
			Evaluated: true, Decided: true, Holds: pbs.ok, Detail: pbs.detail},
	}
	if c.alo {
		is := combine(func(r *startResult) condResult { return r.idemStart })
		rep.Conditions = append(rep.Conditions, MigrationCondition{Key: MigrationIdemStart, Mode: "online",
			Desc: to.name + "'s events absorb a redelivery from the migrated start", Evaluated: true, Decided: true,
			Holds: is.ok, Detail: is.detail})
	}
	rep.Conditions = append(rep.Conditions,
		MigrationCondition{Key: MigrationDS1, Mode: "online", Desc: "in-flight events commute with the switch",
			Evaluated: true, Decided: true, Holds: ds1.ok, Detail: ds1.detail},
		MigrationCondition{Key: MigrationPermBEvery, Mode: "barrier", Desc: everyDesc,
			Evaluated: true, Decided: true, Holds: pbe.ok, Detail: pbe.detail})
	if c.alo {
		ie := combine(func(r *startResult) condResult { return r.idemEvery })
		rep.Conditions = append(rep.Conditions,
			MigrationCondition{Key: MigrationIdemEvery, Mode: "barrier",
				Desc: to.name + "'s events absorb a redelivery from every migrated reachable state", Evaluated: true,
				Decided: true, Holds: ie.ok, Detail: ie.detail},
			lazyCondition(results, state, MigrationAbsorbS,
				"an event redelivered after the switch is absorbed",
				func(r *startResult) bool { return r.absorbEvaluated }, func(r *startResult) condResult { return r.absorb }),
			lazyCondition(results, state, MigrationAmodA,
				"runs of "+from.name+" with the same set of events never migrate apart",
				func(r *startResult) bool { return r.amodAEvaluated }, func(r *startResult) condResult { return r.amodA }))
	} else {
		permADesc, amodMDesc := from.name+" converges from the start", "two orders of the same "+from.name+" events never migrate apart"
		if c.declared {
			permADesc = from.name + "'s declared pairs commute from the start"
			amodMDesc = "two orders of the same " + from.name + " events, related by declared swaps, never migrate apart"
		}
		rep.Conditions = append(rep.Conditions,
			MigrationCondition{Key: MigrationPermA, Mode: "barrier", Desc: permADesc,
				Evaluated: true, Decided: true, Holds: pa.ok, Detail: pa.detail},
			MigrationCondition{Key: MigrationFaithful, Mode: "barrier", Desc: "the migration is injective on the reachable states",
				Evaluated: true, Decided: true, Holds: rep.Faithful, Detail: faithDetail},
			amodMCondition(results, state, amodMDesc))
	}
	rep.SearchStopped = worst.stopped

	// The witnesses come from the start that decided the outcome.
	if rep.Outcome != MigrationSafeOnline {
		rep.LiveWitness = worst.live
	}
	if rep.Outcome == MigrationUnsafe {
		rep.BarrierWitness = worst.bar
	}
	if rep.Outcome != MigrationSafeOnline && rep.Outcome != MigrationUnknown {
		rep.Failed = worst.live.Condition
	}
	if rep.Outcome == MigrationUnsafe {
		rep.Failed = worst.bar.Condition
	}
	rep.Reason = c.reason(rep)
	switch {
	case c.alo:
		aloTheorems(rep)
	case c.declared:
		closure := false
		for _, r := range results {
			closure = closure || r.closure || r.amodMEvaluated && !r.amodM.ok && r.stopped == ""
		}
		declaredTheorems(rep, closure)
	default:
		freeTheorems(rep, results)
	}
	rep.Regime = rep.regimeSummary()
	return rep, nil
}

// reason states the outcome in one line, and sets Failed for an Unknown outcome.
func (c *migCheck) reason(rep *MigrationReport) string {
	from, to := c.a.r.name, c.b.r.name
	switch rep.Outcome {
	case MigrationSafeOnline:
		if c.alo {
			return fmt.Sprintf("in-flight events commute with the migration; %s converges from every migrated state and "+
				"absorbs redeliveries", to)
		}
		return fmt.Sprintf("in-flight events commute with the migration; %s converges from every migrated state", to)
	case MigrationSafeBehindBarrier:
		switch rep.Failed {
		case MigrationDS1:
			return "an event in flight at the switch does not commute with it; drain, then switch"
		case MigrationIdemStart:
			return fmt.Sprintf("an event of %s does not absorb its redelivery from the migrated start; drain, then switch", to)
		case MigrationPermBStart:
			if c.declared {
				return "a declared pair from the migrated start, with an event in flight, does not commute; drain, then switch"
			}
		}
		return fmt.Sprintf("%s diverges from the migrated start; drain, then switch", to)
	case MigrationUnsafe:
		switch rep.Failed {
		case MigrationPermBEvery:
			return fmt.Sprintf("%s diverges after a switch at a barrier", to)
		case MigrationIdemEvery:
			return fmt.Sprintf("an event of %s does not absorb its redelivery after a switch at a barrier", to)
		case MigrationAbsorbS:
			return fmt.Sprintf("an event of %s applied before a barrier switch and redelivered after it changes the "+
				"state", from)
		case MigrationAmodA:
			return fmt.Sprintf("two runs of %s with the same set of events migrate to different states", from)
		}
		return fmt.Sprintf("two orders of the same %s events migrate to different states", from)
	}
	if c.alo {
		rep.Failed = MigrationAmodA
		return fmt.Sprintf("not certified: not safe online, and the search for two runs of %s with the same set of "+
			"events that migrate apart stopped at the size limit before it was exhaustive", from)
	}
	rep.Failed = MigrationAmodM
	return fmt.Sprintf("not certified: not safe online, and the search for two orders of the same %s "+
		"events that migrate apart stopped at the size limit before it was exhaustive", from)
}

// freeTheorems sets the theorems behind an outcome under free delivery (Reconfiguration.v,
// ReconfigurationClosure.v).
func freeTheorems(rep *MigrationReport, results []*startResult) {
	switch rep.Outcome {
	case MigrationSafeOnline:
		rep.Theorems = []string{"det_live_exact", "run_tequiv", "perm_tequiv_total", "det_live_implies_barrier"}
	case MigrationSafeBehindBarrier:
		rep.Theorems = []string{"det_live_exact"}
		closure, permA := false, false
		for _, r := range results {
			closure = closure || r.closure
			permA = permA || (r.outcome == MigrationSafeBehindBarrier && !r.closure)
		}
		switch {
		case rep.Faithful:
			rep.Theorems = append(rep.Theorems, "det_barrier_faithful")
		case permA:
			rep.Theorems = append(rep.Theorems, "det_barrier_exact")
		}
		if closure {
			// The migration absorbs from's own divergence: the exhausted closure search
			// certifies it.
			rep.Theorems = append(rep.Theorems, "det_barrier_closure_exact", "amodm_closure_exact",
				"gsm_closure_exact", "amodm_witness_exact")
		}
		rep.Theorems = append(rep.Theorems, "run_tequiv", "perm_tequiv_total")
	case MigrationUnsafe:
		rep.Theorems = []string{"det_live_exact", "det_barrier_exact"}
		if rep.Faithful {
			rep.Theorems = append(rep.Theorems, "det_barrier_faithful")
		}
	default:
		rep.Theorems = []string{"det_live_exact", "det_barrier_closure_exact"}
	}
}

// declaredTheorems sets the theorems behind an outcome under declared independence
// (ReconfigurationDelivery.v, section DeclaredFree). closure: some start decided the barrier
// by the ClosureI search (exhausted, or a witness).
func declaredTheorems(rep *MigrationReport, closure bool) {
	switch rep.Outcome {
	case MigrationSafeOnline:
		rep.Theorems = []string{"live_declared_exact", "live_implies_barrier_d", "classify_declared_complete"}
	case MigrationUnknown:
		rep.Theorems = []string{"live_declared_exact", "barrier_declared_exact", "closureI_swap_exact"}
	default:
		rep.Theorems = []string{"live_declared_exact", "barrier_declared_exact"}
		if closure {
			rep.Theorems = append(rep.Theorems, "closureI_swap_exact", "closureI_witness_exact")
		}
		rep.Theorems = append(rep.Theorems, "classify_declared_complete")
	}
}

// aloTheorems sets the theorems behind an outcome under at-least-once delivery
// (ReconfigurationDelivery.v, section FreeALO).
func aloTheorems(rep *MigrationReport) {
	switch rep.Outcome {
	case MigrationSafeOnline:
		rep.Theorems = []string{"live_free_alo_exact", "live_implies_barrier_d"}
	default:
		rep.Theorems = []string{"live_free_alo_exact", "barrier_free_alo_exact"}
	}
}

// amodMCondition combines the AmodM search over the starts: evaluated if it was searched from
// some start; failing, with the first witness, if any search found one; not decided if none
// did and some search stopped at the limit; holding if every search was exhausted with none.
func amodMCondition(results []*startResult, state func(uint64) string, desc string) MigrationCondition {
	return lazyCondition(results, state, MigrationAmodM, desc,
		func(r *startResult) bool { return r.amodMEvaluated }, func(r *startResult) condResult { return r.amodM })
}

// lazyCondition combines over the starts a barrier condition evaluated only from some of
// them (AmodM, AbsorbS, AmodA), as amodMCondition describes. A start's search stopped at the
// limit is the one that stopped (startResult.stopped) with that condition evaluated.
func lazyCondition(results []*startResult, state func(uint64) string, key, desc string,
	evaluated func(*startResult) bool, get func(*startResult) condResult) MigrationCondition {
	out := MigrationCondition{Key: key, Mode: "barrier", Desc: desc}
	var fail, stop, hold *startResult
	n := 0
	for _, r := range results {
		if !evaluated(r) {
			continue
		}
		n++
		switch {
		case r.stopped != "" && get(r).detail == r.stopped:
			if stop == nil {
				stop = r
			}
		case !get(r).ok:
			if fail == nil {
				fail = r
			}
		case hold == nil:
			hold = r
		}
	}
	pick := fail
	switch {
	case fail != nil:
		out.Decided = true
	case stop != nil:
		pick = stop
	case hold != nil:
		pick, out.Decided, out.Holds = hold, true, true
	default:
		return out
	}
	out.Evaluated = true
	out.Detail = get(pick).detail
	if len(results) > 1 {
		if out.Holds && n > 1 {
			out.Detail = fmt.Sprintf("from each of the %d starts it was searched from: %s", n, out.Detail)
		} else {
			out.Detail = fmt.Sprintf("from %s: %s", state(pick.s0), out.Detail)
		}
	}
	return out
}

// expandFamilyMap returns the event map with each entry that maps a parameterized event of
// from to one of to expanded into its instances: old(v...) translates to new(v...), for
// every instance of old. The two events must declare the same number of parameters, and
// every value of old's must be a value of new's. Other entries are kept as they are; an
// instance may also be mapped on its own, which overrides the family entry.
func expandFamilyMap(from, to *Registry, events map[string]string) (map[string]string, error) {
	var out map[string]string
	for k, v := range events {
		fi := from.familyByName(k)
		if fi < 0 {
			continue
		}
		f := &from.families[fi]
		ti := to.familyByName(v)
		if ti < 0 {
			return nil, fmt.Errorf("gsm: CheckMigration: event map translates the parameterized event %q of %q to %q, "+
				"which is not a parameterized event of %q (map each instance of %q instead)", k, from.name, v, to.name, k)
		}
		g := &to.families[ti]
		if len(g.params) != len(f.params) {
			return nil, fmt.Errorf("gsm: CheckMigration: event map translates %s of %q to %s of %q, which declares a "+
				"different number of parameters", f.signature(), from.name, g.signature(), to.name)
		}
		for i, p := range f.params {
			if q := g.params[i]; p.Min < q.Min || p.Max > q.Max {
				return nil, fmt.Errorf("gsm: CheckMigration: event map translates %s of %q to %s of %q, but parameter %s "+
					"(%d..%d) has values outside %s (%d..%d)", f.signature(), from.name, g.signature(), to.name, p.Name,
					p.Min, p.Max, q.Name, q.Min, q.Max)
			}
		}
		if out == nil {
			out = make(map[string]string, len(events))
			for k2, v2 := range events {
				if from.familyByName(k2) < 0 {
					out[k2] = v2
				}
			}
		}
		for _, ev := range from.events {
			if ev.family != fi+1 {
				continue
			}
			if _, own := events[ev.name]; !own {
				out[ev.name] = Instance(v, ev.args...)
			}
		}
	}
	if out == nil {
		return events, nil
	}
	return out, nil
}
