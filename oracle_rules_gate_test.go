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

// Above the cap on the rules oracle's work (oracle.RulesCost of the exported
// rules, the declared pairs and the repair depth) the rules oracle does not
// run; at the cap it does. The machines vary each term: events, every pair or
// declared pairs, no events, repair depth, invariants. internal/oracle's tests
// check each term of the work itself.
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
	// A counter in 0..7 walked down to 0 one step at a time: repair depth 7.
	deep := func() *Registry {
		r := NewRegistry("deep")
		x := r.Int("x", 0, 7)
		r.DeclInvariant("zero", Le(V(x), Lit(0)), Do(Set(x, Sub(V(x), Lit(1)))))
		r.DeclEvent("noop", Do())
		return r
	}
	// Three invariants that always hold.
	invs := func() *Registry {
		r := NewRegistry("invs")
		a := r.Bool("a")
		r.Bool("b")
		for k := 0; k < 3; k++ {
			r.DeclInvariant(fmt.Sprintf("i%d", k), Le(Lit(0), Lit(1)), Do())
		}
		r.DeclEvent("raise", Raise(a))
		return r
	}
	for _, c := range []struct {
		name  string
		mk    func() *Registry
		depth int // Report.MaxRepairLen
	}{
		{"two events", combCapped, 1},
		{"three events, every pair", func() *Registry { return three(false) }, 1},
		{"three events, one pair", func() *Registry { return three(true) }, 1},
		{"no events", no, 0},
		{"repair depth", deep, 7},
		{"invariants", invs, 0},
	} {
		var mb, pb strings.Builder
		if err := c.mk().WriteMachineAST(&mb); err != nil {
			t.Fatal(err)
		}
		if err := c.mk().WriteDeclaredPairs(&pb); err != nil {
			t.Fatal(err)
		}
		w, werr := oracle.RulesCost(mb.String(), pb.String(), c.depth)
		if werr != nil {
			t.Fatalf("%s: RulesCost: %v", c.name, werr)
		}
		if !w.Total.IsInt64() {
			t.Fatalf("%s: work %v", c.name, w)
		}
		at := int(w.Total.Int64())
		for _, capv := range []int{at - 1, at} {
			run := capv == at
			withRulesCap(t, capv)
			ran := false
			withRulesOracle(t, func(m, p string) (oracle.RulesResult, error) { ran = true; return oracle.CheckRules(m, p) })
			m, rep, err := c.mk().Build()
			if err != nil || m == nil {
				t.Fatalf("%s, cap %d: Build: %v\n%s", c.name, capv, err, rep)
			}
			if rep.MaxRepairLen != c.depth {
				t.Fatalf("%s: MaxRepairLen = %d, want %d", c.name, rep.MaxRepairLen, c.depth)
			}
			want := AssuranceOracleTables
			if run {
				want = AssuranceOracleTablesAndRules
			}
			if ran != run || rep.Assurance != want {
				t.Errorf("%s, cap %d (work %v): ran = %v, Assurance = %v; want %v, %v", c.name, capv, w.Total, ran, rep.Assurance, run, want)
			}
			if msg := fmt.Sprintf("the rules oracle's work, %v, is above RulesOracleMaxWork (%d)", w, capv); !run && !strings.Contains(rep.RulesOracleSkipped, msg) {
				t.Errorf("%s, cap %d: RulesOracleSkipped = %q; want %q", c.name, capv, rep.RulesOracleSkipped, msg)
			}
		}
	}
}

// When the rules oracle's cost cannot be computed, there is no verdict: Build
// fails closed.
func TestBuildFailsClosedWhenTheRulesCostFails(t *testing.T) {
	saved := rulesCost
	rulesCost = func(string, string, int) (oracle.RulesWork, error) { return oracle.RulesWork{}, errors.New("boom") }
	t.Cleanup(func() { rulesCost = saved })
	m, rep, err := combCapped().Build()
	assertFailedClosed(t, "Build, rules cost fails", m, err)
	if rep == nil || rep.Assurance != AssuranceNone || !strings.Contains(rep.OracleDisagreement, "boom") {
		t.Fatalf("report %+v; want Assurance none and the error", rep)
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

// exprPairs: n counters x_k in 0..2 and a target in 0..2 (3^(n+1) states),
// and n guarded events, every pair checked; event k sets x_k to e_k guarded
// by 0 <= e_k, where e_k = 1 + sum of size terms (x - x) has about 4 x size
// nodes. From the review of #17.
func exprPairs(n, size int) *Registry {
	r := NewRegistry("exprpairs")
	var xs []Var
	for i := 0; i < n; i++ {
		xs = append(xs, r.Int(fmt.Sprintf("x%d", i), 0, 2))
	}
	r.Int("tgt", 0, 2)
	for k := 0; k < n; k++ {
		e := Expr(Lit(1))
		for j := 0; j < size; j++ {
			e = Add(e, Sub(V(xs[(k+j)%n]), V(xs[(k+j)%n])))
		}
		r.DeclEventGuarded(fmt.Sprintf("raise%d", k), Le(Lit(0), e), Do(Set(xs[k], e)))
	}
	return r
}

// The rules oracle evaluates each checked pair's guards and effects at every
// state, so its cost grows with the rules' size: 6 counters, 6 events with
// 2000-node guards and effects and every pair checked was 48114 under the
// count of states x (1 + events + pairs), well within the cap, but takes the
// rules oracle several seconds (8 counters: about 80 s). Its work counts the
// nodes, so it is above the cap.
func TestBuildSkipsTheRulesOracleOnLargeRules(t *testing.T) {
	withRulesOracle(t, func(string, string) (oracle.RulesResult, error) {
		t.Fatal("the rules oracle ran on rules whose evaluation is above the cap")
		return oracle.RulesResult{}, nil
	})
	m, rep, err := exprPairs(6, 500).Build()
	if err != nil || m == nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Assurance != AssuranceOracleTables || !strings.Contains(rep.RulesOracleSkipped, "above RulesOracleMaxWork") {
		t.Fatalf("Assurance = %v, RulesOracleSkipped = %q; want tables only, skipped above the cap", rep.Assurance, rep.RulesOracleSkipped)
	}
}
