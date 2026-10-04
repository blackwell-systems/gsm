package gsm

import (
	"fmt"
	"math/big"
	"strings"

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

// rulesOracle is the rules oracle, and rulesCost its cost. Only tests replace
// them.
var (
	rulesOracle = oracle.CheckRules
	rulesCost   = oracle.RulesCost
)

// RulesOracleMaxWork caps the rules oracle's work. Build runs the rules oracle
// on a combinator machine only when its work is at most this. The work is
// oracle.RulesCost of the rules, the checked pairs (every pair when none is
// declared) and the repair depth (Report.MaxRepairLen): an upper bound on the
// steps the generated checkBuild takes, counted from the expression trees.
// Per state it counts what the oracle evaluates there: normalizing the state
// (each repair step evaluates every invariant's predicate and one repair), and
// for each checked pair both events' guards and effects, each followed by a
// normalization, twice; a variable read or write counts its index and its
// domain's binary digits, and a list cell built counts more than a node
// evaluated. Above the cap the table oracle alone certifies the machine, and
// Report.RulesOracleSkipped gives the work.
//
// Measured on the generated Go (Apple M-series, TestRulesOracleCostPerStep and
// the review of #17), the rules oracle takes at most about 4 ns per step (the
// worst, 4.04 ns, on a wide Int domain; 0.7 to 3.2 ns elsewhere). Its memory is
// mostly the box, about states x (variables + 1) list cells: from about 100
// bytes per state at 2 variables to about 220 at 20, plus a few MB. A state
// costs at least 24 x (variables + 1) steps, so the cap also bounds the states
// and variables together. So within the cap the rules oracle adds at most about
// 4 ns x 2^29, 2.2 s, and under about 170 MB: the most measured within it is
// 160 MB, at 2^20 states of 10 four-valued Ints (work 5.0 x 10^8).
//
// The bound takes the
// repair depth from gsm's verification (Report.MaxRepairLen): if the oracle's
// repair chains were longer than gsm's, it would take longer, up to its fuel
// (the number of states) of repair steps per normalization.
const RulesOracleMaxWork = 1 << 29

// rulesOracleCap is RulesOracleMaxWork. Only tests change it.
var rulesOracleCap = RulesOracleMaxWork

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
	// AssuranceOracleComponentsTested: AssuranceOracleComponents for a machine
	// with Go closure rules, built with TrustClosureFootprints. The oracle
	// certified every component's tables, but that cross-component pairs commute
	// rests on gsm's footprint check, which for closures is a perturbation test
	// (it misses a joint dependence on three or more outside variables), not an
	// exact check.
	AssuranceOracleComponentsTested
)

func (a Assurance) String() string {
	switch a {
	case AssuranceOracleTables:
		return "tables certified by the verified table oracle"
	case AssuranceOracleTablesAndRules:
		return "tables certified by the verified table oracle; rules certified by the verified rules oracle"
	case AssuranceOracleComponents:
		return "component tables certified by the verified table oracle; cross-component independence by gsm's footprint check"
	case AssuranceOracleComponentsTested:
		return "component tables certified by the verified table oracle; cross-component independence by gsm's footprint check, " +
			"which for closure rules is a perturbation test, not exact"
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

// certifyRules runs the rules oracle on r's combinator rules, after the table
// oracle certified r's machine; depth is the machine's longest repair chain
// (Report.MaxRepairLen). It returns why the rules oracle did not run (empty
// when it ran and certified), or an oracleError when it rejects the machine or
// gives no verdict.
func certifyRules(r *Registry, depth int) (skipped string, err error) {
	var mb, pb strings.Builder
	if werr := r.WriteMachineAST(&mb); werr != nil {
		return "not a combinator machine (" + werr.Error() + ")", nil
	}
	if werr := r.WriteDeclaredPairs(&pb); werr != nil {
		return "not a combinator machine (" + werr.Error() + ")", nil
	}
	work, werr := rulesCost(mb.String(), pb.String(), depth)
	if werr != nil {
		return "", &oracleError{fmt.Sprintf("gsm: the verified rules oracle could not check the machine's rules: %v; not certified", werr)}
	}
	if work.Total.Cmp(big.NewInt(int64(rulesOracleCap))) > 0 {
		return fmt.Sprintf("the rules oracle's work, %v, is above RulesOracleMaxWork (%d)", work, rulesOracleCap), nil
	}
	res, cerr := rulesOracle(mb.String(), pb.String())
	if cerr != nil {
		return "", &oracleError{fmt.Sprintf("gsm: the verified rules oracle could not check the machine's rules: %v; not certified", cerr)}
	}
	switch res.Verdict {
	case oracle.RulesCertified:
		return "", nil
	case oracle.RulesOutsideFragment:
		return "outside the rules oracle's fragment (" + res.Reason + ")", nil
	case oracle.RulesNotTerminating, oracle.RulesNotCommuting:
		return "", &oracleError{fmt.Sprintf("gsm: the verified rules oracle rejects the machine's rules (%v), which gsm's verification "+
			"accepted; not certified (a bug in gsm's verification or in the oracle)", res.Verdict)}
	}
	return "", &oracleError{"gsm: the verified rules oracle gave no verdict on the machine's rules; not certified"}
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
