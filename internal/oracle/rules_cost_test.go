package oracle

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func mustCost(t *testing.T, machine, pairs string, depth int) RulesWork {
	t.Helper()
	w, err := RulesCost(machine, pairs, depth)
	if err != nil {
		t.Fatalf("RulesCost(%q, %q, %d): %v", machine, pairs, depth, err)
	}
	return w
}

func delta(a, b *big.Int) int64 { return new(big.Int).Sub(b, a).Int64() }

// One Bool, one event setting it: every term by hand.
func TestRulesCostByHand(t *testing.T) {
	m := "(doms 2)\n(ev (do (set 0 (lit 1))))\n"
	w := mustCost(t, m, "pairs all", 0)
	// The write: 1 + lit 1 + (2 + cellSteps) x (index 0 + 1) + toNatSteps;
	// the transform 1 + that; the event 1 (its guard, (and)) + the transform.
	write := int64(1 + 1 + (2+cellSteps)*1 + toNatSteps)
	ev := 1 + (1 + write)
	// No invariants: allValid 1, one normalize iteration 2 (allValid, find).
	// normalize: fuelOf 4 x (V + 1), and depth + 1 = 1 iteration.
	allValid, iter := int64(1), int64(2)
	norm := 4*2 + 1*iter
	// One event, every pair: no pair; allPairs builds the seq over the event.
	pairsA := int64(cellSteps * (1 + 1))
	perState := 3*cellSteps*2 + // box, three times
		2*norm + 2*allValid + // wfc, twice
		allValid + cellSteps*2 + 2 + // inDA
		pairsA
	fixed := int64(len(m)+len("pairs all")) + 8*ev + pairsA
	if w.States.Int64() != 2 || w.PerState.Int64() != perState || w.Fixed.Int64() != fixed || w.Total.Int64() != 2*perState+fixed {
		t.Errorf("RulesCost = %v; want 2 states x %d steps per state + %d", w, perState, fixed)
	}
	if !strings.Contains(w.String(), fmt.Sprintf("2 states x %d steps per state + %d = %d", perState, fixed, 2*perState+fixed)) {
		t.Errorf("String() = %q", w)
	}
}

// stepI e walks the event list to e: checking the pair (1, 1) costs 4 steps
// more per state than (0, 0), and the input is as long.
func TestRulesCostWalksToTheEvent(t *testing.T) {
	m := "(doms 2)\n(ev (do (set 0 (lit 1))))\n(ev (do (set 0 (lit 1))))\n"
	a, b := mustCost(t, m, "pairs 1 0 0", 0), mustCost(t, m, "pairs 1 1 1", 0)
	if d := delta(a.PerState, b.PerState); d != 4 || delta(a.Fixed, b.Fixed) != 0 {
		t.Errorf("pair (1, 1) against (0, 0): %d more steps per state, %d more fixed; want 4 and 0", d, delta(a.Fixed, b.Fixed))
	}
}

// Three events on four variables, guards (le (lit 0) G) with G of weight g.
func threeEvents(g0 string, inv string) string {
	return "(doms 2 2 3 4)\n" + inv +
		"(evwhen (le (lit 0) " + g0 + ") (do (set 0 (lit 1))))\n" +
		"(evwhen (le (lit 0) (lit 1)) (do (set 1 (lit 1))))\n" +
		"(ev (do (set 2 (lit 1))))\n"
}

// Each term at its boundary: one more node, one more repair step, one more
// pair changes the steps per state by exactly the evaluations it adds.
func TestRulesCostCountsEachTerm(t *testing.T) {
	small, big2 := "(lit 1)", "(add (lit 1) (lit 0))" // 2 more nodes
	for _, c := range []struct {
		pairs string
		want  int64 // per state: 2 stepI per pair the event is in, each +2
	}{
		{"pairs all", 2 * 2 * 2},   // event 0 is in 2 of the 3 pairs
		{"pairs 1 0 1", 1 * 2 * 2}, // in the one pair
		{"pairs 2 0 1 0 2", 2 * 2 * 2},
		{"pairs 1 1 2", 0},             // not in the pair: its guard is never evaluated
		{"pairs 2 0 1 0 1", 2 * 2 * 2}, // a pair declared twice is checked twice
	} {
		a := mustCost(t, threeEvents(small, ""), c.pairs, 0)
		b := mustCost(t, threeEvents(big2, ""), c.pairs, 0)
		if d := delta(a.PerState, b.PerState); d != c.want {
			t.Errorf("%s: two more guard nodes add %d steps per state; want %d", c.pairs, d, c.want)
		}
		if d := delta(a.Fixed, b.Fixed); d != int64(len(big2)-len(small))+8*2 {
			t.Errorf("%s: two more guard nodes add %d fixed steps; want the input and 8 x 2", c.pairs, d)
		}
	}
	// A not is one node.
	a0 := mustCost(t, threeEvents(small, ""), "pairs all", 0)
	n0 := mustCost(t, strings.Replace(threeEvents(small, ""), "(le (lit 0) (lit 1)) (do (set 0", "(not (le (lit 0) (lit 1))) (do (set 0", 1), "pairs all", 0)
	if d := delta(a0.PerState, n0.PerState); d != 2*2*1 {
		t.Errorf("a not adds %d steps per state; want 4", d)
	}
	// pairsOkA counts the events twice per declared pair: one more pair (3
	// events) is 2 x 3 + 2 more fixed steps, and its 4 input bytes.
	p1, p2 := mustCost(t, threeEvents(small, ""), "pairs 1 0 1", 0), mustCost(t, threeEvents(small, ""), "pairs 2 0 1 0 1", 0)
	if d := delta(p1.Fixed, p2.Fixed); d != 2*3+2+4 {
		t.Errorf("one more declared pair adds %d fixed steps; want %d", d, 2*3+2+4)
	}
	// A variable read walks to the variable twice and converts its value, one
	// step per binary digit of its domain: (var 3) of domain 4 against (lit 1)
	// is 2 + 2 x 4 + 3 - 1 more, in each of 2 pairs, twice.
	lit := mustCost(t, threeEvents(small, ""), "pairs all", 0)
	read := mustCost(t, threeEvents("(var 3)", ""), "pairs all", 0)
	if d := delta(lit.PerState, read.PerState); d != 2*2*(2+2*4+3-1) {
		t.Errorf("a read of variable 3 adds %d steps per state; want %d", d, 2*2*(2+2*4+3-1))
	}
	// Repair depth: one more iteration of normalize in wfc (twice) and in every
	// stepI (4 per pair, 3 pairs).
	inv := "(inv (le (var 2) (lit 1)) (do (set 2 (lit 1))))\n"
	for _, d := range []int{0, 1, 5} {
		a := mustCost(t, threeEvents(small, inv), "pairs all", d)
		b := mustCost(t, threeEvents(small, inv), "pairs all", d+1)
		// iter: allValid twice (1 + 1 + le, var 2, lit 1) and the repair.
		pred := int64(1 + (2 + 2*3 + 2) + 1)
		repair := int64(1 + 1 + 1 + (2+cellSteps)*3 + toNatSteps)
		iter := 2*(1+1+pred) + repair
		if got := delta(a.PerState, b.PerState); got != iter*(2+4*3) {
			t.Errorf("depth %d to %d adds %d steps per state; want %d", d, d+1, got, iter*(2+4*3))
		}
		if delta(a.Fixed, b.Fixed) != 0 {
			t.Errorf("the repair depth changes the fixed steps")
		}
	}
	// An invariant's predicate: allValid + 1, so each normalize iteration + 2:
	// per state, wfc 2 x (2 x (depth + 1)) + 2, inDA 1, and 4 per pair x 3.
	inv2 := "(inv (le (var 2) (add (lit 1) (lit 0))) (do (set 2 (lit 1))))\n"
	for _, d := range []int{0, 3} {
		a := mustCost(t, threeEvents(small, inv), "pairs all", d)
		b := mustCost(t, threeEvents(small, inv2), "pairs all", d)
		dn := int64(2 * 2 * (d + 1)) // two more nodes, evaluated twice per iteration
		want := 2*dn + 2*2 + 2 + 4*3*dn
		if got := delta(a.PerState, b.PerState); got != want {
			t.Errorf("depth %d: two more invariant nodes add %d steps per state; want %d", d, got, want)
		}
	}
	// States: the per-state steps times the box, plus the fixed steps.
	w := mustCost(t, threeEvents(small, inv), "pairs all", 2)
	if w.States.Int64() != 2*2*3*4 || w.Total.Cmp(new(big.Int).Add(new(big.Int).Mul(w.States, w.PerState), w.Fixed)) != 0 {
		t.Errorf("RulesCost = %v; want 48 states and Total = States x PerState + Fixed", w)
	}
}

// Every pair, many events: each event is in E-1 pairs, and the event walk
// (evAt) grows with the index.
func TestRulesCostEveryPairMatchesTheDeclaredList(t *testing.T) {
	var b strings.Builder
	b.WriteString("(doms 2 3)\n")
	const E = 9
	for e := 0; e < E; e++ {
		fmt.Fprintf(&b, "(evwhen (le (lit 0) (var %d)) (do (set %d (lit %d))))\n", e%2, e%2, e%2)
	}
	var ps []string
	for i := 0; i < E; i++ {
		for j := i + 1; j < E; j++ {
			ps = append(ps, fmt.Sprintf("%d %d", i, j))
		}
	}
	all := mustCost(t, b.String(), "pairs all", 1)
	list := mustCost(t, b.String(), fmt.Sprintf("pairs %d %s", len(ps), strings.Join(ps, " ")), 1)
	// The same pairs; only pairsA differs (allPairs is rebuilt per state, a
	// declared list is not), and the input.
	pairsA := int64(cellSteps*(1+E+3*len(ps))) - 1
	if d := delta(list.PerState, all.PerState); d != pairsA {
		t.Errorf("every pair costs %d more steps per state than the same pairs declared; want %d (allPairs)", d, pairsA)
	}
}

func TestRulesCostRefusesWhatCheckRulesRefuses(t *testing.T) {
	for name, in := range map[string][2]string{
		"no doms":           {"(ev (do (set 0 (lit 1))))", "pairs all"},
		"variable past end": {"(doms 2)\n(ev (do (set 1 (lit 1))))", "pairs all"},
		"unbalanced":        {"(doms 2", "pairs all"},
		"pair past events":  {"(doms 2)\n(ev (do (set 0 (lit 1))))", "pairs 1 0 1"},
		"bad pairs":         {"(doms 2)\n(ev (do (set 0 (lit 1))))", "pairs x"},
	} {
		if _, err := RulesCost(in[0], in[1], 0); err == nil {
			t.Errorf("%s: RulesCost accepted it", name)
		}
	}
	if _, err := RulesCost("(doms 2)\n", "pairs all", -1); err == nil {
		t.Error("RulesCost accepted a negative repair depth")
	}
}
