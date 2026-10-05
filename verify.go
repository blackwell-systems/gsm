package gsm

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// maxStateSpace is the default ceiling on enumerable states.
const maxStateSpace = 1 << 20 // ~1M states

// Report contains the results of build-time verification.
type Report struct {
	Name       string
	StateCount int
	VarCount   int
	EventCount int

	// WFC results
	WFC          bool
	MaxRepairLen int // longest compensation chain

	// CC results
	CC            bool
	PairsTotal    int
	PairsDisjoint int        // proved by verified footprint disjointness (BuildCompositional only; always 0 for Build)
	PairsBrute    int        // proved by exhaustive check
	CCFailure     *CCFailure // non-nil if CC failed

	// PairsUndeclared is the number of event pairs outside the declared Independent
	// set that Build also checked (0 when every pair is checked, the default).
	PairsUndeclared int

	// CausalOrderRequired lists the undeclared event pairs (Registry.Independent
	// switches to declared-only mode) that do NOT commute, each with a witness state
	// and the two results. Build does not fail on them, but convergence then holds
	// only if each listed pair is delivered in causal order: the two events reach
	// every replica in the same fixed order. Empty when every pair commutes or when
	// every pair was certified.
	CausalOrderRequired []CCFailure

	// NotIdempotent lists the events for which applying the event twice reaches a
	// different state than applying it once, from some state Build checked. The
	// convergence guarantee is about permutations of one multiset of events: each
	// event delivered exactly once. A listed event delivered twice (an at-least-once
	// queue redelivering it) changes the result, so its duplicates must be
	// suppressed (an event id and a dedupe set, for example) before Apply.
	NotIdempotent []string

	// Saturations lists the rules whose write to an Int or enum variable was
	// clamped into the variable's range (SetInt, Inc, Dec, combinator Set) on some
	// state Build ran the rule on. Clamping is part of the verified semantics, so
	// convergence is unaffected, but an invariant meant to catch the overflow never
	// sees it. Recorded for writes made on states Build passed to the rule.
	Saturations []Saturation

	// Coordinated lists, on the report of a federation component, the morphism edges
	// into it that BuildCoordinated removed: their shared variables are external
	// inputs that the coordination mechanism (a single writer, a lock, a consensus
	// round) must serialize, and each write must leave the component valid
	// (Normalize after writing). gsm does not check that coordination. Each point's
	// Authority is this component: its coordinated values are the root the normal form
	// of every cycle the removed edge broke is driven from.
	Coordinated []CoordinationPoint

	// Compositional (BuildCompositional) results
	Components         int  // number of footprint components verified
	MaxComponentStates int  // largest component subspace enumerated
	FootprintChecked   bool // closures verified to respect declared footprints

	// FootprintViolation is non-empty when BuildCompositional rejected the machine
	// because a closure does not respect its declared footprint. The rejection
	// happens before WFC and CC run, so neither was evaluated and WFC/CC are false
	// for that reason, not because either check failed.
	FootprintViolation string

	// DomainViolation is non-empty when the build stopped because a rule (an event's
	// effect or an invariant's repair) returned something that is not a state of the
	// machine; it holds the error. Verification stopped at that rule, so WFC and CC are
	// false: the machine was not certified, whatever had been checked before.
	DomainViolation string

	// Assurance says what certified the machine (see Assurance). It is
	// AssuranceNone unless the build returned a machine.
	Assurance Assurance

	// OracleDisagreement is non-empty when gsm's verification passed but the verified
	// table oracle did not certify the tables (it rejected them, or could not check
	// them); it holds the error. There is no machine: the build fails closed.
	OracleDisagreement string

	// RulesOracleSkipped is non-empty when Build certified the machine with the
	// table oracle alone; it says why the rules oracle did not run (no
	// combinator rules, above RulesOracleMaxWork, or outside the rules
	// oracle's fragment).
	RulesOracleSkipped string
}

// compensationError is Build's error when the compensation as written is missing or does
// not converge (a Repair is missing, WFC fails, or CC fails): the failures a synthesized
// compensation can repair, so BuildOrSynthesize falls back to synthesis on these alone.
type compensationError struct{ msg string }

func (e *compensationError) Error() string { return e.msg }

// noteDomainViolation records err in the report when it is a rule result outside the
// machine (resultError), replacing any footprint violation the same error was filed as.
func (r *Report) noteDomainViolation(err error) {
	var re *resultError
	if r == nil || !errors.As(err, &re) {
		return
	}
	r.DomainViolation = err.Error()
	r.FootprintViolation = ""
	r.WFC, r.CC = false, false
}

// CCFailure describes a specific CC violation.
type CCFailure struct {
	Event1  string
	Event2  string
	State   State
	Result1 State // apply e1 then e2
	Result2 State // apply e2 then e1
}

// Saturation records a rule whose write was clamped into a variable's range.
type Saturation struct {
	Rule   string // `event "deposit"` or `invariant "cap" repair`
	Var    string // the variable written
	States int    // how many input states the rule saturated on
}

func (s Saturation) String() string {
	return fmt.Sprintf("%s clamps %q into its range on %d state(s)", s.Rule, s.Var, s.States)
}

func (r *Report) String() string {
	s := fmt.Sprintf("Machine: %s\n", r.Name)
	s += fmt.Sprintf("  Variables: %d\n", r.VarCount)
	if r.Components > 0 {
		// BuildCompositional never enumerates the global state space.
		s += fmt.Sprintf("  Components: %d\n", r.Components)
	} else {
		s += fmt.Sprintf("  States: %d\n", r.StateCount)
	}
	s += fmt.Sprintf("  Events: %d\n", r.EventCount)
	s += "\n"

	if r.DomainViolation != "" {
		// The build stopped at a rule whose result is outside the machine.
		s += "  Rule results: FAIL\n"
		s += fmt.Sprintf("    %s\n", r.DomainViolation)
		s += "  WFC: not certified (a rule result is not a state of the machine)\n"
		s += "  CC (Compensation Commutativity): not certified (a rule result is not a state of the machine)\n"
		return s
	}
	if r.FootprintViolation != "" {
		// The build stopped before WFC and CC ran; report the cause, not a WFC failure.
		s += "  Footprint conformance: FAIL\n"
		s += fmt.Sprintf("    %s\n", r.FootprintViolation)
		s += "  WFC: not evaluated (footprint violation)\n"
		s += "  CC (Compensation Commutativity): not evaluated (footprint violation)\n"
		return s
	}
	if r.FootprintChecked {
		s += fmt.Sprintf("  Footprint conformance: PASS (largest component: %d states)\n", r.MaxComponentStates)
	}

	if r.WFC {
		s += fmt.Sprintf("  WFC: PASS (max repair depth: %d)\n", r.MaxRepairLen)
	} else {
		s += "  WFC: FAIL (compensation does not terminate)\n"
	}

	if r.CC {
		s += fmt.Sprintf("  CC (Compensation Commutativity): PASS (%d pairs: %d disjoint, %d brute-force)\n",
			r.PairsTotal, r.PairsDisjoint, r.PairsBrute)
	} else if r.CCFailure != nil {
		s += "  CC (Compensation Commutativity): FAIL\n"
		s += fmt.Sprintf("    Events: (%s, %s)\n", r.CCFailure.Event1, r.CCFailure.Event2)
		s += fmt.Sprintf("    State:  %s\n", r.CCFailure.State)
		s += fmt.Sprintf("    %s→%s: %s\n", r.CCFailure.Event1, r.CCFailure.Event2, r.CCFailure.Result1)
		s += fmt.Sprintf("    %s→%s: %s\n", r.CCFailure.Event2, r.CCFailure.Event1, r.CCFailure.Result2)
	}

	if r.OracleDisagreement != "" {
		s += "\n  Verified oracle: did not certify\n"
		s += fmt.Sprintf("    %s\n", r.OracleDisagreement)
		s += "  Convergence: NOT CERTIFIED (no machine)\n"
		return s
	}
	if r.CC && r.PairsUndeclared > 0 {
		if n := len(r.CausalOrderRequired); n == 0 {
			s += fmt.Sprintf("  Undeclared pairs: %d checked, all commute (order free)\n", r.PairsUndeclared)
		} else {
			s += fmt.Sprintf("  Undeclared pairs: %d checked, %d do not commute: each must be causally ordered "+
				"(not independent), delivered in the same order at every replica\n", r.PairsUndeclared, n)
			for _, f := range r.CausalOrderRequired {
				s += fmt.Sprintf("    (%s, %s) from %s: %s→%s gives %s, %s→%s gives %s\n", f.Event1, f.Event2, f.State,
					f.Event1, f.Event2, f.Result1, f.Event2, f.Event1, f.Result2)
			}
		}
	}
	if r.WFC && r.CC && r.Assurance != AssuranceNone {
		if n := len(r.CausalOrderRequired); n > 0 {
			s += fmt.Sprintf("\n  Convergence: GUARANTEED under causal delivery of the %d undeclared pair(s) above\n", n)
		} else {
			s += "\n  Convergence: GUARANTEED\n"
		}
		s += fmt.Sprintf("  Assurance: %s\n", r.Assurance)
		if r.RulesOracleSkipped != "" {
			s += fmt.Sprintf("  Rules oracle: not run: %s\n", r.RulesOracleSkipped)
		}
	} else {
		s += fmt.Sprintf("\n  Assurance: %s\n", r.Assurance)
	}
	s += r.obligations()

	return s
}

// obligations renders what the guarantee assumes of the runtime and of the rules
// beyond what Build certified: exactly-once delivery of non-idempotent events,
// silent saturation, and federation edges left to external coordination.
func (r *Report) obligations() string {
	var b strings.Builder
	if len(r.NotIdempotent) > 0 {
		fmt.Fprintf(&b, "  Delivery: exactly once for %s (applying one twice differs from once); "+
			"deduplicate redelivered events\n", strings.Join(r.NotIdempotent, ", "))
	}
	for _, sat := range r.Saturations {
		fmt.Fprintf(&b, "  Saturation: %s (silently; an invariant testing the bound never sees the overflow)\n", sat)
	}
	for _, cp := range r.Coordinated {
		fmt.Fprintf(&b, "  Coordinated input: %s is set by external coordination, not by a morphism; "+
			"%s is the authority the normal form of the cycles it breaks depends on; "+
			"serialize its writes and Normalize after each (not checked by gsm)\n", cp, cp.Authority)
	}
	return b.String()
}

// Build verifies WFC and CC, then returns an immutable Machine.
//
// Build runs every event effect on every valid encoding and every repair while computing
// normal forms, and returns an error if any of them returns something that is not a state
// of this machine (see EffectFunc): a different variable schema, a variable outside its
// range, or bits outside the encoding. The error names the event or invariant, the input
// state and the result. Membership is by value, so a state built from another machine
// with an identical variable declaration list is accepted.
//
// When gsm's verification passes, the machine's tables also go to the verified table
// oracle: check_fn from the normalization-confluence proof, generated as Go from the
// Rocq extraction (internal/oracle). The machine is returned only if the oracle certifies
// the tables. Build then runs the verified rules oracle (checkBuild, generated the same
// way) on the machine's combinator rules, when it can (see RulesOracleMaxWork):
// Report.Assurance is AssuranceOracleTablesAndRules when both certified, and
// AssuranceOracleTables when the rules oracle did not run (Report.RulesOracleSkipped says
// why). If either oracle rejects the machine or cannot check it, Build returns an error
// and no machine (it fails closed), and Report.OracleDisagreement says why.
func (r *Registry) Build() (*Machine, *Report, error) {
	m, rep, err := r.build(true)
	if err == nil {
		// The oracle gate: the verified table oracle must certify the tables too.
		if err = certifyMachine(m); err != nil {
			rep.failClosed(err)
			m = nil
		} else if skipped, rerr := certifyRules(r, rep.MaxRepairLen); rerr != nil {
			// The rules oracle: it must certify the rules too, when it runs.
			err = rerr
			rep.failClosed(err)
			m = nil
		} else if skipped != "" {
			rep.Assurance, rep.RulesOracleSkipped = AssuranceOracleTables, skipped
		} else {
			rep.Assurance = AssuranceOracleTablesAndRules
		}
	}
	if buildObserver != nil {
		buildObserver(r, m, rep, err)
	}
	return m, rep, err
}

// buildObserver, when non-nil, sees every Build result. This package's tests set
// it (once, before any test runs), to cross-check every machine the test suite
// builds against the extracted checkers (oracle_differential_test.go), and so does
// the example-machine gate (gate.go, built only with the gsmgate tag).
var buildObserver func(r *Registry, m *Machine, rep *Report, err error)

// machineObserver, when non-nil, sees every machine handed out without Build: kind
// "synthesized" (Synthesis.Machine) or "compositional" (BuildCompositional). Only
// the example-machine gate sets it (gate.go, built with the gsmgate tag).
var machineObserver func(kind string, r *Registry, m *Machine)

// build is Build with a switch to skip Phase 3 (CC verification). runCC=false is used only by
// Federation.DiagnoseCycle, which needs a component's normal forms to iterate a loop and makes no
// convergence claim about the component. Nothing that certifies convergence skips CC: certified
// components embedded with EmbedCertified are rebuilt with Build, so a certificate's verdict is
// re-checked, never trusted.
func (r *Registry) build(runCC bool) (_ *Machine, rep *Report, err error) {
	if err = r.checkNames(); err != nil {
		return nil, nil, err
	}
	before := r.shape()
	// A rule that declares on the registry mid-run can also make a later check fail (a
	// result built after a declaration has the new variable list, so it is not a state of
	// the machine being verified). The change is the cause, so report it instead.
	defer func() {
		if err != nil {
			if gerr := r.checkUnchanged(before); gerr != nil {
				err = gerr
			}
			rep.noteDomainViolation(err)
		}
	}()
	if r.totalBits > 20 {
		return nil, nil, fmt.Errorf("gsm: state space too large (%d bits, max 20)", r.totalBits)
	}
	for _, inv := range r.invariants {
		if inv.repair == nil {
			return nil, nil, &compensationError{fmt.Sprintf("gsm: invariant %q has no Repair; provide one, or call "+
				"Synthesize to generate a convergent compensation", inv.name)}
		}
	}

	stateCount := 1
	for _, v := range r.vars {
		if v.domain > 0 && stateCount > maxStateSpace/v.domain {
			return nil, nil, fmt.Errorf("gsm: state space overflow (exceeds limit %d)", maxStateSpace)
		}
		stateCount *= v.domain
	}
	if stateCount > maxStateSpace {
		return nil, nil, fmt.Errorf("gsm: state space %d exceeds limit %d", stateCount, maxStateSpace)
	}

	packedCount := 1 << r.totalBits

	report := &Report{
		Name:       r.name,
		StateCount: stateCount,
		VarCount:   len(r.vars),
		EventCount: len(r.events),
	}

	// Build validity mask
	valid := make([]bool, packedCount)
	for i := 0; i < packedCount; i++ {
		valid[i] = r.isValidEncoding(uint64(i))
	}

	// The rules run on states over a build-private copy of the variable list, so a
	// write that saturates (SetInt, combinator Set) can be attributed to this build
	// (see clampRecorder). The copy is the same schema, so rule results are states of
	// the machine exactly as before.
	bvars := append([]Var(nil), r.vars...)
	mkState := func(id uint64) State {
		return State{packed: id, vars: bvars}
	}
	rec := watchClamps(bvars)
	defer rec.stop()
	run := checkedRules{r: r, dom: newDomainCheck(bvars), clamp: rec}

	// Phase 1: Verify WFC and compute normal forms
	nf, err := r.computeNormalForms(run, packedCount, stateCount, valid, mkState, report)
	if err != nil {
		return nil, report, err
	}

	// Phase 2: Compute step tables
	step, err := r.computeStepTables(run, packedCount, valid, nf, mkState)
	if err != nil {
		return nil, report, err
	}
	report.Saturations = rec.saturations()

	// Phase 3: Verify CC (skipped when a certificate already attests convergence).
	if runCC {
		err = r.verifyCC(packedCount, valid, nf, step, mkState, report)
		if err != nil {
			return nil, report, err
		}
		report.NotIdempotent = r.notIdempotent(packedCount, valid, nf, step)
	}

	if err := r.checkUnchanged(before); err != nil {
		return nil, report, err
	}

	// Build immutable machine
	m := &Machine{
		name:     r.name,
		vars:     r.vars,
		events:   make(map[string]int),
		step:     step,
		nf:       nf,
		valid:    valid,
		dom:      newDomainCheck(r.vars),
		ccPairs:  r.ccPairs(),
		allPairs: r.allIndependent,
	}
	for i, ev := range r.events {
		m.events[ev.name] = i
	}

	return m, report, nil
}

// computeNormalForms verifies WFC and computes the normal form table.
func (r *Registry) computeNormalForms(run checkedRules, packedCount, stateCount int, valid []bool, mkState func(uint64) State, report *Report) ([]uint64, error) {
	nf := make([]uint64, packedCount)
	maxRepair := 0
	var err error

	for i := 0; i < packedCount; i++ {
		if !valid[i] {
			nf[i] = uint64(i)
			continue
		}

		s := mkState(uint64(i))
		depth := 0
		seen := make(map[uint64]bool)
		seen[s.packed] = true

		for !r.allInvariantsHold(s) {
			if s, err = run.applyFirstRepair(s); err != nil {
				return nil, err
			}
			depth++

			// Detect non-termination: if we've seen this state before, we have a repair cycle.
			// Also fail if depth exceeds state count (impossible in a terminating machine).
			if seen[s.packed] || depth > stateCount {
				report.WFC = false
				return nil, &compensationError{"gsm: WFC check failed — compensation does not terminate"}
			}
			seen[s.packed] = true
		}

		nf[i] = s.packed
		if depth > maxRepair {
			maxRepair = depth
		}
	}

	report.WFC = true
	report.MaxRepairLen = maxRepair

	// Idempotence on valid states (nf[s] == s when every invariant holds) needs no check:
	// the repair loop above runs only while an invariant is violated, so it never starts
	// from a valid state.
	return nf, nil
}

// computeStepTables builds the Step[e][s] = NF(apply(e, s)) tables.
func (r *Registry) computeStepTables(run checkedRules, packedCount int, valid []bool, nf []uint64, mkState func(uint64) State) ([][]uint64, error) {
	step := make([][]uint64, len(r.events))
	for ei, ev := range r.events {
		step[ei] = make([]uint64, packedCount)
		for i := 0; i < packedCount; i++ {
			if valid[i] {
				after, err := run.applyEvent(ev, mkState(uint64(i)))
				if err != nil {
					return nil, err
				}
				step[ei][i] = nf[after.packed]
			}
		}
	}
	return step, nil
}

// verifyCC checks compensation commutativity for every event pair ccPairs selects,
// exactly, over the whole state space: for each valid state s it compares
// Step[j][Step[i][s]] with Step[i][Step[j][s]]. The step tables are already
// computed, so each pair costs two table lookups per state and no closure calls.
// A declared pair that does not commute fails the build.
//
// In declared-only mode (Registry.Independent) it also checks every undeclared pair
// the same way, without failing: an undeclared pair that does not commute is
// recorded in Report.CausalOrderRequired, since convergence then depends on that
// pair being delivered in causal order. The cost is at most that of the default
// all-pairs check.
//
// There is deliberately no disjointness shortcut here. Skipping a pair because the
// two events touch different variables is sound only when each event also READS
// nothing outside its own footprint (the precondition of disjoint_events_commute in
// normalization-confluence coq/Gsm.v), and an event's guard or effect may read any
// variable. Establishing that precondition (verifyFootprints) costs more closure
// calls per event than the exact check costs table lookups per pair, so Build always
// runs the exact check and PairsDisjoint is always 0 for Build. BuildCompositional,
// which cannot enumerate the global space, uses the shortcut only after
// verifyFootprints has established the precondition for every component.
func (r *Registry) verifyCC(packedCount int, valid []bool, nf []uint64, step [][]uint64, mkState func(uint64) State, report *Report) error {
	pairsChecked := 0
	inDomain := ccDomain(packedCount, valid, nf)

	// firstWitness returns the first state in the CC domain on which i and j do not
	// commute, or -1.
	firstWitness := func(i, j int) int {
		for s := 0; s < packedCount; s++ {
			if inDomain[s] && step[j][step[i][s]] != step[i][step[j][s]] {
				return s
			}
		}
		return -1
	}
	failure := func(i, j, s int) CCFailure {
		return CCFailure{
			Event1:  r.events[i].name,
			Event2:  r.events[j].name,
			State:   mkState(uint64(s)),
			Result1: mkState(step[j][step[i][s]]),
			Result2: mkState(step[i][step[j][s]]),
		}
	}

	declared := r.ccPairs()
	for _, p := range declared {
		i, j := p[0], p[1]
		pairsChecked++
		if s := firstWitness(i, j); s >= 0 {
			f := failure(i, j, s)
			report.CC = false
			report.PairsTotal = pairsChecked
			report.PairsDisjoint = 0
			report.PairsBrute = pairsChecked
			report.CCFailure = &f
			return &compensationError{"gsm: Compensation Commutativity (CC) check failed"}
		}
	}

	report.CC = true
	report.PairsTotal = pairsChecked
	report.PairsDisjoint = 0
	report.PairsBrute = pairsChecked

	if !r.allIndependent {
		isDeclared := make(map[[2]int]bool, len(declared))
		for _, p := range declared {
			isDeclared[p] = true
		}
		for i := 0; i < len(r.events); i++ {
			for j := i + 1; j < len(r.events); j++ {
				if isDeclared[[2]int{i, j}] {
					continue
				}
				report.PairsUndeclared++
				if s := firstWitness(i, j); s >= 0 {
					report.CausalOrderRequired = append(report.CausalOrderRequired, failure(i, j, s))
				}
			}
		}
	}
	return nil
}

// ccDomain is the CC domain: every valid state (its own normal form), plus the zero
// state Machine.NewState returns, so a run started from NewState is covered even when
// the zero state violates an invariant. This is CC1 as THEORY.md §6.3 states it (over
// valid states). Every step lands on a valid state, so commutation on this domain
// makes any permutation of the checked events reach the same state from any valid
// start or from NewState. Machine.Apply normalizes any other input first, so it
// starts inside the domain too.
func ccDomain(packedCount int, valid []bool, nf []uint64) []bool {
	inDomain := make([]bool, packedCount)
	for s := 0; s < packedCount; s++ {
		inDomain[s] = valid[s] && (nf[s] == uint64(s) || s == 0)
	}
	return inDomain
}

// notIdempotent returns, in declaration order, the events e with
// Step[e][Step[e][s]] != Step[e][s] for some s in the CC domain: delivering e twice
// differs from delivering it once, so e needs exactly-once delivery.
func (r *Registry) notIdempotent(packedCount int, valid []bool, nf []uint64, step [][]uint64) []string {
	inDomain := ccDomain(packedCount, valid, nf)
	var out []string
	for ei, ev := range r.events {
		for s := 0; s < packedCount; s++ {
			if inDomain[s] && step[ei][step[ei][s]] != step[ei][s] {
				out = append(out, ev.name)
				break
			}
		}
	}
	return out
}

// clampRecorder attributes saturating writes (State.SetInt and combinator enum
// writes clamp a value into the variable's range) to the rule Build is running.
// A build registers one for its private copy of the variable list; noteClamp finds
// it by the address of that copy, which only states derived from the build's own
// states carry. A nil recorder records nothing.
type clampRecorder struct {
	key     *Var
	pending atomic.Int32 // set by noteClamp; the hot path reads only this
	mu      sync.Mutex
	hits    []string          // variables clamped since the last call ended; reused
	counts  map[[3]string]int // (kind, name, variable) -> calls that clamped it
	order   [][3]string
}

var (
	clampWatchers  atomic.Int32 // number of registered recorders; noteClamp's fast exit
	clampRecorders sync.Map     // *Var (first element of a build's variable copy) -> *clampRecorder
)

// watchClamps registers a recorder for states over vars (a build-private slice).
func watchClamps(vars []Var) *clampRecorder {
	if len(vars) == 0 {
		return nil
	}
	rec := &clampRecorder{key: &vars[0], counts: map[[3]string]int{}}
	clampRecorders.Store(rec.key, rec)
	clampWatchers.Add(1)
	return rec
}

func (c *clampRecorder) stop() {
	if c == nil {
		return
	}
	clampRecorders.Delete(c.key)
	clampWatchers.Add(-1)
}

// noteClamp is called by every saturating write. It costs one atomic load unless
// a build is running.
func noteClamp(s State, v Var) {
	if clampWatchers.Load() == 0 || len(s.vars) == 0 {
		return
	}
	if rec, ok := clampRecorders.Load(&s.vars[0]); ok {
		c := rec.(*clampRecorder)
		c.mu.Lock()
		c.hits = append(c.hits, v.name)
		c.pending.Store(1)
		c.mu.Unlock()
	}
}

// after attributes the clamps noted during one rule call (kind "event" or
// "invariant", name) to that rule, once per variable per call. It costs one atomic
// load when the call clamped nothing.
func (c *clampRecorder) after(kind, name string) {
	if c == nil || c.pending.Load() == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, v := range c.hits {
		dup := false
		for _, w := range c.hits[:i] {
			if w == v {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		k := [3]string{kind, name, v}
		if c.counts[k] == 0 {
			c.order = append(c.order, k)
		}
		c.counts[k]++
	}
	c.hits = c.hits[:0]
	c.pending.Store(0)
}

func (c *clampRecorder) saturations() []Saturation {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Saturation
	for _, k := range c.order {
		rule := fmt.Sprintf("%s %q", k[0], k[1])
		if k[0] == "invariant" {
			rule += " repair"
		}
		out = append(out, Saturation{Rule: rule, Var: k[2], States: c.counts[k]})
	}
	return out
}

// allInvariantsHold checks V_R(s).
func (r *Registry) allInvariantsHold(s State) bool {
	for _, inv := range r.invariants {
		if !inv.check(s) {
			return false
		}
	}
	return true
}

// checkedRules runs a registry's closures and checks that each result is a state of the
// machine (domainCheck). Every verification path runs closures through one, made once per
// run so the domain check is precomputed.
type checkedRules struct {
	r     *Registry
	dom   *domainCheck
	clamp *clampRecorder // Build only: attributes saturating writes to rules
}

func (r *Registry) checked() checkedRules { return checkedRules{r: r, dom: newDomainCheck(r.vars)} }

// applyFirstRepair fires the first violated invariant's repair (priority order). It
// returns an error if the repair's result is not a state of this machine.
func (c checkedRules) applyFirstRepair(s State) (State, error) {
	for _, inv := range c.r.invariants {
		if !inv.check(s) {
			return c.repair(inv, s)
		}
	}
	return s, nil
}

// repair runs inv's repair on s and checks the result is a state of this machine.
func (c checkedRules) repair(inv invariantDef, s State) (State, error) {
	out := inv.repair(s)
	c.clamp.after("invariant", inv.name)
	return out, c.dom.ruleError(c.r.name, "invariant", inv.name, "repair", s, out)
}

// applyEvent applies an event's effect (or no-op if guard fails). It returns an error if
// the effect's result is not a state of this machine.
func (c checkedRules) applyEvent(ev eventDef, s State) (State, error) {
	if ev.guard != nil && !ev.guard(s) {
		return s, nil
	}
	out := ev.effect(s)
	c.clamp.after("event", ev.name)
	return out, c.dom.ruleError(c.r.name, "event", ev.name, "effect", s, out)
}

// isValidEncoding checks that all variable values in a packed ID
// are within their domains (rejects padding-bit waste).
func (r *Registry) isValidEncoding(packed uint64) bool {
	for _, v := range r.vars {
		mask := uint64((1 << v.bits) - 1)
		raw := (packed >> v.offset) & mask
		if int(raw) >= v.domain {
			return false
		}
	}
	return true
}
