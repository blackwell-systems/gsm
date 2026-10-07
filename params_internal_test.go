package gsm

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// dedupInts returns the distinct values of a sorted slice.
func dedupInts(xs []int) []int {
	var out []int
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// Internal tests for events with parameters (params.go): a parameterized event against the
// same instances declared one by one, and the abstraction check with parameters against
// enumeration of every instance.

// randomParamRegistry builds a random registry with nv variables over [lo, hi] and nfam
// parameterized events (each with 1 or 2 parameters over [plo, phi]) plus a plain event. With
// manual set, each family is declared instead as its instances, one DeclEventGuarded per
// value, named as the family would name them. arith allows Add and Sub (outside the
// comparison fragment); consts are the literals the rules may use.
func randomParamRegistry(seed int64, name string, nv, lo, hi, plo, phi int, consts []int, arith, manual bool) *Registry {
	rng := rand.New(rand.NewSource(seed))
	r := NewRegistry(name)
	vars := make([]Var, nv)
	for i := range vars {
		vars[i] = r.Int(fmt.Sprintf("v%d", i), lo, hi)
	}
	var params []string
	leaf := func() Expr {
		switch k := rng.Intn(4); {
		case k == 0 && len(consts) > 0:
			return Lit(consts[rng.Intn(len(consts))])
		case k == 1 && len(params) > 0:
			return Arg(params[rng.Intn(len(params))])
		}
		return V(vars[rng.Intn(nv)])
	}
	expr := func() Expr {
		if arith && rng.Intn(3) == 0 {
			if rng.Intn(2) == 0 {
				return Add(leaf(), leaf())
			}
			return Sub(leaf(), leaf())
		}
		return leaf()
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
		return ops[rng.Intn(len(ops))](expr(), expr())
	}
	transform := func() Transform {
		var t Transform
		for k := 0; k < 1+rng.Intn(2); k++ {
			t = append(t, Set(vars[rng.Intn(nv)], expr()))
		}
		return t
	}
	for k := 0; k < rng.Intn(3); k++ {
		r.DeclInvariant(fmt.Sprintf("inv%d", k), pred(2), transform())
	}
	nfam := 1 + rng.Intn(2)
	var names []string
	for f := 0; f < nfam; f++ {
		fname := fmt.Sprintf("f%d", f)
		names = append(names, fname)
		params = []string{"a"}
		if rng.Intn(3) == 0 {
			params = append(params, "b")
		}
		var guard Pred
		if rng.Intn(2) == 0 {
			guard = pred(1)
		}
		eff := transform()
		if !manual {
			b := r.On(fname)
			for _, p := range params {
				b.Param(p, plo, phi)
			}
			b.Does(eff)
			if guard != nil {
				b.OnlyIf(guard)
			}
			b.Add()
		} else {
			dom := make([][]int, len(params))
			for i := range dom {
				for v := plo; v <= phi; v++ {
					dom[i] = append(dom[i], v)
				}
			}
			tuples(dom, func(args []int) {
				vals := map[string]int{}
				for i, p := range params {
					vals[p] = args[i]
				}
				name := Instance(fname, args...)
				if guard != nil {
					r.DeclEventGuarded(name, substPred(guard, vals), substTransform(eff, vals))
				} else {
					r.DeclEvent(name, substTransform(eff, vals))
				}
			})
		}
		params = nil
	}
	r.DeclEvent("plain", transform())
	names = append(names, "plain")
	if rng.Intn(4) == 0 && !manual {
		a, b := names[rng.Intn(len(names))], names[rng.Intn(len(names))]
		r.Independent(a, b)
	}
	return r
}

// manualIndependent mirrors, on the manual registry, the Independent declaration of the
// parameterized one: each declared pair by instance name.
func manualIndependent(param, manual *Registry) {
	if param.allIndependent {
		return
	}
	manual.allIndependent = false
	for _, p := range param.independent {
		manual.Independent(param.events[p[0]].name, param.events[p[1]].name)
	}
}

// A parameterized event gives the same result as declaring each instance as its own event:
// the same outcome, witness, obligations and tables, over random registries (arithmetic on
// parameters included).
func TestParams_RandomSameAsManualInstances(t *testing.T) {
	n := 200
	if testing.Short() {
		n = 40
	}
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		seed := int64(1000 + i)
		p := randomParamRegistry(seed, "p", 2, 0, 4, 0, 2, []int{1, 3}, true, false)
		q := randomParamRegistry(seed, "p", 2, 0, 4, 0, 2, []int{1, 3}, true, true)
		manualIndependent(p, q)
		if len(p.events) != len(q.events) {
			t.Fatalf("case %d: %d instances, %d manual events", i, len(p.events), len(q.events))
		}
		for k := range p.events {
			if p.events[k].name != q.events[k].name {
				t.Fatalf("case %d: event %d is %q, manual %q", i, k, p.events[k].name, q.events[k].name)
			}
		}
		pm, prep, perr := p.Build()
		qm, qrep, qerr := q.Build()
		got, want := absVerdictOf(prep, perr), absVerdictOf(qrep, qerr)
		if got != want {
			t.Fatalf("case %d: parameterized %s, manual %s", i, got, want)
		}
		counts[got]++
		if prep == nil || qrep == nil {
			continue
		}
		if !reflect.DeepEqual(prep.NotIdempotent, qrep.NotIdempotent) || prep.PairsTotal != qrep.PairsTotal ||
			len(prep.CausalOrderRequired) != len(qrep.CausalOrderRequired) {
			t.Fatalf("case %d: reports differ:\n%s\n%s", i, prep, qrep)
		}
		if (prep.CCFailure == nil) != (qrep.CCFailure == nil) || (prep.CCFailure != nil &&
			(prep.CCFailure.Event1 != qrep.CCFailure.Event1 || prep.CCFailure.Event2 != qrep.CCFailure.Event2 ||
				prep.CCFailure.State.packed != qrep.CCFailure.State.packed)) {
			t.Fatalf("case %d: witnesses differ: %+v vs %+v", i, prep.CCFailure, qrep.CCFailure)
		}
		if pm != nil && !reflect.DeepEqual(pm.step, qm.step) {
			t.Fatalf("case %d: step tables differ", i)
		}
	}
	t.Logf("outcomes: %v", counts)
	if counts["pass"] == 0 || counts["cc"] == 0 {
		t.Fatalf("the random registries do not exercise both outcomes: %v", counts)
	}
}

// Differential, with parameters: on random registries in the comparison fragment, Build by
// abstraction (parameters over representative values, cutoff n + 2m) reaches the outcome
// that enumerating every instance reaches, over variable and parameter ranges that contain
// every representative. By cc1_valid_abs, idem_valid_abs and term_abs both equal the outcome
// over all integers: the range is closed under the rules and contains every witness the
// representative check can report.
func TestParamsAbstract_DifferentialAgainstInstances(t *testing.T) {
	n := 200
	if testing.Short() {
		n = 40
	}
	constSets := [][]int{nil, {0}, {3}}
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		consts := constSets[rng.Intn(len(constSets))]
		nv := 1 + rng.Intn(2)
		seed := rng.Int63()
		// Parameters over a small range; the check's constants are the declared ones and the
		// parameters' bounds. The cutoff depends on m, which the generator decides; take the
		// widest (m = 2), and give the variables a range containing every representative.
		plo := rng.Intn(3) - 1
		phi := plo + 1 + rng.Intn(3)
		all := append(append([]int(nil), consts...), plo, phi)
		sort.Ints(all)
		reps, err := absReps(nv+4, dedupInts(all))
		if err != nil {
			t.Fatal(err)
		}
		lo, hi := reps[0]-1, reps[len(reps)-1]+1
		mk := func() *Registry {
			return randomParamRegistry(seed, fmt.Sprintf("absp_%d", i), nv, lo, hi, plo, phi, consts, false, false)
		}
		r := mk()
		m, rep, err := r.Abstract(consts...).Build()
		got := absVerdictOf(rep, err)
		if strings.HasPrefix(got, "error") {
			t.Fatalf("case %d: %s", i, got)
		}
		e := mk()
		want := bruteVerdict(e)
		if got != want {
			t.Fatalf("case %d (constants %v, %d variables over %d..%d, m = %d): abstraction %s, enumeration %s\n%s",
				i, consts, nv, lo, hi, rep.Abstraction.Params, got, want, rep)
		}
		counts[got]++
		if m == nil {
			continue
		}
		// NotIdempotent: a family is listed by abstraction iff some instance is listed by
		// enumeration (idem_valid_abs), and the enumerated machine agrees with the abstract
		// one at run time.
		_, erep, eerr := e.build(true)
		if erep != nil && (eerr == nil || erep.CCFailure == nil || erep.CCFailure.State.packed == 0) {
			listed := map[string]bool{}
			for _, name := range erep.NotIdempotent {
				for _, f := range e.families {
					if strings.HasPrefix(name, f.name+"(") {
						listed[f.signature()] = true
					}
				}
				if name == "plain" {
					listed[name] = true
				}
			}
			absListed := map[string]bool{}
			for _, name := range rep.NotIdempotent {
				absListed[name] = true
			}
			if eerr == nil && !reflect.DeepEqual(listed, absListed) {
				t.Fatalf("case %d: NotIdempotent by abstraction %v, by enumeration %v", i, rep.NotIdempotent, erep.NotIdempotent)
			}
		}
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
				ev := e.events[rng.Intn(len(e.events))].name
				s, as = em.Apply(s, ev), m.Apply(as, ev)
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

// The regime summary of a parameterized machine names the families and cites only
// allowlisted theorems, by enumeration and by abstraction.
func TestParams_RegimeShape(t *testing.T) {
	r := NewRegistry("ledger")
	dep, req := r.Int("deposited", 0, 6), r.Int("requested", 0, 6)
	r.On("deposit").Param("amount", 1, 2).Does(IncByArg(dep, "amount")).Add()
	r.On("withdraw").Param("amount", 1, 2).Does(IncByArg(req, "amount")).Add()
	_, rep, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	checkRegimeShape(t, rep.Regime)
	if !strings.Contains(rep.Regime.String(), "events with parameters deposit(amount 1..2), withdraw(amount 1..2), "+
		"each instance checked as its own event") {
		t.Errorf("regime:\n%s", rep.Regime)
	}
	a := NewRegistry("register")
	v := a.Int("value", 0, 1000)
	a.On("raise").Param("to", 0, 1000).OnlyIf(Lt(V(v), Arg("to"))).Does(SetToArg(v, "to")).Add()
	_, rep, err = a.Abstract().Build()
	if err != nil {
		t.Fatal(err)
	}
	checkRegimeShape(t, rep.Regime)
	if !strings.Contains(rep.Regime.String(), "checked over representative parameter values") {
		t.Errorf("regime:\n%s", rep.Regime)
	}
}

// A registry whose instances are too many for the global check is refused up front.
func TestParams_GlobalBudget(t *testing.T) {
	r := NewRegistry("big")
	x := r.Int("x", 0, 1<<16-1)
	r.Int("y", 0, 7)
	for k := 0; k < 3; k++ {
		r.On(fmt.Sprintf("set%d", k)).Param("v", 0, 1000).Does(SetToArg(x, "v")).Add()
	}
	_, _, err := r.build(true)
	if err == nil || !strings.Contains(err.Error(), "declare Abstract") {
		t.Fatalf("err = %v", err)
	}
}

func TestParams_GroupEvents(t *testing.T) {
	fams := []EventFamily{{Name: "w", Params: []Param{{"a", 1, 3}}, Instances: 3},
		{Name: "m", Params: []Param{{"x", 0, 1}, {"y", 0, 1}}, Instances: 4},
		{Name: "r", Params: []Param{{"t", 0, 9}}, Representative: true, NotIdempotent: []string{"r(4)"}}}
	got := groupEvents([]string{"plain", "w(1)", "w(2)", "w(3)", "m(0,1)", "r(t)"}, fams)
	want := "plain, w(a) (every value), m(x, y) at (0,1), r(t) (every value; witness r(4))"
	if got != want {
		t.Errorf("groupEvents = %q, want %q", got, want)
	}
	if args, ok := parseArgs("1,-2", 2); !ok || !reflect.DeepEqual(args, []int{1, -2}) {
		t.Errorf("parseArgs: %v %v", args, ok)
	}
	for _, bad := range []string{"1", "1,", "01,2", "a,b", "1, 2"} {
		if _, ok := parseArgs(bad, 2); ok {
			t.Errorf("parseArgs(%q) accepted", bad)
		}
	}
}
