package gsm

import (
	"errors"
	"fmt"
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

	if r.WFC && r.CC {
		s += "\n  Convergence: GUARANTEED\n"
	}

	return s
}

// Build verifies WFC and CC, then returns an immutable Machine.
//
// Build runs every event effect on every valid encoding and every repair while computing
// normal forms, and returns an error if any of them returns something that is not a state
// of this machine (see EffectFunc): a different variable schema, a variable outside its
// range, or bits outside the encoding. The error names the event or invariant, the input
// state and the result. Membership is by value, so a state built from another machine
// with an identical variable declaration list is accepted.
func (r *Registry) Build() (*Machine, *Report, error) {
	m, rep, err := r.build(true)
	if buildObserver != nil {
		buildObserver(r, m, rep, err)
	}
	return m, rep, err
}

// buildObserver, when non-nil, sees every Build result. Only this package's tests
// set it (once, before any test runs), to cross-check every machine the test suite
// builds against the extracted checkers (oracle_differential_test.go).
var buildObserver func(r *Registry, m *Machine, rep *Report, err error)

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

	mkState := func(id uint64) State {
		return State{packed: id, vars: r.vars}
	}

	// Phase 1: Verify WFC and compute normal forms
	nf, err := r.computeNormalForms(packedCount, stateCount, valid, mkState, report)
	if err != nil {
		return nil, report, err
	}

	// Phase 2: Compute step tables
	step, err := r.computeStepTables(packedCount, valid, nf, mkState)
	if err != nil {
		return nil, report, err
	}

	// Phase 3: Verify CC (skipped when a certificate already attests convergence).
	if runCC {
		err = r.verifyCC(packedCount, valid, nf, step, mkState, report)
		if err != nil {
			return nil, report, err
		}
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
func (r *Registry) computeNormalForms(packedCount, stateCount int, valid []bool, mkState func(uint64) State, report *Report) ([]uint64, error) {
	nf := make([]uint64, packedCount)
	maxRepair := 0
	run := r.checked()
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

	// Verify idempotence on valid states
	for i := 0; i < packedCount; i++ {
		if valid[i] {
			s := mkState(uint64(i))
			if r.allInvariantsHold(s) && nf[i] != uint64(i) {
				return nil, &compensationError{fmt.Sprintf("gsm: compensation moves valid state %s — repair must be identity on valid states", s)}
			}
		}
	}

	return nf, nil
}

// computeStepTables builds the Step[e][s] = NF(apply(e, s)) tables.
func (r *Registry) computeStepTables(packedCount int, valid []bool, nf []uint64, mkState func(uint64) State) ([][]uint64, error) {
	step := make([][]uint64, len(r.events))
	run := r.checked()
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

	// The CC domain: every valid state (its own normal form), plus the zero state
	// Machine.NewState returns, so a run started from NewState is covered even when
	// the zero state violates an invariant. This is CC1 as THEORY.md §6.3 states it
	// (over valid states). Every step lands on a valid state, so commutation on this
	// domain makes any permutation of the checked events reach the same state from
	// any valid start or from NewState. A state that is neither (an invalid state a
	// caller builds by hand) is outside the guarantee.
	inDomain := make([]bool, packedCount)
	for s := 0; s < packedCount; s++ {
		inDomain[s] = valid[s] && (nf[s] == uint64(s) || s == 0)
	}

	for _, p := range r.ccPairs() {
		i, j := p[0], p[1]
		pairsChecked++
		for s := 0; s < packedCount; s++ {
			if !inDomain[s] {
				continue
			}

			after_ij := step[j][step[i][s]]
			after_ji := step[i][step[j][s]]

			if after_ij != after_ji {
				report.CC = false
				report.PairsTotal = pairsChecked
				report.PairsDisjoint = 0
				report.PairsBrute = pairsChecked
				report.CCFailure = &CCFailure{
					Event1:  r.events[i].name,
					Event2:  r.events[j].name,
					State:   mkState(uint64(s)),
					Result1: mkState(after_ij),
					Result2: mkState(after_ji),
				}
				return &compensationError{"gsm: Compensation Commutativity (CC) check failed"}
			}
		}
	}

	report.CC = true
	report.PairsTotal = pairsChecked
	report.PairsDisjoint = 0
	report.PairsBrute = pairsChecked
	return nil
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
	r   *Registry
	dom *domainCheck
}

func (r *Registry) checked() checkedRules { return checkedRules{r, newDomainCheck(r.vars)} }

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
	return out, c.dom.ruleError(c.r.name, "invariant", inv.name, "repair", s, out)
}

// applyEvent applies an event's effect (or no-op if guard fails). It returns an error if
// the effect's result is not a state of this machine.
func (c checkedRules) applyEvent(ev eventDef, s State) (State, error) {
	if ev.guard != nil && !ev.guard(s) {
		return s, nil
	}
	out := ev.effect(s)
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
