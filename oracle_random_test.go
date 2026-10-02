package gsm_test

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestDifferential_RandomCombinators builds random combinator machines so the
// differential cross-check in oracle_differential_test.go (which sees every
// Build) has rules-level cases beyond the hand-written ones: signed minimums
// and literals, Sub, Bool/Enum/Int writes, guards, invariants with repairs, and
// sometimes declared independent pairs. The machines are small enough for the
// rules oracle to enumerate. Without the oracles this is only a Build smoke test.
// GSM_DIFF_RANDOM sets the count (default 600 with oracles, 60 without).
func TestDifferential_RandomCombinators(t *testing.T) {
	n := 60
	if os.Getenv(envASTOracle) != "" {
		n = 600
	}
	if s := os.Getenv("GSM_DIFF_RANDOM"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("GSM_DIFF_RANDOM: %v", err)
		}
		n = v
	}
	ok := 0
	for i := 0; i < n; i++ {
		r := randomCombinatorMachine(rand.New(rand.NewSource(int64(i))), fmt.Sprintf("comb_%d", i))
		if _, _, err := r.Build(); err == nil {
			ok++
		}
	}
	t.Logf("%d of %d random combinator machines built", ok, n)
}

func randomCombinatorMachine(rng *rand.Rand, name string) *gsm.Registry {
	r := gsm.NewRegistry(name)
	nv := 2 + rng.Intn(3)
	vars := make([]gsm.Var, nv)
	// binary marks two-valued variables with minimum 0 (every Bool, Int 0..1, and
	// two-value Enum). The rules oracle certifies a write into one only when the
	// value cannot be negative, so most writes into them use non-negative forms.
	binary := make([]bool, nv)
	for i := range vars {
		vn := fmt.Sprintf("v%d", i)
		switch rng.Intn(4) {
		case 0:
			vars[i] = r.Bool(vn)
			binary[i] = true
		case 1:
			vals := []string{"a", "b", "c"}[:2+rng.Intn(2)]
			vars[i] = r.Enum(vn, vals...)
			binary[i] = len(vals) == 2
		default:
			lo := rng.Intn(5) - 3
			hi := lo + 1 + rng.Intn(3)
			vars[i] = r.Int(vn, lo, hi)
			binary[i] = lo == 0 && hi == 1
		}
	}
	var expr func(depth int) gsm.Expr
	expr = func(depth int) gsm.Expr {
		if depth <= 0 || rng.Intn(3) == 0 {
			if rng.Intn(2) == 0 {
				return gsm.V(vars[rng.Intn(nv)])
			}
			return gsm.Lit(rng.Intn(7) - 3)
		}
		if rng.Intn(2) == 0 {
			return gsm.Add(expr(depth-1), expr(depth-1))
		}
		return gsm.Sub(expr(depth-1), expr(depth-1))
	}
	var pred func(depth int) gsm.Pred
	pred = func(depth int) gsm.Pred {
		if depth > 0 && rng.Intn(4) == 0 {
			switch rng.Intn(3) {
			case 0:
				return gsm.And(pred(depth-1), pred(depth-1))
			case 1:
				return gsm.Or(pred(depth-1), pred(depth-1))
			default:
				return gsm.Not(pred(depth - 1))
			}
		}
		a, b := expr(1), expr(1)
		switch rng.Intn(6) {
		case 0:
			return gsm.Le(a, b)
		case 1:
			return gsm.Lt(a, b)
		case 2:
			return gsm.Eq(a, b)
		case 3:
			return gsm.Ge(a, b)
		case 4:
			return gsm.Gt(a, b)
		default:
			return gsm.Ne(a, b)
		}
	}
	nonNegBinary := func() gsm.Expr {
		var bs []gsm.Var
		for i, b := range binary {
			if b {
				bs = append(bs, vars[i])
			}
		}
		switch rng.Intn(4) {
		case 0:
			return gsm.Lit(rng.Intn(2))
		case 1:
			return gsm.V(bs[rng.Intn(len(bs))])
		case 2:
			return gsm.Sub(gsm.Lit(1), gsm.V(bs[rng.Intn(len(bs))]))
		default:
			return gsm.Add(gsm.V(bs[rng.Intn(len(bs))]), gsm.Lit(rng.Intn(2)))
		}
	}
	transform := func() gsm.Transform {
		k := 1 + rng.Intn(2)
		as := make([]gsm.Assign, k)
		for i := range as {
			j := rng.Intn(nv)
			e := expr(2)
			if binary[j] && rng.Intn(6) != 0 {
				e = nonNegBinary()
			}
			as[i] = gsm.Set(vars[j], e)
		}
		return gsm.Do(as...)
	}
	for i, ni := 0, rng.Intn(3); i < ni; i++ {
		r.DeclInvariant(fmt.Sprintf("inv%d", i), pred(1), transform())
	}
	ne := 2 + rng.Intn(3)
	for i := 0; i < ne; i++ {
		en := fmt.Sprintf("e%d", i)
		if rng.Intn(3) == 0 {
			r.DeclEventGuarded(en, pred(1), transform())
		} else {
			r.DeclEvent(en, transform())
		}
	}
	if rng.Intn(6) == 0 {
		r.Independent("e0", "e1")
	}
	return r
}
