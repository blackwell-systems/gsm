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

// Above the cap on states x (events + pairs), pairs being every pair when none
// is declared, the rules oracle does not run; at the cap it does.
func TestBuildSkipsTheRulesOracleAboveItsCap(t *testing.T) {
	three := func(declare bool) *Registry {
		r := combCapped()
		r.DeclEvent("raiseb2", Raise(r.vars[1]))
		if declare {
			r.Independent("inca", "raiseb")
		}
		return r
	}
	no := func() *Registry {
		r := NewRegistry("no_events")
		r.Int("a", 0, 3)
		return r
	}
	for _, c := range []struct {
		name string
		mk   func() *Registry
		cap  int
		run  bool
		msg  string
	}{
		// combCapped: 4 x 2 = 8 states, 2 events, 1 pair (every pair).
		{"two events", combCapped, 23, false, "8 states x (2 events + 1 pairs) = 24"},
		{"two events", combCapped, 24, true, ""},
		// Three events: every pair is 3 pairs.
		{"three events", func() *Registry { return three(false) }, 47, false, "8 states x (3 events + 3 pairs) = 48"},
		{"three events", func() *Registry { return three(false) }, 48, true, ""},
		// Declaring one pair independent leaves one.
		{"three events, one pair", func() *Registry { return three(true) }, 31, false, "8 states x (3 events + 1 pairs) = 32"},
		{"three events, one pair", func() *Registry { return three(true) }, 32, true, ""},
		// No events: the work is the states.
		{"no events", no, 3, false, "4 states x (0 events + 0 pairs) = 4"},
		{"no events", no, 4, true, ""},
	} {
		withRulesCap(t, c.cap)
		ran := false
		withRulesOracle(t, func(m, p string) (oracle.RulesResult, error) { ran = true; return oracle.CheckRules(m, p) })
		m, rep, err := c.mk().Build()
		if err != nil || m == nil {
			t.Fatalf("%s, cap %d: Build: %v\n%s", c.name, c.cap, err, rep)
		}
		want := AssuranceOracleTables
		if c.run {
			want = AssuranceOracleTablesAndRules
		}
		if ran != c.run || rep.Assurance != want {
			t.Errorf("%s, cap %d: ran = %v, Assurance = %v; want %v, %v", c.name, c.cap, ran, rep.Assurance, c.run, want)
		}
		if !c.run && !strings.Contains(rep.RulesOracleSkipped, c.msg+" is above RulesOracleMaxWork ("+fmt.Sprint(c.cap)+")") {
			t.Errorf("%s, cap %d: RulesOracleSkipped = %q; want it to give %q and the cap", c.name, c.cap, rep.RulesOracleSkipped, c.msg)
		}
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
