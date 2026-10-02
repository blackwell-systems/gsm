package oracle

import (
	"fmt"
	"math/big"
	"math/bits"
	"strings"
)

// The rules oracle's cost.
//
// CheckRules runs bounded, signSafe, wfc and checkBuild (AstChecker.v), which
// is bounded, signSafe, pairsOkA, wfc and ccA. RulesCost counts, from the same
// input, an upper bound on the basic steps this takes in the generated Go: a
// list cell visited, an expression or predicate node evaluated, a binary digit
// of a nat converted to Z or back, a call of fuelOf's fold, and cellSteps per
// list cell built (an allocation, and later work for the collector). Per state of the box (every
// valuation, in range or not):
//   - box builds the state's valuation, three times (wfc twice, ccA once);
//   - wfc, twice: normalize the state, then allValid;
//   - ccA: inDA (allValid, and a comparison with the zero state), pairsA
//     (allPairs is rebuilt for each state when every pair is checked), and for
//     each checked pair (a, b): stepI a and stepI b twice each, and a comparison.
//
// normalize repairs at most depth times (the machine's longest repair chain),
// so it evaluates allValid and repair1 at most depth+1 times; repair1 is a find
// over the invariants' predicates and one repair. stepI e walks the event list
// to e, evaluates e's guard and effect, then normalizes. A variable read walks
// the valuation and the minimums to its index and converts the value to Z; a
// write walks the minimums, the domains and the valuation to its index and
// converts the value back. Independent of the states: reading the input,
// bounded and signSafe (twice each) over the rules, and pairsOkA, which
// counts the events twice per pair.

// RulesWork is RulesCost's bound: Total = States x PerState + Fixed.
type RulesWork struct {
	States   *big.Int // the box: the product of the domains
	PerState *big.Int // the most steps CheckRules takes per state
	Fixed    *big.Int // the steps that do not depend on the states
	Total    *big.Int
}

func (w RulesWork) String() string {
	return fmt.Sprintf("%v states x %v steps per state + %v = %v", w.States, w.PerState, w.Fixed, w.Total)
}

// toNatSteps bounds a write's conversion of a value to a nat (Z.to_nat walks
// its binary digits; a bounded value has at most 32), with the subtraction of
// the minimum and the clamp.
const toNatSteps = 34

// cellSteps is the steps a list cell built counts as. Allocation dominates the
// box and writes: measured on the generated Go, a cell costs several times an
// evaluated node.
const cellSteps = 8

// RulesCost bounds CheckRules(machine, pairs)'s steps, for a machine whose
// longest repair chain from any state in the box is depth. It reads the input
// as CheckRules does, and returns an error where CheckRules would.
func RulesCost(machine, pairs string, depth int) (RulesWork, error) {
	if depth < 0 {
		return RulesWork{}, fmt.Errorf("oracle: rules cost: negative repair depth %d", depth)
	}
	var p rulesParser
	var forms []sexp
	var ne int
	if err := catch(func() { forms = parseAll(tokenize(machine)); _, ne = p.machine(forms) }); err != nil {
		return RulesWork{}, fmt.Errorf("oracle: rules: %v", err)
	}
	var ps *I_option[*I_list[*I_prod[int64, int64]]]
	if err := catch(func() { ps = p.pairs(pairs, ne) }); err != nil {
		return RulesWork{}, fmt.Errorf("oracle: pairs: %v", err)
	}
	var c ruleCost
	for _, f := range forms {
		if head(f) == "doms" {
			c.doms = ints(f.list[1:], natOf)
		}
	}
	// The rules' weights. p.machine accepted the forms, so they are well formed.
	var invPreds, maxRepair, evs []int64
	ast := int64(0)
	for _, f := range forms {
		switch head(f) {
		case "inv":
			pw, rw := c.pred(f.list[1]), c.transform(f.list[2])
			invPreds = append(invPreds, pw)
			maxRepair = append(maxRepair, rw)
			ast += pw + rw
		case "ev":
			w := 1 + c.transform(f.list[1]) // the guard is (and): one node
			evs = append(evs, w)
			ast += w
		case "evwhen":
			w := c.pred(f.list[1]) + c.transform(f.list[2])
			evs = append(evs, w)
			ast += w
		}
	}
	n := func(x int64) *big.Int { return big.NewInt(x) }
	add := func(xs ...*big.Int) *big.Int {
		s := new(big.Int)
		for _, x := range xs {
			s.Add(s, x)
		}
		return s
	}
	mul := func(xs ...*big.Int) *big.Int {
		s := n(1)
		for _, x := range xs {
			s.Mul(s, x)
		}
		return s
	}
	V, E := n(int64(len(c.doms))), n(int64(len(evs)))
	states := n(1)
	for _, d := range c.doms {
		states.Mul(states, n(d))
	}
	// allValid: forallb over the invariants' predicates.
	allValid := n(1)
	for _, pw := range invPreds {
		allValid.Add(allValid, n(1+pw))
	}
	// One normalize iteration: allValid, then repair1 (a find over the same
	// predicates, then one repair).
	repair := n(0)
	for _, rw := range maxRepair {
		if r := n(rw); r.Cmp(repair) > 0 {
			repair = r
		}
	}
	iter := add(allValid, allValid, repair)
	cells := func(x *big.Int) *big.Int { return mul(n(cellSteps), x) }
	V1 := add(V, n(1))
	// normalize from fuelOf (a fold over the domains, a closure call each): at
	// most depth+1 iterations.
	norm := add(mul(n(4), V1), mul(n(int64(depth)+1), iter))
	// stepI e: evAt walks to e, then the guard, the effect and normalize.
	step := make([]*big.Int, len(evs))
	stepSum := n(0)
	for e, w := range evs {
		step[e] = add(n(int64(e)+2+w), norm)
		stepSum.Add(stepSum, step[e])
	}
	// The checked pairs: per pair, stepI a and stepI b twice each and a
	// comparison of two valuations.
	cmp := V1
	var nPairs, pairSum, pairsA *big.Int
	if toks := strings.Fields(pairs); len(toks) == 2 && toks[1] == "all" { // None: every pair a < b.
		// Each event is in E-1 of the E(E-1)/2 pairs.
		em1 := new(big.Int).Sub(E, n(1))
		if em1.Sign() < 0 {
			em1.SetInt64(0)
		}
		nPairs = new(big.Int).Rsh(mul(E, em1), 1)
		pairSum = add(mul(nPairs, cmp), mul(n(2), em1, stepSum))
		// allPairs: seq over the events, then per event a seq and a map, and
		// flat_map's appends: three cells per pair.
		pairsA = cells(add(n(1), E, mul(n(3), nPairs)))
	} else {
		nPairs, pairSum, pairsA = n(0), n(0), n(1)
		for l := ps.f0_0; l.tag == 1; l = l.f1_1 { // Some l
			a, b := l.f1_0.f0_0, l.f1_0.f0_1
			nPairs.Add(nPairs, n(1))
			pairSum.Add(pairSum, add(mul(n(2), step[a]), mul(n(2), step[b]), cmp))
		}
	}
	perState := add(
		mul(n(3), cells(V1)),     // box, three times
		mul(n(2), norm),          // wfc, twice: normalize ...
		mul(n(2), allValid),      // ... then allValid
		allValid, cells(V1), cmp, // inDA: allValid, zeroV, valeqb
		pairsA,
		pairSum,
	)
	fixed := add(
		n(int64(len(machine)+len(pairs))),            // reading the input
		mul(n(8), n(ast)),                            // bounded and signSafe, twice each
		mul(nPairs, add(mul(n(2), E), n(2))), pairsA, // pairsOkA
	)
	return RulesWork{
		States:   states,
		PerState: perState,
		Fixed:    fixed,
		Total:    add(mul(states, perState), fixed),
	}, nil
}

// ruleCost weighs the rules' nodes by the steps evaluating them takes.
type ruleCost struct{ doms []int64 }

// expr: a read walks the valuation and the minimums to the variable and
// converts the stored value (below its domain) to Z, one step per binary digit.
func (c *ruleCost) expr(x sexp) int64 {
	switch head(x) {
	case "var":
		i := natOf(x.list[1].atom)
		return 2 + 2*(i+1) + int64(bits.Len64(uint64(c.doms[i])))
	case "lit":
		return 1
	}
	return 1 + c.expr(x.list[1]) + c.expr(x.list[2]) // add, sub
}

func (c *ruleCost) pred(x sexp) int64 {
	switch head(x) {
	case "and", "or":
		w := int64(1)
		for _, q := range x.list[1:] {
			w += 1 + c.pred(q)
		}
		return w
	case "not":
		return 1 + c.pred(x.list[1])
	}
	return 1 + c.expr(x.list[1]) + c.expr(x.list[2]) // le, lt, eq
}

// transform: each write evaluates its expression, walks the minimums and the
// domains to its variable, converts the value to a nat, and rebuilds the
// valuation up to the variable.
func (c *ruleCost) transform(x sexp) int64 {
	w := int64(1)
	for _, a := range x.list[1:] {
		i := natOf(a.list[1].atom)
		w += 1 + c.expr(a.list[2]) + (2+cellSteps)*(i+1) + toNatSteps
	}
	return w
}
