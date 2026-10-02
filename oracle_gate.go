package gsm

import (
	"fmt"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// The oracle gate.
//
// gsm's own verification (WFC and CC in Go) is followed by an independent check.
// The checker is check_fn from the normalization-confluence proof, translated
// from Rocq to Go mechanically (internal/oracle). It reads only the tables the
// verification produced, in place, through accessors (oracleLookup). If it does not certify them, there is no machine:
// Build, BuildOrSynthesize, SynthesizeWith (and so Synthesis.Machine) and
// BuildCompositional return an error and no machine. Report.Assurance says what
// certified a machine.

// tableOracle is the table oracle. Only tests replace it.
var tableOracle = oracle.CheckLookup

// rulesOracle is the rules oracle. Only tests replace it.
var rulesOracle = oracle.CheckRules

// RulesOracleMaxStatePairs caps the rules oracle's work. Build runs the rules
// oracle on a combinator machine only when its number of states times its
// number of checked event pairs (every pair when none is declared; at least 1)
// is at most this; above it the table oracle alone certifies the machine. The
// rules oracle re-derives every step from the expression trees, at about 3 µs
// per state and pair in the generated Go (Apple M-series): at the cap, about
// 2^20 states with 2 pairs, it takes about 7 s.
const RulesOracleMaxStatePairs = 1 << 21

// rulesOracleCap is RulesOracleMaxStatePairs. Only tests change it.
var rulesOracleCap = RulesOracleMaxStatePairs

// Assurance says what certified a machine's convergence.
type Assurance int

const (
	// AssuranceNone: not certified. Either the build failed, or the table oracle
	// rejected the tables gsm's verification accepted (see
	// Report.OracleDisagreement).
	AssuranceNone Assurance = iota
	// AssuranceOracleTables: gsm's verification passed, and the table oracle
	// generated from the Rocq proof independently certified the machine's
	// tables:
	//   - normal forms and steps land on valid states;
	//   - the declared pairs commute on the valid states and the zero state.
	// check_fn_converges then gives convergence for the machine as built.
	AssuranceOracleTables
	// AssuranceOracleComponents: BuildCompositional's verification passed, and
	// the table oracle certified every footprint component's tables over that
	// component's subspace. The step from per-component to global convergence
	// (cross-component pairs commute because footprints are disjoint) rests on
	// gsm's footprint check, which the oracle does not see.
	AssuranceOracleComponents
	// AssuranceOracleTablesAndRules: AssuranceOracleTables, and the rules
	// oracle generated from the Rocq proof independently certified the
	// machine from its combinator rules (checkBuild_converges): it re-derives
	// every step from the expression trees, so it does not trust gsm's tables
	// at all.
	AssuranceOracleTablesAndRules
)

func (a Assurance) String() string {
	switch a {
	case AssuranceOracleTables:
		return "tables certified by the verified table oracle"
	case AssuranceOracleTablesAndRules:
		return "tables certified by the verified table oracle; rules certified by the verified rules oracle"
	case AssuranceOracleComponents:
		return "component tables certified by the verified table oracle; cross-component independence by gsm's footprint check"
	}
	return "not certified"
}

// oracleError is the error when the table oracle does not certify a machine
// gsm's own verification accepted.
type oracleError struct{ msg string }

func (e *oracleError) Error() string { return e.msg }

// certifyTables runs the table oracle on tb. what names the tables in the error.
func certifyTables(tb oracle.Lookup, what string) error {
	ok, err := tableOracle(tb)
	if err != nil {
		return &oracleError{fmt.Sprintf("gsm: the verified table oracle could not check %s: %v; not certified", what, err)}
	}
	if !ok {
		return &oracleError{fmt.Sprintf("gsm: the verified table oracle rejects %s, which gsm's verification accepted; "+
			"not certified (with deterministic rules, a bug in gsm's verification or in the oracle; an impure rule re-run with "+
			"a different result can also cause it)", what)}
	}
	return nil
}

// oracleLookup gives the table oracle the machine's tables in place: the
// in-domain encodings, renumbered 0..V-1 in encoding order (so the zero state is
// 0), with the declared pairs. They are the tables WriteConvergenceTables writes
// (oracleTables), read through m.nf and m.step rather than copied. Every entry
// of an in-domain state is checked to be an in-domain encoding first, so the
// accessors only index. m.nf and m.step do not change after Build, so the
// accessors are pure, as oracle.Lookup requires.
func (m *Machine) oracleLookup() (oracle.Lookup, error) {
	rid := make([]int32, len(m.nf))
	order := make([]uint64, 0, len(m.nf))
	for s := range rid {
		rid[s] = -1
		if m.inDomain(uint64(s)) {
			rid[s] = int32(len(order))
			order = append(order, uint64(s))
		}
	}
	in := func(packed uint64) error {
		if packed >= uint64(len(rid)) || rid[packed] < 0 {
			return fmt.Errorf("gsm: table entry %d is not an in-domain encoding", packed)
		}
		return nil
	}
	for _, old := range order {
		if err := in(m.nf[old]); err != nil {
			return oracle.Lookup{}, err
		}
		for e := range m.step {
			if err := in(m.step[e][old]); err != nil {
				return oracle.Lookup{}, err
			}
		}
	}
	l := oracle.Lookup{
		N: len(order), NE: len(m.step),
		NF:       func(k int) int { return int(rid[m.nf[order[k]]]) },
		Step:     func(e, k int) int { return int(rid[m.step[e][order[k]]]) },
		AllPairs: m.allPairs,
	}
	if len(order) == len(m.nf) {
		// Every encoding is in the domain, so ids are encodings: skip rid.
		l.NF = func(k int) int { return int(m.nf[k]) }
		l.Step = func(e, k int) int { return int(m.step[e][k]) }
	}
	if !m.allPairs {
		l.Pairs = append([][2]int{}, m.ccPairs...)
	}
	return l, nil
}

// oracleTables returns oracleLookup's tables as slices, for
// WriteConvergenceTables.
func (m *Machine) oracleTables() (oracle.Tables, error) {
	l, err := m.oracleLookup()
	if err != nil {
		return oracle.Tables{}, err
	}
	tb := oracle.Tables{NF: make([]int, l.N), Step: make([][]int, l.NE), Pairs: l.Pairs, AllPairs: l.AllPairs}
	for k := range tb.NF {
		tb.NF[k] = l.NF(k)
	}
	for e := range tb.Step {
		tb.Step[e] = make([]int, l.N)
		for k := range tb.Step[e] {
			tb.Step[e][k] = l.Step(e, k)
		}
	}
	return tb, nil
}

// certifyMachine runs the gate on a machine Build or SynthesizeWith produced.
func certifyMachine(m *Machine) error {
	l, err := m.oracleLookup()
	if err != nil {
		return &oracleError{fmt.Sprintf("gsm: cannot give the machine's tables to the verified table oracle: %v; not certified", err)}
	}
	return certifyTables(l, "the machine's tables")
}

// componentTables returns one footprint component's tables for the table
// oracle. The states are the component's subspace with every other variable at
// zero, in enumComponent order, so the zero state is 0. The events are the
// component's, and the pairs are those of localPairs (global event indices)
// whose events are in it, renumbered to the component's events. A state's
// normal form and an event's step are computed as BuildCompositional's checks
// compute them: apply, then repair until every invariant holds. A result
// outside the subspace is an error, since footprints were verified to prevent it.
func (r *Registry) componentTables(c *component, localPairs [][2]int) (oracle.Tables, error) {
	var states []State
	r.enumComponent(c, func(s State) { states = append(states, s) })
	id := make(map[uint64]int, len(states))
	for i, s := range states {
		id[s.packed] = i
	}
	run := r.checked()
	normal := func(s State) (int, error) {
		for steps := 0; !r.allInvariantsHold(s); steps++ {
			if steps > len(states) {
				return 0, fmt.Errorf("gsm: repair does not terminate in component %v", c.vars)
			}
			var err error
			if s, err = run.applyFirstRepair(s); err != nil {
				return 0, err
			}
		}
		i, ok := id[s.packed]
		if !ok {
			return 0, fmt.Errorf("gsm: a result leaves component %v", c.vars)
		}
		return i, nil
	}
	tb := oracle.Tables{NF: make([]int, len(states)), Step: make([][]int, len(c.events)), Pairs: [][2]int{}}
	for i, s := range states {
		var err error
		if tb.NF[i], err = normal(s); err != nil {
			return tb, err
		}
	}
	local := make(map[int]int, len(c.events))
	for k, ei := range c.events {
		local[ei] = k
		tb.Step[k] = make([]int, len(states))
		for i, s := range states {
			after, err := run.applyEvent(r.events[ei], s)
			if err != nil {
				return tb, err
			}
			if tb.Step[k][i], err = normal(after); err != nil {
				return tb, err
			}
		}
	}
	for _, p := range localPairs {
		a, inA := local[p[0]]
		b, inB := local[p[1]]
		if inA && inB {
			tb.Pairs = append(tb.Pairs, [2]int{a, b})
		}
	}
	return tb, nil
}

// failClosed records an oracle rejection in the report.
func (rep *Report) failClosed(err error) {
	if rep == nil {
		return
	}
	rep.Assurance = AssuranceNone
	rep.OracleDisagreement = err.Error()
}
