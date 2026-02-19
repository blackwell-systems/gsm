package gsm

import "fmt"

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
	PairsDisjoint int        // proved by footprint disjointness
	PairsBrute    int        // proved by exhaustive check
	CCFailure     *CCFailure // non-nil if CC failed
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
	s += fmt.Sprintf("  States: %d\n", r.StateCount)
	s += fmt.Sprintf("  Events: %d\n", r.EventCount)
	s += "\n"

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
func (b *Builder) Build() (*Machine, *Report, error) {
	if b.totalBits > 20 {
		return nil, nil, fmt.Errorf("gsm: state space too large (%d bits, max 20)", b.totalBits)
	}

	stateCount := 1
	for _, v := range b.vars {
		stateCount *= v.domain
	}
	if stateCount > maxStateSpace {
		return nil, nil, fmt.Errorf("gsm: state space %d exceeds limit %d", stateCount, maxStateSpace)
	}

	packedCount := 1 << b.totalBits

	report := &Report{
		Name:       b.name,
		StateCount: stateCount,
		VarCount:   len(b.vars),
		EventCount: len(b.events),
	}

	// Build validity mask
	valid := make([]bool, packedCount)
	for i := 0; i < packedCount; i++ {
		valid[i] = b.isValidEncoding(uint64(i))
	}

	mkState := func(id uint64) State {
		return State{packed: id, vars: b.vars}
	}

	// Phase 1: Verify WFC and compute normal forms
	nf, err := b.computeNormalForms(packedCount, stateCount, valid, mkState, report)
	if err != nil {
		return nil, report, err
	}

	// Phase 2: Compute step tables
	step := b.computeStepTables(packedCount, valid, nf, mkState)

	// Phase 3: Verify CC
	err = b.verifyCC(packedCount, valid, step, mkState, report)
	if err != nil {
		return nil, report, err
	}

	// Build immutable machine
	m := &Machine{
		name:   b.name,
		vars:   b.vars,
		events: make(map[string]int),
		step:   step,
		nf:     nf,
	}
	for i, ev := range b.events {
		m.events[ev.name] = i
	}

	return m, report, nil
}

// computeNormalForms verifies WFC and computes the normal form table.
func (b *Builder) computeNormalForms(packedCount, stateCount int, valid []bool, mkState func(uint64) State, report *Report) ([]uint64, error) {
	nf := make([]uint64, packedCount)
	maxRepair := 0

	for i := 0; i < packedCount; i++ {
		if !valid[i] {
			nf[i] = uint64(i)
			continue
		}

		s := mkState(uint64(i))
		depth := 0
		seen := make(map[uint64]bool)
		seen[s.packed] = true

		for !b.allInvariantsHold(s) {
			s = b.applyFirstRepair(s)
			depth++

			if seen[s.packed] || depth > stateCount {
				report.WFC = false
				return nil, fmt.Errorf("gsm: WFC check failed — compensation does not terminate")
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
			if b.allInvariantsHold(s) && nf[i] != uint64(i) {
				return nil, fmt.Errorf("gsm: compensation moves valid state %s — repair must be identity on valid states", s)
			}
		}
	}

	return nf, nil
}

// computeStepTables builds the Step[e][s] = NF(apply(e, s)) tables.
func (b *Builder) computeStepTables(packedCount int, valid []bool, nf []uint64, mkState func(uint64) State) [][]uint64 {
	step := make([][]uint64, len(b.events))
	for ei, ev := range b.events {
		step[ei] = make([]uint64, packedCount)
		for i := 0; i < packedCount; i++ {
			if valid[i] {
				s := mkState(uint64(i))
				after := b.applyEvent(ev, s)
				after = b.clampState(after)
				step[ei][i] = nf[after.packed]
			}
		}
	}
	return step
}

// verifyCC checks compensation commutativity for independent event pairs.
func (b *Builder) verifyCC(packedCount int, valid []bool, step [][]uint64, mkState func(uint64) State, report *Report) error {
	pairsDisjoint := 0
	pairsBrute := 0

	type pair struct{ i, j int }
	var pairsToCheck []pair

	if b.allIndependent {
		for i := 0; i < len(b.events); i++ {
			for j := i + 1; j < len(b.events); j++ {
				pairsToCheck = append(pairsToCheck, pair{i, j})
			}
		}
	} else {
		for _, p := range b.independent {
			i, j := p[0], p[1]
			if i > j {
				i, j = j, i
			}
			pairsToCheck = append(pairsToCheck, pair{i, j})
		}
	}

	for _, p := range pairsToCheck {
		i, j := p.i, p.j

		if b.eventsDisjoint(i, j) {
			pairsDisjoint++
			continue
		}

		pairsBrute++
		for s := 0; s < packedCount; s++ {
			if !valid[s] {
				continue
			}

			after_ij := step[j][step[i][s]]
			after_ji := step[i][step[j][s]]

			if after_ij != after_ji {
				report.CC = false
				report.PairsTotal = pairsDisjoint + pairsBrute
				report.PairsDisjoint = pairsDisjoint
				report.PairsBrute = pairsBrute
				report.CCFailure = &CCFailure{
					Event1:  b.events[i].name,
					Event2:  b.events[j].name,
					State:   mkState(uint64(s)),
					Result1: mkState(after_ij),
					Result2: mkState(after_ji),
				}
				return fmt.Errorf("gsm: Compensation Commutativity (CC) check failed")
			}
		}
	}

	report.CC = true
	report.PairsTotal = pairsDisjoint + pairsBrute
	report.PairsDisjoint = pairsDisjoint
	report.PairsBrute = pairsBrute
	return nil
}

// allInvariantsHold checks V_R(s).
func (b *Builder) allInvariantsHold(s State) bool {
	for _, inv := range b.invariants {
		if !inv.check(s) {
			return false
		}
	}
	return true
}

// applyFirstRepair fires the first violated invariant's repair (priority order).
func (b *Builder) applyFirstRepair(s State) State {
	for _, inv := range b.invariants {
		if !inv.check(s) {
			return inv.repair(s)
		}
	}
	return s
}

// applyEvent applies an event's effect (or no-op if guard fails).
func (b *Builder) applyEvent(ev eventDef, s State) State {
	if ev.guard != nil && !ev.guard(s) {
		return s
	}
	return ev.effect(s)
}

// clampState ensures all variable values are within their domains.
// This handles cases where arithmetic produces out-of-range values
// before the bitpacking truncates them.
func (b *Builder) clampState(s State) State {
	for _, v := range b.vars {
		raw := s.getRaw(v)
		max := uint64(v.domain - 1)
		if raw > max {
			s = s.setRaw(v, max)
		}
	}
	return s
}

// isValidEncoding checks that all variable values in a packed ID
// are within their domains (rejects padding-bit waste).
func (b *Builder) isValidEncoding(packed uint64) bool {
	for _, v := range b.vars {
		mask := uint64((1 << v.bits) - 1)
		raw := (packed >> v.offset) & mask
		if int(raw) >= v.domain {
			return false
		}
	}
	return true
}

// eventsDisjoint returns true if two events have disjoint write sets
// AND the invariants they can trigger have disjoint footprints.
func (b *Builder) eventsDisjoint(ei, ej int) bool {
	// Get invariant footprint vars for each event
	fp1 := b.eventFootprint(ei)
	fp2 := b.eventFootprint(ej)

	for v := range fp1 {
		if fp2[v] {
			return false
		}
	}
	return true
}

// eventFootprint returns the union of footprints of all invariants
// whose footprint overlaps with the event's write set.
func (b *Builder) eventFootprint(ei int) map[int]bool {
	writes := make(map[int]bool)
	for _, vi := range b.events[ei].writes {
		writes[vi] = true
	}

	fp := make(map[int]bool)
	for _, inv := range b.invariants {
		overlaps := false
		for _, vi := range inv.footprint {
			if writes[vi] {
				overlaps = true
				break
			}
		}
		if overlaps {
			for _, vi := range inv.footprint {
				fp[vi] = true
			}
		}
	}
	return fp
}
