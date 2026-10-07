package gsm

import (
	"errors"
	"fmt"
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
// events still in flight at the switch; each is applied after it as its translation, and
// their translations are the first len(InFlight) entries of After.
type MigrationRun struct {
	Before   []string
	InFlight []string
	After    []string
}

func (r MigrationRun) render(from, to string) string {
	part := func(evs []string, reg string) string {
		if len(evs) == 0 {
			return "nothing under " + reg
		}
		return strings.Join(evs, ", ") + " under " + reg
	}
	sw := "switch"
	if len(r.InFlight) > 0 {
		sw = "switch with " + strings.Join(r.InFlight, ", ") + " in flight"
	}
	return part(r.Before, from) + ", " + sw + ", " + part(r.After, to)
}

// MigrationWitness is a divergence: two runs of the switch from one start, with the same
// events, that end in different states of the new configuration. Both runs are replayed
// before the report is returned.
type MigrationWitness struct {
	Condition        string // the key of the failing condition
	Start            State  // the start, a state of the old registry
	Run1, Run2       MigrationRun
	Result1, Result2 State // the two final states, states of the new registry
	Barrier          bool  // both runs switch at a barrier (no event in flight)
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
	// coq/Reconfiguration.v, coq/ReconfigurationClosure.v and coq/Trace.v); docs/theory.md
	// §11.11 maps each to its use.
	Theorems []string

	// SearchStopped is non-empty when the search for a MigrationAmodM witness stopped at the
	// state limit before it was exhaustive; the outcome is then MigrationUnknown.
	SearchStopped string
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
		"and normalizes it under %s; each event is applied once, under the configuration current when it is applied\n", r.To)
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
	fmt.Fprintf(&b, "  Theorems: %s (normalization-confluence coq/Reconfiguration.v, coq/ReconfigurationClosure.v, "+
		"coq/Trace.v)\n",
		strings.Join(r.Theorems, ", "))
	b.WriteString("  Not covered: a switch between an event and its repair (not gsm's runtime); declared " +
		"Independent pairs, causal or at-least-once delivery across the switch; federations, collections and " +
		"projection deployments (propagation in flight)\n")
	return b.String()
}

func (w *MigrationWitness) render(from, to string) string {
	return fmt.Sprintf("    run 1: %s: %s\n    run 2: %s: %s\n",
		w.Run1.render(from, to), w.Result1, w.Run2.render(from, to), w.Result2)
}

// MigrationOption configures CheckMigration.
type MigrationOption func(*migrationOptions)

type migrationOptions struct {
	starts []State
	limit  int
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
// to with the same name). Each event is applied once, in any order (free delivery). A live
// switch may happen at any point; a barrier switch only once every event submitted under
// from has been applied. The change is safe online when every run converges: the same events
// from the same start reach one state, wherever each replica switched.
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
// Every witness is replayed before the report is returned. The registries need not pass
// Build: CheckMigration checks the states runs reach, not every state, so a from that
// diverges or a to that fails CC elsewhere is fine. Each rule must return a state of its
// registry, and repair must terminate on every state a run reaches.
//
// Not covered, so refused: a registry declared with Abstract or with Independent pairs
// (delivery classes across the switch are an open gap), more than 64 bits of state, and
// an invariant without a Repair. Federations, collections and projection deployments
// (propagation in flight) are not covered by this function. It returns an error when a
// side reaches more than 2^20 states.
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
		if !r.allIndependent {
			return nil, fmt.Errorf("gsm: CheckMigration: registry %q declares Independent pairs; the migration "+
				"theorems are for free delivery, and declared independence or causal delivery across the switch is "+
				"an open gap (normalization-confluence REGIME-AUDIT.md gap 20, residue (b); not covered)", r.name)
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
	c := &migCheck{a: newMigSide(from), b: newMigSide(to), migrate: migrate, limit: o.limit,
		img: map[uint64]uint64{}, bdom: newDomainCheck(to.vars)}
	toIdx := make(map[string]int, len(to.events))
	for i, ev := range to.events {
		toIdx[ev.name] = i
	}
	fromIdx := make(map[string]bool, len(from.events))
	for _, ev := range from.events {
		fromIdx[ev.name] = true
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
	return c, nil
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

// replay runs r from start s0: Before under from, the switch, After under to.
func (c *migCheck) replay(s0 uint64, before, after []int) (uint64, error) {
	t, err := c.a.runAll(s0, before)
	if err != nil {
		return 0, err
	}
	x, err := c.image(t)
	if err != nil {
		return 0, err
	}
	return c.b.runAll(x, after)
}

// witness builds a witness for two runs from s0, replays both, and checks they diverge.
//
// Run 2's first len(fly2) events under to are the translations of the from-events fly2, which
// were in flight at its switch; run 1 has none in flight.
func (c *migCheck) witness(cond string, barrier bool, s0 uint64, b1, a1, b2, fly2, a2 []int) (*MigrationWitness, error) {
	r1, err := c.replay(s0, b1, a1)
	if err != nil {
		return nil, err
	}
	r2, err := c.replay(s0, b2, a2)
	if err != nil {
		return nil, err
	}
	if r1 == r2 {
		return nil, fmt.Errorf("gsm: CheckMigration: internal error: the %s witness does not diverge on replay", cond)
	}
	// The two runs must be runs of the same events: the same from-events submitted before the
	// switch, and the same native to-events after it.
	if !sameMultiset(b1, append(append([]int(nil), b2...), fly2...)) || len(a2) < len(fly2) ||
		!sameMultiset(a1, a2[len(fly2):]) {
		return nil, fmt.Errorf("gsm: CheckMigration: internal error: the %s witness runs have different events", cond)
	}
	return &MigrationWitness{
		Condition: cond, Start: c.a.state(s0), Barrier: barrier,
		Run1:    MigrationRun{Before: c.a.names(b1), After: c.b.names(a1)},
		Run2:    MigrationRun{Before: c.a.names(b2), InFlight: c.a.names(fly2), After: c.b.names(a2)},
		Result1: c.b.state(r1), Result2: c.b.state(r2),
	}, nil
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

// commuteFail returns the first pair (e1 < e2) of sd's events whose two orders differ at p.
func commuteFail(sd *migSide, p uint64) (e1, e2 int, r1, r2 uint64, failed bool, err error) {
	n := len(sd.r.events)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			x, err := sd.step(i, p)
			if err != nil {
				return 0, 0, 0, 0, false, err
			}
			if x, err = sd.step(j, x); err != nil {
				return 0, 0, 0, 0, false, err
			}
			y, err := sd.step(j, p)
			if err != nil {
				return 0, 0, 0, 0, false, err
			}
			if y, err = sd.step(i, y); err != nil {
				return 0, 0, 0, 0, false, err
			}
			if x != y {
				return i, j, x, y, true, nil
			}
		}
	}
	return 0, 0, 0, 0, false, nil
}

// startResult is the outcome from one start.
type startResult struct {
	s0                       uint64
	outcome                  MigrationOutcome
	permBStart, ds1          condResult
	permBEvery, permA, amodM condResult
	faithful                 bool
	collision                *MigrationCollision
	live, bar                *MigrationWitness
	amodMEvaluated           bool
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
	nEvA, nEvB := len(c.a.r.events), len(c.b.r.events)

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

	// PermB at the migrated start, and at every migrated reachable state.
	res.permBStart = condResult{ok: true, detail: fmt.Sprintf("%s of %s events at the %s %s reaches from the "+
		"migrated start", count(nEvB*(nEvB-1)/2, "pair"), to, count(res.nBStart, "state"), to)}
	res.permBEvery = condResult{ok: true, detail: fmt.Sprintf("%s of %s events at the %s %s reaches from the %s",
		count(nEvB*(nEvB-1)/2, "pair"), to, count(res.nB, "state"), to, count(len(imgs), "migrated reachable state"))}
	for i, y := range gb.keys {
		e1, e2, r1, r2, failed, err := commuteFail(c.b, y)
		if err != nil {
			return nil, err
		}
		if !failed {
			continue
		}
		w, root := gb.path(i)
		u, _ := ga.path(root)
		v1 := append(append(append([]int(nil), w...), e1), e2)
		v2 := append(append(append([]int(nil), w...), e2), e1)
		detail := fmt.Sprintf("at %s, %s then %s gives %s, %s then %s gives %s", c.b.state(y),
			c.b.r.events[e1].name, c.b.r.events[e2].name, c.b.state(r1),
			c.b.r.events[e2].name, c.b.r.events[e1].name, c.b.state(r2))
		if i < res.nBStart && res.permBStart.ok {
			res.permBStart = condResult{detail: detail}
			if res.live == nil {
				if res.live, err = c.witness(MigrationPermBStart, true, s0, nil, v1, nil, nil, v2); err != nil {
					return nil, err
				}
			}
		}
		res.permBEvery = condResult{detail: detail + fmt.Sprintf(", after the barrier switch at %s", c.a.state(ga.keys[root]))}
		if res.bar, err = c.witness(MigrationPermBEvery, true, s0, u, v1, u, nil, v2); err != nil {
			return nil, err
		}
		break
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
				if res.live, err = c.witness(MigrationDS1, false, s0, ue, nil, u, []int{e}, []int{c.tau[e]}); err != nil {
					return nil, err
				}
			}
			break ds1
		}
	}

	// PermA: every pair of from's events commutes at every reachable state. Keep every
	// local failure: they seed the AmodM search.
	type localFail struct {
		t      int
		e1, e2 int
		x, y   uint64
	}
	var fails []localFail
	res.permA = condResult{ok: true, detail: fmt.Sprintf("%s of %s events at the %s it reaches",
		count(nEvA*(nEvA-1)/2, "pair"), from, count(res.nA, "state"))}
	for i, t := range ga.keys {
		for e1 := 0; e1 < nEvA; e1++ {
			for e2 := e1 + 1; e2 < nEvA; e2++ {
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
			w, err := c.witness(MigrationAmodM, true, s0, b1, nil, b2, nil, nil)
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
	rep.Conditions = []MigrationCondition{
		{Key: MigrationPermBStart, Mode: "online", Desc: to.name + " converges from the migrated start",
			Evaluated: true, Decided: true, Holds: pbs.ok, Detail: pbs.detail},
		{Key: MigrationDS1, Mode: "online", Desc: "in-flight events commute with the switch",
			Evaluated: true, Decided: true, Holds: ds1.ok, Detail: ds1.detail},
		{Key: MigrationPermBEvery, Mode: "barrier", Desc: to.name + " converges from every migrated reachable state",
			Evaluated: true, Decided: true, Holds: pbe.ok, Detail: pbe.detail},
		{Key: MigrationPermA, Mode: "barrier", Desc: from.name + " converges from the start",
			Evaluated: true, Decided: true, Holds: pa.ok, Detail: pa.detail},
		{Key: MigrationFaithful, Mode: "barrier", Desc: "the migration is injective on the reachable states",
			Evaluated: true, Decided: true, Holds: rep.Faithful, Detail: faithDetail},
		amodMCondition(results, func(s0 uint64) string { return c.a.state(s0).String() },
			"two orders of the same "+from.name+" events never migrate apart"),
	}
	rep.SearchStopped = worst.stopped

	// The witnesses come from the start that decided the outcome.
	if rep.Outcome != MigrationSafeOnline {
		rep.LiveWitness = worst.live
	}
	if rep.Outcome == MigrationUnsafe {
		rep.BarrierWitness = worst.bar
	}
	switch rep.Outcome {
	case MigrationSafeOnline:
		rep.Reason = fmt.Sprintf("in-flight events commute with the migration; %s converges from every migrated state", to.name)
		rep.Theorems = []string{"det_live_exact", "run_tequiv", "perm_tequiv_total", "det_live_implies_barrier"}
	case MigrationSafeBehindBarrier:
		rep.Failed = worst.live.Condition
		if rep.Failed == MigrationDS1 {
			rep.Reason = "an event in flight at the switch does not commute with it; drain, then switch"
		} else {
			rep.Reason = fmt.Sprintf("%s diverges from the migrated start; drain, then switch", to.name)
		}
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
		rep.Failed = worst.bar.Condition
		if rep.Failed == MigrationPermBEvery {
			rep.Reason = fmt.Sprintf("%s diverges after a switch at a barrier", to.name)
		} else {
			rep.Reason = fmt.Sprintf("two orders of the same %s events migrate to different states", from.name)
		}
		rep.Theorems = []string{"det_live_exact", "det_barrier_exact"}
		if rep.Faithful {
			rep.Theorems = append(rep.Theorems, "det_barrier_faithful")
		}
	default:
		rep.Failed = MigrationAmodM
		rep.Reason = fmt.Sprintf("not certified: not safe online, and the search for two orders of the same %s "+
			"events that migrate apart stopped at the size limit before it was exhaustive", from.name)
		rep.Theorems = []string{"det_live_exact", "det_barrier_closure_exact"}
	}
	return rep, nil
}

// amodMCondition combines the AmodM search over the starts: evaluated if it was searched from
// some start; failing, with the first witness, if any search found one; not decided if none
// did and some search stopped at the limit; holding if every search was exhausted with none.
func amodMCondition(results []*startResult, state func(uint64) string, desc string) MigrationCondition {
	out := MigrationCondition{Key: MigrationAmodM, Mode: "barrier", Desc: desc}
	var fail, stop, hold *startResult
	n := 0
	for _, r := range results {
		if !r.amodMEvaluated {
			continue
		}
		n++
		switch {
		case r.stopped != "":
			if stop == nil {
				stop = r
			}
		case !r.amodM.ok:
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
	out.Detail = pick.amodM.detail
	if len(results) > 1 {
		if out.Holds && n > 1 {
			out.Detail = fmt.Sprintf("from each of the %d starts it was searched from: %s", n, out.Detail)
		} else {
			out.Detail = fmt.Sprintf("from %s: %s", state(pick.s0), out.Detail)
		}
	}
	return out
}
