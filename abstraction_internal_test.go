package gsm

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// Internal tests for verification by abstraction (abstraction.go): the representative domain
// against normalization-confluence coq/AbstractionCutoff.v, the representative check run
// without the fragment check (what Build refuses to rely on), and a differential test of the
// abstraction outcome against enumeration.

func TestAbsReps_MatchCoq(t *testing.T) {
	cases := []struct {
		n      int
		consts []int
		want   []int
	}{
		{3, []int{5}, []int{2, 3, 4, 5, 6, 7, 8}}, // capped_reps: reps 3 [5] = [5; 6; 7; 8; 4; 3; 2]
		{5, nil, []int{0, 1, 2, 3, 4}},            // reps 5 [] (exact13_passes)
		{3, nil, []int{0, 1, 2}},                  // reps 3 [] (triangle_passes)
		{1, []int{0, 10}, []int{-1, 0, 1, 10, 11}},
		{0, []int{4}, []int{4}},
	}
	for _, c := range cases {
		got, err := absReps(c.n, c.consts)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("absReps(%d, %v) = %v, %v; want %v", c.n, c.consts, got, err, c.want)
		}
	}
	// reps_length: |C|(N+1)+N when no two representatives coincide.
	for n := 0; n < 5; n++ {
		consts := []int{-100, 0, 100}
		got, _ := absReps(n, consts)
		if len(got) != len(consts)*(n+1)+n {
			t.Errorf("n=%d: %d representatives, want %d", n, len(got), len(consts)*(n+1)+n)
		}
	}
}

// absCommutes reports whether every checked pair commutes at every valid representative state.
func absCommutes(r *Registry, a *absResult) bool {
	for _, p := range r.ccPairs() {
		if r.absWitness(a, p[0], p[1]) >= 0 {
			return false
		}
	}
	return true
}

// The representative check alone passes on the gsm form of exact13 (the representatives
// 0..3 never test 13), which is why Build refuses the undeclared literal instead of running
// it (exact13_passes, exact13_diverges); with 13 declared it fails (exact13_declared).
func TestAbs_Exact13RepresentativesPass(t *testing.T) {
	mk := func() *Registry {
		r := NewRegistry("pay")
		amount, tag1, tag2, last := r.Int("amount", 0, 20), r.Int("tag1", 0, 20), r.Int("tag2", 0, 20), r.Int("last", 0, 20)
		r.On("pay_1").OnlyIf(Is(amount, 13)).Does(Copy(last, tag1)).Add()
		r.On("pay_2").OnlyIf(Is(amount, 13)).Does(Copy(last, tag2)).Add()
		return r
	}
	r := mk()
	a, err := r.absCheck(nil)
	if err != nil || !a.wfc || !absCommutes(r, a) {
		t.Fatalf("representative check over 0..3: %v; want a pass", err)
	}
	if _, _, err := mk().Build(); err == nil {
		t.Fatal("ordinary Build accepted the diverging events")
	}
	a13, err := r.absCheck([]int{13})
	if err != nil || absCommutes(r, a13) {
		t.Fatalf("with 13 declared the check should fail: %v", err)
	}
}

// The representative check alone passes on the gsm form of triangle over 0, 1, 2, though A
// and B diverge at (2, 3, 4) (triangle_passes, triangle_diverges).
func TestAbs_TriangleRepresentativesPass(t *testing.T) {
	r := NewRegistry("triangle")
	x, y, z := r.Int("x", 0, 5), r.Int("y", 0, 5), r.Int("z", 0, 5)
	guard := And(BelowVar(x, y), BelowVar(y, z), Lt(V(z), Add(V(x), V(y))))
	r.On("A").OnlyIf(guard).Does(Copy(x, y)).Add()
	r.On("B").OnlyIf(guard).Does(Copy(y, x)).Add()
	a, err := r.absCheck(nil)
	if err != nil || !reflect.DeepEqual(a.reps, []int{0, 1, 2}) || !absCommutes(r, a) {
		t.Fatalf("representative check over 0, 1, 2: %v; want a pass", err)
	}
	var ae *AbstractionError
	if _, _, err := r.Abstract().Build(); !errors.As(err, &ae) {
		t.Fatalf("Build: %v; want a refusal", err)
	}
}

// absVerdict is the outcome of a check: "pass", "wfc" or "cc".
func absVerdictOf(rep *Report, err error) string {
	switch {
	case err == nil:
		return "pass"
	case rep != nil && !rep.WFC:
		return "wfc"
	case rep != nil && rep.CCFailure != nil:
		return "cc"
	}
	return "error: " + err.Error()
}

// bruteVerdict decides WFC and CC1 at every valid state of r by enumeration: Build's own
// phases 1 and 2 (normal forms with the WFC check, step tables), then CC1 for every checked
// pair at every valid state, independently of abstraction.go.
func bruteVerdict(r *Registry) string {
	m, rep, err := r.build(false)
	if err != nil {
		if rep != nil && !rep.WFC {
			return "wfc"
		}
		return "error: " + err.Error()
	}
	for _, p := range r.ccPairs() {
		for s := range m.nf {
			if m.valid[s] && m.nf[s] == uint64(s) && m.step[p[1]][m.step[p[0]][s]] != m.step[p[0]][m.step[p[1]][s]] {
				return "cc"
			}
		}
	}
	return "pass"
}

// randomAbsRegistry builds a random registry in the comparison fragment over consts, with
// every variable over the same range [lo, hi] (so no write saturates).
func randomAbsRegistry(rng *rand.Rand, name string, consts []int, nv, lo, hi int) *Registry {
	r := NewRegistry(name)
	vars := make([]Var, nv)
	for i := range vars {
		vars[i] = r.Int(fmt.Sprintf("v%d", i), lo, hi)
	}
	leaf := func() Expr {
		if len(consts) > 0 && rng.Intn(3) == 0 {
			return Lit(consts[rng.Intn(len(consts))])
		}
		return V(vars[rng.Intn(nv)])
	}
	var pred func(depth int) Pred
	pred = func(depth int) Pred {
		if depth > 0 && rng.Intn(3) == 0 {
			switch rng.Intn(3) {
			case 0:
				return And(pred(depth-1), pred(depth-1))
			case 1:
				return Or(pred(depth-1), pred(depth-1))
			default:
				return Not(pred(depth - 1))
			}
		}
		ops := []func(a, b Expr) Pred{Le, Lt, Eq, Ge, Gt, Ne}
		return ops[rng.Intn(len(ops))](leaf(), leaf())
	}
	transform := func() Transform {
		var t Transform
		for k := 0; k < 1+rng.Intn(2); k++ {
			t = append(t, Set(vars[rng.Intn(nv)], leaf()))
		}
		return t
	}
	for k := 0; k < rng.Intn(3); k++ {
		r.DeclInvariant(fmt.Sprintf("inv%d", k), pred(2), transform())
	}
	for k := 0; k < 2+rng.Intn(2); k++ {
		if rng.Intn(2) == 0 {
			r.DeclEventGuarded(fmt.Sprintf("e%d", k), pred(1), transform())
		} else {
			r.DeclEvent(fmt.Sprintf("e%d", k), transform())
		}
	}
	if rng.Intn(4) == 0 && len(r.events) > 2 {
		r.Independent(r.events[0].name, r.events[1].name)
	}
	return r
}

// Differential: on random registries in the fragment, Build by abstraction reaches the
// outcome that enumeration reaches over a range containing every representative. By the
// abstraction theorems (term_abs, cc1_abs) both equal the outcome over all integers, since
// the range is closed under the rules (closure_ap, closure_rp) and contains every witness the
// representative check can report.
func TestAbs_DifferentialAgainstEnumeration(t *testing.T) {
	n := 300
	if testing.Short() {
		n = 60
	}
	constSets := [][]int{nil, {0}, {5}, {-1, 2}, {0, 3, 4}}
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		consts := constSets[rng.Intn(len(constSets))]
		nv := 1 + rng.Intn(3)
		reps, err := absReps(nv, consts)
		if err != nil {
			t.Fatal(err)
		}
		margin := 1 + rng.Intn(3)
		lo, hi := reps[0]-margin, reps[len(reps)-1]+margin
		seed := rng.Int63()
		mk := func() *Registry {
			return randomAbsRegistry(rand.New(rand.NewSource(seed)), fmt.Sprintf("abs_%d", i), consts, nv, lo, hi)
		}
		r := mk()
		m, rep, err := r.Abstract(consts...).Build()
		got := absVerdictOf(rep, err)
		want := bruteVerdict(mk())
		if got != want {
			t.Fatalf("case %d (constants %v, %d variables over %d..%d): abstraction %s, enumeration %s\n%s",
				i, consts, nv, lo, hi, got, want, rep)
		}
		counts[got]++
		if m == nil {
			continue
		}
		// On a pass, ordinary Build agrees, except that it also checks CC1 at the zero state,
		// which abstraction does not need (its machine normalizes the zero state first).
		if _, erep, err := mk().build(true); err != nil &&
			(erep == nil || erep.CCFailure == nil || erep.CCFailure.State.packed != 0) {
			t.Fatalf("case %d: abstraction passes, ordinary Build fails off the zero state: %v\n%s", i, err, erep)
		}
		// And the machine agrees with the enumerated machine on random runs.
		em, _, err := mk().build(false)
		if err != nil {
			t.Fatal(err)
		}
		for run := 0; run < 20; run++ {
			s := em.Normalize(State{packed: uint64(rng.Intn(len(em.nf))), vars: em.vars})
			if em.dom.notStateOf(s) != nil {
				continue
			}
			as := State{packed: s.packed, vars: m.vars}
			for k := 0; k < 5; k++ {
				e := r.events[rng.Intn(len(r.events))].name
				s, as = em.Apply(s, e), m.Apply(as, e)
				if s.packed != as.packed {
					t.Fatalf("case %d: runtime differs: %s vs %s", i, s, as)
				}
			}
		}
	}
	t.Logf("outcomes: %v", counts)
	if counts["pass"] == 0 || counts["cc"] == 0 {
		t.Fatalf("the random registries do not exercise both outcomes: %v", counts)
	}
}
