package gsm

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

func withRulesOracle(t *testing.T, f func(machine, pairs string) (oracle.RulesResult, error)) {
	t.Helper()
	saved := rulesOracle
	rulesOracle = f
	t.Cleanup(func() { rulesOracle = saved })
}

func withRulesCap(t *testing.T, cap int) {
	t.Helper()
	saved := rulesOracleCap
	rulesOracleCap = cap
	t.Cleanup(func() { rulesOracleCap = saved })
}

// combCapped: a combinator machine with a repair: a counter a in 0..3 capped
// at 2 by its invariant, a flag b, and an event for each.
func combCapped() *Registry {
	r := NewRegistry("comb_capped")
	a := r.Int("a", 0, 3)
	b := r.Bool("b")
	r.DeclInvariant("acap", AtMost(a, 2), SetTo(a, 2))
	r.DeclEvent("inca", Do(Set(a, Add(V(a), Lit(1)))))
	r.DeclEvent("raiseb", Raise(b))
	return r
}

// A combinator machine within the cap is certified by both oracles.
func TestBuildRunsTheRulesOracle(t *testing.T) {
	calls := 0
	withRulesOracle(t, func(m, p string) (oracle.RulesResult, error) { calls++; return oracle.CheckRules(m, p) })
	m, rep, err := combCapped().Build()
	if err != nil || m == nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if calls != 1 {
		t.Fatalf("the rules oracle ran %d times, want once", calls)
	}
	if rep.Assurance != AssuranceOracleTablesAndRules || rep.RulesOracleSkipped != "" {
		t.Fatalf("Assurance = %v, RulesOracleSkipped = %q; want both oracles and no skip", rep.Assurance, rep.RulesOracleSkipped)
	}
	if !strings.Contains(rep.String(), "rules oracle") {
		t.Errorf("the report does not say the rules oracle ran:\n%s", rep)
	}
}

// The rules oracle sees exactly what WriteMachineAST and WriteDeclaredPairs
// write, so the in-process verdict is the external astchecker's.
func TestTheRulesOracleSeesTheExportedRules(t *testing.T) {
	r := combCapped()
	r.Independent("inca", "raiseb")
	var seen [2]string
	withRulesOracle(t, func(m, p string) (oracle.RulesResult, error) { seen = [2]string{m, p}; return oracle.CheckRules(m, p) })
	if _, rep, err := r.Build(); err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	var mb, pb strings.Builder
	if err := r.WriteMachineAST(&mb); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteDeclaredPairs(&pb); err != nil {
		t.Fatal(err)
	}
	if seen != [2]string{mb.String(), pb.String()} {
		t.Errorf("the rules oracle saw\n%q\nwant\n%q", seen, [2]string{mb.String(), pb.String()})
	}
}

// A machine with closure rules has no rules to give the rules oracle: table
// oracle only, and the report says why.
func TestBuildSkipsTheRulesOracleWithoutCombinatorRules(t *testing.T) {
	withRulesOracle(t, func(string, string) (oracle.RulesResult, error) {
		t.Fatal("the rules oracle ran on a machine without combinator rules")
		return oracle.RulesResult{}, nil
	})
	m, rep, err := wideFlags(3).Build()
	if err != nil || m == nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Assurance != AssuranceOracleTables || !strings.Contains(rep.RulesOracleSkipped, "combinator") {
		t.Fatalf("Assurance = %v, RulesOracleSkipped = %q; want tables only, skipped for having no combinator rules", rep.Assurance, rep.RulesOracleSkipped)
	}
}

// Above the cap on states x pairs (pairs: every pair when none is declared),
// the rules oracle does not run; at the cap it does.
func TestBuildSkipsTheRulesOracleAboveItsCap(t *testing.T) {
	// combCapped: 4 x 2 = 8 states, 1 pair (every pair of 2 events).
	for cap, wantRun := range map[int]bool{7: false, 8: true} {
		withRulesCap(t, cap)
		ran := false
		withRulesOracle(t, func(m, p string) (oracle.RulesResult, error) { ran = true; return oracle.CheckRules(m, p) })
		m, rep, err := combCapped().Build()
		if err != nil || m == nil {
			t.Fatalf("cap %d: Build: %v", cap, err)
		}
		if ran != wantRun {
			t.Errorf("cap %d: the rules oracle ran = %v, want %v", cap, ran, wantRun)
		}
		want := AssuranceOracleTables
		if wantRun {
			want = AssuranceOracleTablesAndRules
		}
		if rep.Assurance != want {
			t.Errorf("cap %d: Assurance = %v, want %v", cap, rep.Assurance, want)
		}
		if !wantRun && !strings.Contains(rep.RulesOracleSkipped, fmt.Sprint(cap)) {
			t.Errorf("cap %d: RulesOracleSkipped = %q does not name the cap", cap, rep.RulesOracleSkipped)
		}
	}
	// Pairs count: three events give three pairs (8 x 3 = 24), and declaring
	// one pair independent leaves one (8 x 1).
	three := func(declare bool) *Registry {
		r := combCapped()
		r.DeclEvent("raiseb2", Raise(r.vars[1]))
		if declare {
			r.Independent("inca", "raiseb")
		}
		return r
	}
	for _, c := range []struct {
		declare bool
		cap     int
		want    Assurance
	}{
		{false, 23, AssuranceOracleTables},
		{false, 24, AssuranceOracleTablesAndRules},
		{true, 7, AssuranceOracleTables},
		{true, 8, AssuranceOracleTablesAndRules},
	} {
		withRulesCap(t, c.cap)
		m, rep, err := three(c.declare).Build()
		if err != nil || m == nil {
			t.Fatalf("three events, declare %v, cap %d: Build: %v\n%s", c.declare, c.cap, err, rep)
		}
		if rep.Assurance != c.want {
			t.Errorf("three events, declare %v, cap %d: Assurance = %v, want %v", c.declare, c.cap, rep.Assurance, c.want)
		}
	}
	// With no events there are no pairs: the cost is the states.
	withRulesCap(t, 3)
	r := NewRegistry("no_events")
	r.Int("a", 0, 3)
	if _, rep, err := r.Build(); err != nil || rep.Assurance != AssuranceOracleTables {
		t.Errorf("4 states, no pairs, cap 3: Assurance = %v, err %v; want tables only", rep.Assurance, err)
	}
}

// Outside the rules oracle's fragment there is no verdict about the machine:
// table oracle only, not a disagreement.
func TestBuildSkipsTheRulesOracleOutsideItsFragment(t *testing.T) {
	r := NewRegistry("overflow")
	a := r.Int("a", 0, 3)
	r.DeclEvent("big", Do(Set(a, Add(Lit(2147483647), V(a)))))
	m, rep, err := r.Build()
	if err != nil || m == nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Assurance != AssuranceOracleTables || !strings.Contains(rep.RulesOracleSkipped, "fragment") {
		t.Fatalf("Assurance = %v, RulesOracleSkipped = %q; want tables only, skipped as outside the fragment", rep.Assurance, rep.RulesOracleSkipped)
	}
}

// A rules-oracle rejection of a machine gsm's verification accepted is a
// disagreement: Build fails closed.
func TestBuildFailsClosedWhenTheRulesOracleRejects(t *testing.T) {
	for name, f := range map[string]func(string, string) (oracle.RulesResult, error){
		"not commuting": func(string, string) (oracle.RulesResult, error) {
			return oracle.RulesResult{Verdict: oracle.RulesNotCommuting}, nil
		},
		"not terminating": func(string, string) (oracle.RulesResult, error) {
			return oracle.RulesResult{Verdict: oracle.RulesNotTerminating}, nil
		},
		"no verdict": func(string, string) (oracle.RulesResult, error) { return oracle.RulesResult{}, nil },
		"errors": func(string, string) (oracle.RulesResult, error) {
			return oracle.RulesResult{}, errors.New("oracle: the checker stopped: boom")
		},
	} {
		withRulesOracle(t, f)
		m, rep, err := combCapped().Build()
		assertFailedClosed(t, "Build, rules oracle "+name, m, err)
		if !strings.Contains(err.Error(), "rules oracle") {
			t.Errorf("%s: error %q does not name the rules oracle", name, err)
		}
		if rep == nil || rep.Assurance != AssuranceNone || rep.OracleDisagreement == "" {
			t.Fatalf("%s: report %+v; want Assurance none and the disagreement", name, rep)
		}
	}
}
