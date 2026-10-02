package gsm

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// costMachines are machines that stress one term of the rules oracle's cost
// each. scale picks their size: the measurement uses large ones.
func costMachines(scale int) map[string]*Registry {
	ms := map[string]*Registry{}
	// States: flags, one event.
	ms["states"] = combFlags(scale+6, 1, 0)
	// Pairs: 20 events on flags, every pair.
	ms["pairs"] = combFlags(scale, 20, -1)
	// Many events, one pair: the event walk (evAt) and pairsOkA.
	ms["events"] = combFlags(4, 40*scale, 1)
	// Many events, every pair, few states.
	ms["events, every pair"] = combFlags(1, 20*scale, -1)
	// Large rules in every pair (the review of #17).
	ms["rule size x pairs"] = exprPairs(4, 25*scale)
	// Repair depth: a counter walked down one step at a time.
	{
		r := NewRegistry("depth")
		x := r.Int("x", 0, 1<<(scale-1)-1)
		r.DeclInvariant("zero", Le(V(x), Lit(0)), Do(Set(x, Sub(V(x), Lit(1)))))
		r.DeclEvent("noop", Do())
		ms["repair depth"] = r
	}
	// Invariants that always hold.
	{
		r := NewRegistry("invs")
		var fs []Var
		for i := 0; i < scale; i++ {
			fs = append(fs, r.Bool(fmt.Sprintf("f%d", i)))
		}
		for k := 0; k < 200; k++ {
			r.DeclInvariant(fmt.Sprintf("i%d", k), Le(Lit(0), Lit(1)), Do())
		}
		r.DeclEvent("raise", Raise(fs[0]))
		ms["invariants"] = r
	}
	// Reads and writes of the last variable, whose domain is large.
	{
		r := NewRegistry("reads")
		var fs []Var
		for i := 0; i < scale-6; i++ {
			fs = append(fs, r.Bool(fmt.Sprintf("f%d", i)))
		}
		x := r.Int("x", 0, 63)
		e := Expr(Lit(0))
		for k := 0; k < 50; k++ {
			e = Add(e, Sub(V(x), V(x)))
		}
		r.DeclEvent("w", Do(Set(x, e), Set(x, e)))
		r.DeclEvent("r", Raise(fs[0]))
		ms["variable reads"] = r
	}
	// Many writes.
	{
		r := NewRegistry("writes")
		var fs []Var
		for i := 0; i < scale; i++ {
			fs = append(fs, r.Bool(fmt.Sprintf("f%d", i)))
		}
		var as []Assign
		for k := 0; k < 100; k++ {
			as = append(as, Set(fs[len(fs)-1], Lit(1)))
		}
		r.DeclEvent("w", Do(as...))
		r.DeclEvent("v", Raise(fs[0]))
		ms["writes"] = r
	}
	// Deep predicates.
	{
		r := NewRegistry("preds")
		var fs []Var
		for i := 0; i < scale; i++ {
			fs = append(fs, r.Bool(fmt.Sprintf("f%d", i)))
		}
		var ps []Pred
		for k := 0; k < 100; k++ {
			ps = append(ps, Not(Lt(V(fs[k%scale]), Lit(0))))
		}
		r.DeclEventGuarded("g", And(ps...), Raise(fs[0]))
		r.DeclEventGuarded("h", Or(ps...), Raise(fs[1]))
		ms["predicates"] = r
	}
	return ms
}

// randomCostMachine: a random combinator machine (as in the review fuzz of
// #17), sized up so that the oracle's run is measurable.
func randomCostMachine(rnd *rand.Rand, it int) *Registry {
	r := NewRegistry(fmt.Sprintf("rc%d", it))
	var vs []Var
	nv := 4 + rnd.Intn(8)
	for i := 0; i < nv; i++ {
		switch rnd.Intn(3) {
		case 0:
			lo := rnd.Intn(5) - 2
			vs = append(vs, r.Int(fmt.Sprintf("i%d", i), lo, lo+1+rnd.Intn(6)))
		case 1:
			vs = append(vs, r.Bool(fmt.Sprintf("b%d", i)))
		default:
			vs = append(vs, r.Enum(fmt.Sprintf("e%d", i), "x", "y", "z"))
		}
	}
	var expr func(d int) Expr
	expr = func(d int) Expr {
		switch k := rnd.Intn(4); {
		case d > 3 || k == 0:
			return Lit(rnd.Intn(3))
		case k == 1:
			return V(vs[rnd.Intn(nv)])
		case k == 2:
			return Add(expr(d+1), expr(d+1))
		default:
			return Sub(expr(d+1), expr(d+1))
		}
	}
	pred := func() Pred {
		a, b := expr(1), expr(1)
		switch rnd.Intn(4) {
		case 0:
			return Le(a, b)
		case 1:
			return Not(Eq(a, b))
		case 2:
			return Or(Lt(a, b), Le(b, a))
		}
		return Ge(a, b)
	}
	for k := 0; k < rnd.Intn(4); k++ {
		v := vs[rnd.Intn(nv)]
		r.DeclInvariant(fmt.Sprintf("inv%d", k), AtMost(v, 1), SetTo(v, 0))
	}
	ne := 2 + rnd.Intn(10)
	for k := 0; k < ne; k++ {
		as := []Assign{Set(vs[rnd.Intn(nv)], expr(0))}
		if rnd.Intn(2) == 0 {
			r.DeclEventGuarded(fmt.Sprintf("ev%d", k), pred(), Do(as...))
		} else {
			r.DeclEvent(fmt.Sprintf("ev%d", k), Do(as...))
		}
	}
	return r
}

// measureRules times CheckRules on r's rules and samples the live heap.
func measureRules(t *testing.T, name string, r *Registry) (nsPerStep, bytesPerStep float64, ok bool) {
	t.Helper()
	m, rep, err := r.build(true)
	if err != nil || m == nil {
		t.Logf("%s: not built: %v", name, err)
		return 0, 0, false
	}
	var mb, pb strings.Builder
	if r.WriteMachineAST(&mb) != nil || r.WriteDeclaredPairs(&pb) != nil {
		return 0, 0, false
	}
	w, err := oracle.RulesCost(mb.String(), pb.String(), rep.MaxRepairLen)
	if err != nil {
		t.Fatalf("%s: RulesCost: %v", name, err)
	}
	runtime.GC()
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	base := sample[0].Value.Uint64()
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		for {
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
				metrics.Read(s)
				if v := s[0].Value.Uint64(); v > peak.Load() {
					peak.Store(v)
				}
			}
		}
	}()
	start := time.Now()
	res, err := oracle.CheckRules(mb.String(), pb.String())
	el := time.Since(start)
	close(done)
	total, _ := new(big.Float).SetInt(w.Total).Float64()
	live := float64(0)
	if p := peak.Load(); p > base {
		live = float64(p - base)
	}
	t.Logf("%-20s %v: %v (%v), %.3g s, %.0f MB live; %.2f ns, %.2f B per step", name, w, res.Verdict, err, el.Seconds(), live/1e6, float64(el.Nanoseconds())/total, live/total)
	return float64(el.Nanoseconds()) / total, live / total, res.Verdict == oracle.RulesCertified
}

// The calibration of RulesOracleMaxWork: the rules oracle's time and memory
// per step of RulesCost, on machines that stress each term, and on random
// machines. GSM_RULES_COST=<scale> runs it (14 or so: a few minutes).
func TestRulesOracleCostPerStep(t *testing.T) {
	var scale int
	if _, err := fmt.Sscan(os.Getenv("GSM_RULES_COST"), &scale); err != nil {
		t.Skip("GSM_RULES_COST=<scale> runs the calibration")
	}
	worstNs, worstB := 0.0, 0.0
	for name, r := range costMachines(scale) {
		ns, b, _ := measureRules(t, name, r)
		worstNs, worstB = max(worstNs, ns), max(worstB, b)
	}
	rnd := rand.New(rand.NewSource(int64(scale)))
	for it := 0; it < 3*scale; it++ {
		ns, b, _ := measureRules(t, fmt.Sprintf("random %d", it), randomCostMachine(rnd, it))
		worstNs, worstB = max(worstNs, ns), max(worstB, b)
	}
	t.Logf("worst: %.2f ns and %.2f B per step; at RulesOracleMaxWork (%d): %.2f s, %.0f MB",
		worstNs, worstB, RulesOracleMaxWork, worstNs*RulesOracleMaxWork/1e9, worstB*RulesOracleMaxWork/1e6)
}
