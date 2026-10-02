package gsm

import (
	"fmt"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// The oracle gate.
//
// gsm's own verification (WFC and CC in Go) is followed by an independent check.
// The checker is check_fast from the normalization-confluence proof, translated
// from Rocq to Go mechanically (internal/oracle). It reads only the tables the
// verification produced. If it does not certify them, there is no machine:
// Build, BuildOrSynthesize, SynthesizeWith (and so Synthesis.Machine) and
// BuildCompositional return an error and no machine. Report.Assurance says what
// certified a machine.

// tableOracle is the table oracle. Only tests replace it.
var tableOracle = oracle.CheckTables

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
	// check_fast_converges then gives convergence for the machine as built.
	AssuranceOracleTables
	// AssuranceOracleComponents: BuildCompositional's verification passed, and
	// the table oracle certified every footprint component's tables over that
	// component's subspace. The step from per-component to global convergence
	// (cross-component pairs commute because footprints are disjoint) rests on
	// gsm's footprint check, which the oracle does not see.
	AssuranceOracleComponents
)

func (a Assurance) String() string {
	switch a {
	case AssuranceOracleTables:
		return "tables certified by the verified table oracle"
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
func certifyTables(tb oracle.Tables, what string) error {
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

// oracleTables returns the machine's tables for the table oracle: the
// in-domain encodings, renumbered 0..V-1 in encoding order (so the zero state is
// 0), with the declared pairs. They are the tables WriteConvergenceTables writes.
func (m *Machine) oracleTables() (oracle.Tables, error) {
	newID := make(map[uint64]int)
	var order []uint64
	for s := 0; s < len(m.nf); s++ {
		if m.inDomain(uint64(s)) {
			newID[uint64(s)] = len(order)
			order = append(order, uint64(s))
		}
	}
	id := func(packed uint64) (int, error) {
		i, ok := newID[packed]
		if !ok {
			return 0, fmt.Errorf("gsm: table entry %d is not an in-domain encoding", packed)
		}
		return i, nil
	}
	tb := oracle.Tables{NF: make([]int, len(order)), Step: make([][]int, len(m.step)), AllPairs: m.allPairs}
	for k, old := range order {
		i, err := id(m.nf[old])
		if err != nil {
			return tb, err
		}
		tb.NF[k] = i
	}
	if !m.allPairs {
		tb.Pairs = append([][2]int{}, m.ccPairs...)
	}
	for e := range m.step {
		tb.Step[e] = make([]int, len(order))
		for k, old := range order {
			i, err := id(m.step[e][old])
			if err != nil {
				return tb, err
			}
			tb.Step[e][k] = i
		}
	}
	return tb, nil
}

// certifyMachine runs the gate on a machine Build or SynthesizeWith produced.
func certifyMachine(m *Machine) error {
	tb, err := m.oracleTables()
	if err != nil {
		return &oracleError{fmt.Sprintf("gsm: cannot give the machine's tables to the verified table oracle: %v; not certified", err)}
	}
	return certifyTables(tb, "the machine's tables")
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
