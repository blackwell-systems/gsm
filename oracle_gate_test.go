package gsm

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// withTableOracle replaces the table oracle for one test.
func withTableOracle(t *testing.T, f func(oracle.Tables) (bool, error)) {
	t.Helper()
	saved := tableOracle
	tableOracle = f
	t.Cleanup(func() { tableOracle = saved })
}

func rejecting(oracle.Tables) (bool, error) { return false, nil }

func failing(oracle.Tables) (bool, error) {
	return false, errors.New("oracle: the checker stopped: boom")
}

// capCounter: n in 0..3, capped at 2 by a repair, one increment event.
func capCounter() *Registry {
	r := NewRegistry("cap")
	n := r.Int("n", 0, 3)
	r.Invariant("le2").Watches(n).
		Holds(func(s State) bool { return s.GetInt(n) <= 2 }).
		Repair(func(s State) State { return s.SetInt(n, 2) }).Add()
	r.Event("inc").Writes(n).Apply(func(s State) State {
		if b := s.GetInt(n); b < 3 {
			return s.SetInt(n, b+1)
		}
		return s
	}).Add()
	return r
}

// twoCounters: two independent counters, each with its own event.
func twoCounters() *Registry {
	r := NewRegistry("two")
	a := r.Int("a", 0, 3)
	b := r.Int("b", 0, 2)
	r.Event("inca").Writes(a).Apply(func(s State) State { return s.SetInt(a, (s.GetInt(a)+1)%4) }).Add()
	r.Event("incb").Writes(b).Apply(func(s State) State { return s.SetInt(b, (s.GetInt(b)+1)%3) }).Add()
	return r
}

// unrepaired needs synthesis: its invariant has no Repair.
func unrepaired() *Registry {
	r := NewRegistry("approval")
	stage := r.Enum("stage", "pending", "approved", "held")
	balance := r.Int("balance", 0, 2)
	r.Invariant("funded_if_approved").Watches(stage, balance).
		Holds(func(s State) bool { return s.Get(stage) != "approved" || s.GetInt(balance) >= 2 }).Add()
	r.Event("approve").Writes(stage).Apply(func(s State) State { return s.Set(stage, "approved") }).Add()
	r.Event("credit").Writes(stage, balance).Apply(func(s State) State {
		if b := s.GetInt(balance); b < 2 {
			return s.SetInt(balance, b+1)
		}
		return s
	}).Add()
	return r
}

func TestBuildIsCertifiedByTheTableOracle(t *testing.T) {
	for _, r := range []*Registry{capCounter(), twoCounters()} {
		m, rep, err := r.Build()
		if err != nil || m == nil {
			t.Fatalf("%s: Build: %v", r.name, err)
		}
		if rep.Assurance != AssuranceOracleTables {
			t.Errorf("%s: Assurance = %v, want %v", r.name, rep.Assurance, AssuranceOracleTables)
		}
		if !strings.Contains(rep.String(), "Assurance: ") {
			t.Errorf("%s: the report does not state its assurance:\n%s", r.name, rep)
		}
	}
}

// The gate checks the machine's own tables: exactly what WriteConvergenceTables
// writes for the external checker.
func TestTheGateChecksTheMachinesTables(t *testing.T) {
	var seen []oracle.Tables
	withTableOracle(t, func(tb oracle.Tables) (bool, error) {
		seen = append(seen, tb)
		return oracle.CheckTables(tb)
	})
	r := twoCounters()
	r.Independent("inca", "incb")
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("the oracle ran %d times, want once", len(seen))
	}
	text, err := m.convergenceTables()
	if err != nil {
		t.Fatal(err)
	}
	if got := tablesText(seen[0]); got != string(text) {
		t.Errorf("the oracle saw\n%s\nbut the machine's tables are\n%s", got, text)
	}
}

// tablesText renders Tables in WriteConvergenceTables' format.
func tablesText(tb oracle.Tables) string {
	var b strings.Builder
	b.WriteString("gsm-tables 2\n" + strconv.Itoa(len(tb.NF)) + " " + strconv.Itoa(len(tb.Step)) + "\nnf")
	for _, x := range tb.NF {
		b.WriteString(" " + strconv.Itoa(x))
	}
	if tb.AllPairs {
		b.WriteString("\npairs all\n")
	} else {
		b.WriteString("\npairs " + strconv.Itoa(len(tb.Pairs)))
		for _, p := range tb.Pairs {
			b.WriteString(" " + strconv.Itoa(p[0]) + " " + strconv.Itoa(p[1]))
		}
		b.WriteByte('\n')
	}
	for _, row := range tb.Step {
		for k, x := range row {
			if k > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.Itoa(x))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func assertFailedClosed(t *testing.T, what string, m *Machine, err error) {
	t.Helper()
	if err == nil || m != nil {
		t.Fatalf("%s: got machine %v, error %v; want no machine and an error", what, m != nil, err)
	}
	if !strings.Contains(err.Error(), "oracle") {
		t.Errorf("%s: error %q does not name the oracle", what, err)
	}
}

func TestBuildFailsClosedWhenTheOracleRejects(t *testing.T) {
	for name, f := range map[string]func(oracle.Tables) (bool, error){"rejects": rejecting, "errors": failing} {
		withTableOracle(t, f)
		m, rep, err := capCounter().Build()
		assertFailedClosed(t, "Build, oracle "+name, m, err)
		if rep == nil || rep.Assurance != AssuranceNone || rep.OracleDisagreement == "" {
			t.Fatalf("Build, oracle %s: report %+v; want Assurance none and the disagreement", name, rep)
		}
		if !strings.Contains(rep.String(), "oracle") {
			t.Errorf("Build, oracle %s: the report does not explain the rejection:\n%s", name, rep)
		}
	}
}

func TestBuildOrSynthesizeFailsClosedWhenTheOracleRejects(t *testing.T) {
	withTableOracle(t, rejecting)
	// As written: the rejection is not a compensation failure, so there is no
	// synthesis fallback.
	m, syn, err := capCounter().BuildOrSynthesize()
	assertFailedClosed(t, "BuildOrSynthesize as written", m, err)
	if syn != nil {
		t.Errorf("BuildOrSynthesize fell back to synthesis after an oracle rejection")
	}
	// Synthesized: the synthesized tables are checked too.
	m, _, err = unrepaired().BuildOrSynthesize()
	assertFailedClosed(t, "BuildOrSynthesize synthesized", m, err)
}

func TestSynthesisIsCertifiedByTheTableOracle(t *testing.T) {
	syn, err := unrepaired().SynthesizeWith()
	if err != nil || !syn.Convergent {
		t.Fatalf("SynthesizeWith: %v, %v", syn, err)
	}
	if syn.Assurance != AssuranceOracleTables || syn.Machine() == nil {
		t.Fatalf("Assurance = %v, machine %v; want %v and a machine", syn.Assurance, syn.Machine() != nil, AssuranceOracleTables)
	}

	withTableOracle(t, rejecting)
	syn, err = unrepaired().SynthesizeWith()
	if err == nil || !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("SynthesizeWith with a rejecting oracle: %v, %v; want an oracle error", syn, err)
	}
	if syn != nil && syn.Machine() != nil {
		t.Fatal("a synthesis the oracle rejected still yields a machine")
	}
}

func TestSynthesisMachineNeedsTheOracle(t *testing.T) {
	syn, err := unrepaired().SynthesizeWith()
	if err != nil {
		t.Fatal(err)
	}
	syn.Assurance = AssuranceNone // as if the gate had not run
	if syn.Machine() != nil {
		t.Fatal("Synthesis.Machine returned a machine the oracle did not certify")
	}
}

func TestBuildCompositionalIsCertifiedPerComponent(t *testing.T) {
	var calls int
	withTableOracle(t, func(tb oracle.Tables) (bool, error) {
		calls++
		return oracle.CheckTables(tb)
	})
	r := twoCounters()
	m, rep, err := r.BuildCompositional()
	if err != nil || m == nil {
		t.Fatalf("BuildCompositional: %v", err)
	}
	if rep.Assurance != AssuranceOracleComponents {
		t.Errorf("Assurance = %v, want %v", rep.Assurance, AssuranceOracleComponents)
	}
	if calls != rep.Components {
		t.Errorf("the oracle ran %d times for %d components", calls, rep.Components)
	}

	withTableOracle(t, rejecting)
	m, rep, err = twoCounters().BuildCompositional()
	assertFailedClosed(t, "BuildCompositional", m, err)
	if rep == nil || rep.Assurance != AssuranceNone || rep.OracleDisagreement == "" {
		t.Fatalf("BuildCompositional: report %+v; want Assurance none and the disagreement", rep)
	}
}

// A component's tables, as the gate builds them, are the component's subspace:
// the zero state first, every state of the component, its events, and the
// pairs local to it.
func TestComponentTables(t *testing.T) {
	var seen []oracle.Tables
	withTableOracle(t, func(tb oracle.Tables) (bool, error) {
		seen = append(seen, tb)
		return oracle.CheckTables(tb)
	})
	r := NewRegistry("comp")
	a := r.Int("a", 0, 3)
	b := r.Int("b", 0, 1)
	r.Invariant("acap").Watches(a).
		Holds(func(s State) bool { return s.GetInt(a) <= 2 }).
		Repair(func(s State) State { return s.SetInt(a, 2) }).Add()
	r.Event("inca").Writes(a).Apply(func(s State) State { return s.SetInt(a, min(s.GetInt(a)+1, 3)) }).Add()
	r.Event("seta").Writes(a).Apply(func(s State) State { return s.SetInt(a, 2) }).Add()
	r.Event("flipb").Writes(b).Apply(func(s State) State { return s.SetInt(b, 1-s.GetInt(b)) }).Add()
	if _, _, err := r.BuildCompositional(); err != nil {
		t.Fatal(err)
	}
	want := []oracle.Tables{
		// a: states 0..3; 3 is invalid (repaired to 2); inca and seta, pair (0, 1).
		{NF: []int{0, 1, 2, 2}, Step: [][]int{{1, 2, 2, 2}, {2, 2, 2, 2}}, Pairs: [][2]int{{0, 1}}},
		// b: states 0..1; flipb; no local pair.
		{NF: []int{0, 1}, Step: [][]int{{1, 0}}, Pairs: [][2]int{}},
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("component tables\n got %+v\nwant %+v", seen, want)
	}
}
