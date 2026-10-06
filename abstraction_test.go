package gsm_test

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// Tests for verification by abstraction (Registry.Abstract, roadmap item 1b), against
// normalization-confluence coq/AbstractionCutoff.v.

// cappedInventory declares the capped inventory of AbstractionCutoff.v (capped_*) in gsm's
// model, where events carry no parameters: Restock(level) becomes two restock events, each
// copying a pending shipment level held in the state (a, b). Restock raises stock to the
// level (stock := max(stock, level)); the invariant stock <= 5 is repaired by clamping to the
// declared cap. Three variables and the constant 5 give reps 3 [5] = {2, ..., 8}: the seven
// representatives of capped_reps.
func cappedInventory(lo, hi int) (*gsm.Registry, gsm.Var, gsm.Var, gsm.Var) {
	r := gsm.NewRegistry("inventory")
	stock := r.Int("stock", lo, hi)
	a := r.Int("a", lo, hi)
	b := r.Int("b", lo, hi)
	r.Rule("cap").Require(gsm.AtMost(stock, 5)).RepairWith(gsm.SetTo(stock, 5)).Add()
	r.On("restock_a").OnlyIf(gsm.BelowVar(stock, a)).Does(gsm.Copy(stock, a)).Add()
	r.On("restock_b").OnlyIf(gsm.BelowVar(stock, b)).Does(gsm.Copy(stock, b)).Add()
	return r, stock, a, b
}

func TestAbstraction_CappedInventory(t *testing.T) {
	r, stock, a, b := cappedInventory(0, 1_000_000)
	m, rep, err := r.Abstract(5).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	want := &gsm.AbstractionReduction{
		Over:            []string{"stock", "a", "b"},
		Constants:       []int{5},
		Cutoff:          3,
		Representatives: []int{2, 3, 4, 5, 6, 7, 8},
		States:          343,
	}
	if !reflect.DeepEqual(rep.Abstraction, want) {
		t.Fatalf("Report.Abstraction = %+v, want %+v", rep.Abstraction, want)
	}
	line := "Verified by abstraction over stock, a, b (rules compare values only; constants {5}; 7 representatives)"
	if got := rep.Abstraction.String(); got != line {
		t.Fatalf("Abstraction.String() = %q, want %q", got, line)
	}
	out := rep.String()
	for _, s := range []string{line, "Convergence: GUARANTEED", "States: 343 representative",
		"Assurance: representative tables certified by the verified table oracle"} {
		if !strings.Contains(out, s) {
			t.Errorf("report lacks %q:\n%s", s, out)
		}
	}
	if rep.Assurance != gsm.AssuranceOracleRepresentatives || !rep.WFC || !rep.CC || rep.MaxRepairLen != 1 {
		t.Fatalf("report: %+v", rep)
	}

	// Values far beyond anything enumerable: restock to 900000, and the cap repair fires
	// (capped_repair_fires).
	s := m.NewState().SetInt(a, 900_000).SetInt(b, 700_000)
	s1 := m.Apply(m.Apply(s, "restock_a"), "restock_b")
	s2 := m.Apply(m.Apply(s, "restock_b"), "restock_a")
	if s1.ID() != s2.ID() || s1.GetInt(stock) != 5 {
		t.Fatalf("orders disagree or the cap did not repair: %s vs %s", s1, s2)
	}
	if !m.IsValid(s1) || m.IsValid(m.NewState().SetInt(stock, 999_999)) {
		t.Fatal("IsValid wrong")
	}
	if got := m.Normalize(m.NewState().SetInt(stock, 999_999)).GetInt(stock); got != 5 {
		t.Fatalf("Normalize: stock %d, want 5", got)
	}
}

// The abstraction's result agrees with a brute-force check over a large bounded range: every
// pair of events commutes at every valid state with each variable in 0..60 (226,981 states),
// on the machine Build returned by abstraction; and ordinary Build over 0..20 agrees.
func TestAbstraction_CappedAgreesWithBruteForce(t *testing.T) {
	r, stock, a, b := cappedInventory(0, 1_000_000)
	m, _, err := r.Abstract(5).Build()
	if err != nil {
		t.Fatal(err)
	}
	events := m.Events()
	const hi = 60
	for x := 0; x <= hi; x++ {
		for y := 0; y <= hi; y++ {
			for z := 0; z <= hi; z++ {
				s := m.NewState().SetInt(stock, x).SetInt(a, y).SetInt(b, z)
				if !m.IsValid(s) {
					continue
				}
				for i := range events {
					for j := i + 1; j < len(events); j++ {
						l := m.Apply(m.Apply(s, events[i]), events[j])
						rr := m.Apply(m.Apply(s, events[j]), events[i])
						if l.ID() != rr.ID() {
							t.Fatalf("(%s, %s) diverge from %s: %s vs %s", events[i], events[j], s, l, rr)
						}
					}
				}
			}
		}
	}
	small, _, _, _ := cappedInventory(0, 20)
	if _, rep, err := small.Build(); err != nil {
		t.Fatalf("ordinary Build over 0..20: %v\n%s", err, rep)
	}
}

// A comparison example with two constants: a bid counts only once it reaches the reserve
// price 100, and the highest bid so far is kept (0 for none). The representatives below the
// least constant (-1, -2, -3) lie outside the declared ranges; a pass covers the ranges anyway.
func TestAbstraction_ReserveAuction(t *testing.T) {
	r := gsm.NewRegistry("auction")
	highest := r.Int("highest", 0, 1_000_000)
	bidA := r.Int("bid_a", 0, 1_000_000)
	bidB := r.Int("bid_b", 0, 1_000_000)
	r.Rule("reserve").Require(gsm.Or(gsm.Is(highest, 0), gsm.AtLeast(highest, 100))).
		RepairWith(gsm.SetTo(highest, 0)).Add()
	r.On("place_a").OnlyIf(gsm.BelowVar(highest, bidA)).Does(gsm.Copy(highest, bidA)).Add()
	r.On("place_b").OnlyIf(gsm.BelowVar(highest, bidB)).Does(gsm.Copy(highest, bidB)).Add()
	m, rep, err := r.Abstract(0, 100).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if got := rep.Abstraction.Representatives; !reflect.DeepEqual(got, []int{-3, -2, -1, 0, 1, 2, 3, 100, 101, 102, 103}) {
		t.Fatalf("representatives %v", got)
	}
	s := m.NewState().SetInt(bidA, 50).SetInt(bidB, 250_000)
	for _, order := range [][]string{{"place_a", "place_b"}, {"place_b", "place_a"}} {
		got := m.Apply(m.Apply(s, order[0]), order[1])
		if got.GetInt(highest) != 250_000 {
			t.Fatalf("%v: %s", order, got)
		}
	}
	if got := m.Apply(m.NewState().SetInt(bidA, 99), "place_a").GetInt(highest); got != 0 {
		t.Fatalf("a bid below the reserve counted: %d", got)
	}
}

// copy_tight in gsm terms: x := y and y := x diverge exactly when x != y, which the check over
// reps 2 [] = {0, 1} finds.
func TestAbstraction_CopyConflictFails(t *testing.T) {
	r2 := gsm.NewRegistry("copy")
	x2 := r2.Int("x", 0, 1<<20)
	y2 := r2.Int("y", 0, 1<<20)
	r2.On("x_gets_y").Does(gsm.Copy(x2, y2)).Add()
	r2.On("y_gets_x").Does(gsm.Copy(y2, x2)).Add()
	_, rep, err := r2.Abstract().Build()
	if err == nil || rep.CC || rep.CCFailure == nil {
		t.Fatalf("Build accepted diverging copies: %v\n%s", err, rep)
	}
	w := rep.CCFailure.Abstract
	if w == nil || !w.InRange || w.State[0] == w.State[1] {
		t.Fatalf("witness %+v", w)
	}
	if rep.CCFailure.State.GetInt(x2) != w.State[0] || rep.CCFailure.State.GetInt(y2) != w.State[1] {
		t.Fatalf("CCFailure.State %s does not match the witness %v", rep.CCFailure.State, w.State)
	}
	if !strings.Contains(rep.String(), "CC (Compensation Commutativity): FAIL") {
		t.Fatalf("report:\n%s", rep)
	}
}

// exact13 (exact13_passes, exact13_diverges, exact13_refused, exact13_declared) in gsm terms:
// Pay(13, tag) becomes two events that copy a tag when amount = 13.
func exact13Registry() (*gsm.Registry, gsm.Var, gsm.Var, gsm.Var, gsm.Var) {
	r := gsm.NewRegistry("pay")
	amount := r.Int("amount", 0, 20)
	tag1 := r.Int("tag1", 0, 20)
	tag2 := r.Int("tag2", 0, 20)
	last := r.Int("last", 0, 20)
	r.On("pay_1").OnlyIf(gsm.Is(amount, 13)).Does(gsm.Copy(last, tag1)).Add()
	r.On("pay_2").OnlyIf(gsm.Is(amount, 13)).Does(gsm.Copy(last, tag2)).Add()
	return r, amount, tag1, tag2, last
}

func TestAbstraction_UndeclaredLiteralRefused(t *testing.T) {
	r, _, _, _, _ := exact13Registry()
	m, rep, err := r.Abstract().Build()
	var ae *gsm.AbstractionError
	if m != nil || !errors.As(err, &ae) {
		t.Fatalf("Build = %v, %v; want an *AbstractionError", m, err)
	}
	if ae.Rule != `event "pay_1"` || !strings.Contains(ae.Reason, "literal 13") ||
		!strings.Contains(ae.Reason, "exact13_diverges") {
		t.Fatalf("refusal %+v", ae)
	}
	if rep == nil || rep.AbstractionRefused != err.Error() || rep.WFC || rep.CC || rep.Abstraction != nil {
		t.Fatalf("report %+v", rep)
	}
	if !strings.Contains(rep.String(), "Abstraction: REFUSED") {
		t.Fatalf("report:\n%s", rep)
	}

	// The refusal is required: over the real domain the two events diverge at amount = 13
	// (ordinary Build finds it), though the representatives 0..3 never test 13
	// (TestAbstraction_Exact13RepresentativesPass, internal).
	plain, amount, _, _, _ := exact13Registry()
	_, prep, perr := plain.Build()
	if perr == nil || prep.CCFailure == nil || prep.CCFailure.State.GetInt(amount) != 13 {
		t.Fatalf("ordinary Build: %v\n%s", perr, prep)
	}

	// With 13 declared the rules are in the fragment and the check fails, as it should.
	declared, _, _, _, _ := exact13Registry()
	_, drep, derr := declared.Abstract(13).Build()
	if derr == nil || drep.CCFailure == nil || drep.CCFailure.Abstract.State[0] != 13 {
		t.Fatalf("Abstract(13): %v\n%s", derr, drep)
	}
}

// triangle (triangle_passes, triangle_diverges, triangle_refused) in gsm terms.
func triangleRegistry() *gsm.Registry {
	r := gsm.NewRegistry("triangle")
	x, y, z := r.Int("x", 0, 5), r.Int("y", 0, 5), r.Int("z", 0, 5)
	guard := gsm.And(gsm.BelowVar(x, y), gsm.BelowVar(y, z), gsm.Lt(gsm.V(z), gsm.Add(gsm.V(x), gsm.V(y))))
	r.On("A").OnlyIf(guard).Does(gsm.Copy(x, y)).Add()
	r.On("B").OnlyIf(guard).Does(gsm.Copy(y, x)).Add()
	return r
}

func TestAbstraction_ArithmeticGuardRefused(t *testing.T) {
	_, rep, err := triangleRegistry().Abstract().Build()
	var ae *gsm.AbstractionError
	if !errors.As(err, &ae) || ae.Rule != `event "A"` || !strings.Contains(ae.Reason, "computes x + y") ||
		!strings.Contains(ae.Reason, "triangle_diverges") || rep.AbstractionRefused == "" {
		t.Fatalf("Build: %v", err)
	}
	// Over the real domain A and B diverge at (2, 3, 4).
	_, prep, perr := triangleRegistry().Build()
	if perr == nil || prep.CCFailure == nil || prep.CCFailure.State.String() != "{x=2, y=3, z=4}" {
		t.Fatalf("ordinary Build: %v\n%s", perr, prep)
	}
	// Arithmetic in an effect is refused the same way (a wallet: the linear fragment).
	w := gsm.NewRegistry("wallet")
	bal := w.Int("balance", -1000, 1000)
	w.On("deposit").Does(gsm.IncBy(bal, 5)).Add()
	if _, _, err := w.Abstract().Build(); !errors.As(err, &ae) || !strings.Contains(ae.Reason, "computes balance + 5") {
		t.Fatalf("wallet: %v", err)
	}
}

func TestAbstraction_ClosureRulesRefused(t *testing.T) {
	cases := map[string]func(r *gsm.Registry, x gsm.Var) string{
		"event closure": func(r *gsm.Registry, x gsm.Var) string {
			r.Event("bump").Writes(x).Apply(func(s gsm.State) gsm.State { return s }).Add()
			return `event "bump"`
		},
		"invariant closure": func(r *gsm.Registry, x gsm.Var) string {
			r.Invariant("cap").Watches(x).Holds(func(s gsm.State) bool { return s.GetInt(x) <= 5 }).
				Repair(func(s gsm.State) gsm.State { return s.SetInt(x, 5) }).Add()
			return `invariant "cap"`
		},
		"closure guard on a combinator effect": func(r *gsm.Registry, x gsm.Var) string {
			r.Event("guarded").Writes(x).Guard(func(gsm.State) bool { return true }).
				Apply(func(s gsm.State) gsm.State { return s }).Add()
			return `event "guarded"`
		},
		"invariant without a repair transform": func(r *gsm.Registry, x gsm.Var) string {
			r.Rule("cap").Require(gsm.AtMost(x, 5)).Add()
			return `invariant "cap"`
		},
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			r := gsm.NewRegistry("c")
			x := r.Int("x", 0, 100)
			rule := declare(r, x)
			_, _, err := r.Abstract(5).Build()
			var ae *gsm.AbstractionError
			if !errors.As(err, &ae) || ae.Rule != rule || !strings.Contains(ae.Reason, "Go closure") {
				t.Fatalf("Build: %v", err)
			}
		})
	}
	for _, kind := range []string{"Bool", "Enum"} {
		r := gsm.NewRegistry("k")
		r.Int("x", 0, 100)
		if kind == "Bool" {
			r.Bool("flag")
		} else {
			r.Enum("status", "a", "b")
		}
		_, _, err := r.Abstract().Build()
		var ae *gsm.AbstractionError
		if !errors.As(err, &ae) || !strings.HasPrefix(ae.Rule, "variable ") || !strings.Contains(ae.Reason, "integer variables only") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
}

func TestAbstraction_BoundsAndOverflow(t *testing.T) {
	var ae *gsm.AbstractionError
	refused := func(t *testing.T, r *gsm.Registry, consts []int, want string) {
		t.Helper()
		m, rep, err := r.Abstract(consts...).Build()
		if m != nil || !errors.As(err, &ae) || !strings.Contains(err.Error(), want) {
			t.Fatalf("Build = %v, want a refusal containing %q", err, want)
		}
		if rep == nil || rep.AbstractionRefused == "" {
			t.Fatalf("report %+v", rep)
		}
	}
	t.Run("copy into a narrower range", func(t *testing.T) {
		r := gsm.NewRegistry("narrow")
		stock := r.Int("stock", 0, 10)
		incoming := r.Int("incoming", 0, 100)
		r.On("receive").Does(gsm.Copy(stock, incoming)).Add()
		refused(t, r, nil, `copies "incoming" (0..100) into "stock" (0..10)`)
	})
	t.Run("constant outside the target range", func(t *testing.T) {
		r := gsm.NewRegistry("const")
		stock := r.Int("stock", 0, 10)
		r.On("fill").Does(gsm.SetTo(stock, 50)).Add()
		refused(t, r, []int{50}, `writes 50 into "stock" (0..10)`)
	})
	t.Run("more than 64 bits", func(t *testing.T) {
		r := gsm.NewRegistry("wide")
		r.Int("x", 0, 1<<40)
		r.Int("y", 0, 1<<40)
		refused(t, r, nil, "need 82 bits")
	})
	t.Run("representatives overflow int", func(t *testing.T) {
		r := gsm.NewRegistry("edge")
		x := r.Int("x", 0, 10)
		r.Rule("lim").Require(gsm.AtMost(x, math.MaxInt)).RepairWith(gsm.SetTo(x, 0)).Add()
		refused(t, r, []int{0, math.MaxInt}, "would overflow")
	})
	t.Run("representative domain too large", func(t *testing.T) {
		r := gsm.NewRegistry("many")
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			r.Int(n, 0, 1000)
		}
		refused(t, r, []int{10, 20, 30, 40}, "limit")
	})

	// A failure at a representative outside the declared ranges cannot be certified away:
	// over the integers the events diverge at x = -1, but no state of the machine (x >= 0)
	// enables them, so the machine as declared converges (ordinary Build agrees).
	outside := func() (*gsm.Registry, gsm.Var) {
		r := gsm.NewRegistry("outside")
		x, y := r.Int("x", 0, 10), r.Int("y", 0, 10)
		r.On("A").OnlyIf(gsm.Below(x, 0)).Does(gsm.Copy(x, y)).Add()
		r.On("B").OnlyIf(gsm.Below(x, 0)).Does(gsm.Copy(y, x)).Add()
		return r, x
	}
	r, _ := outside()
	_, rep, err := r.Abstract(0).Build()
	if err == nil || !strings.Contains(err.Error(), "outside the declared ranges") || rep.CCFailure == nil {
		t.Fatalf("Build: %v", err)
	}
	w := rep.CCFailure.Abstract
	if w == nil || w.InRange || w.State[0] >= 0 || rep.CCFailure.State.ID() != 0 {
		t.Fatalf("witness %+v, state %s", w, rep.CCFailure.State)
	}
	if !strings.Contains(rep.String(), "(outside the declared ranges)") {
		t.Fatalf("report:\n%s", rep)
	}
	plain, _ := outside()
	if _, _, err := plain.Build(); err != nil {
		t.Fatalf("ordinary Build: %v", err)
	}
}

// A machine verified by abstraction normalizes every invalid input before an event, the zero
// state included: abstraction certifies order independence from the valid states only.
func TestAbstraction_ApplyNormalizesTheZeroState(t *testing.T) {
	r := gsm.NewRegistry("floor")
	level := r.Int("level", 0, 1<<30)
	target := r.Int("target", 0, 1<<30)
	r.Rule("floor").Require(gsm.AtLeast(level, 3)).RepairWith(gsm.SetTo(level, 3)).Add()
	r.On("raise").OnlyIf(gsm.BelowVar(level, target)).Does(gsm.Copy(level, target)).Add()
	m, rep, err := r.Abstract(3).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	zero := m.NewState()
	if m.IsValid(zero) {
		t.Fatal("the zero state should be invalid")
	}
	if got := m.Apply(zero, "raise").GetInt(level); got != 3 {
		t.Fatalf("Apply from the zero state: level %d, want 3 (normalized first)", got)
	}
}

func TestAbstraction_DeclaredPairs(t *testing.T) {
	r := gsm.NewRegistry("declared")
	x := r.Int("x", 0, 1<<20)
	y := r.Int("y", 0, 1<<20)
	z := r.Int("z", 0, 1<<20)
	r.On("x_gets_y").Does(gsm.Copy(x, y)).Add()
	r.On("y_gets_x").Does(gsm.Copy(y, x)).Add()
	r.On("z_gets_z").Does(gsm.Copy(z, z)).Add()
	r.Independent("x_gets_y", "z_gets_z").Independent("y_gets_x", "z_gets_z")
	_, rep, err := r.Abstract().Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.PairsTotal != 2 || rep.PairsUndeclared != 1 || len(rep.CausalOrderRequired) != 1 {
		t.Fatalf("report: %+v", rep)
	}
	f := rep.CausalOrderRequired[0]
	if f.Event1 != "x_gets_y" || f.Event2 != "y_gets_x" || !f.Abstract.InRange {
		t.Fatalf("causal pair %+v", f)
	}
	if !strings.Contains(rep.String(), "GUARANTEED under causal delivery of the 1 undeclared pair(s)") {
		t.Fatalf("report:\n%s", rep)
	}
}

// A collection whose template is verified by abstraction: symmetry and abstraction compose.
func TestAbstraction_CollectionTemplate(t *testing.T) {
	item, stock, a, _ := cappedInventory(0, 1_000_000)
	item.Abstract(5)
	type sku string
	c, rep, err := gsm.NewCollection[sku]("SKU", item).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Symmetry == nil || rep.Abstraction == nil {
		t.Fatalf("report %+v", rep)
	}
	out := rep.String()
	for _, s := range []string{
		"Verified by symmetry over SKU (items independent; cutoff 1)",
		"Verified by abstraction over stock, a, b (rules compare values only; constants {5}; 7 representatives)",
		"Delivery: exactly once per SKU for every event",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("report lacks %q:\n%s", s, out)
		}
	}
	s := c.NewState()
	c.Apply(s, "w1", "restock_a")
	if got := s.Item("w1").GetInt(stock); got != 0 || s.Len() != 1 {
		t.Fatalf("w1 stock %d, %d keys", got, s.Len())
	}
	if got := c.Item().Apply(c.Item().NewState().SetInt(a, 400_000), "restock_a").GetInt(stock); got != 5 {
		t.Fatalf("template machine: stock %d, want 5", got)
	}
}

func TestAbstraction_FederationRefuses(t *testing.T) {
	r, _, _, _ := cappedInventory(0, 1000)
	r.Abstract(5)
	f := gsm.NewFederation("f").Add(r)
	if _, _, err := f.Build(); err == nil || !strings.Contains(err.Error(), "declares Abstract") {
		t.Fatalf("Federation.Build: %v", err)
	}
}

// Random runs: the machine Build returns by abstraction agrees with the enumerated machine of
// the same rules over a small range, event by event.
func TestAbstraction_RuntimeAgreesWithEnumeration(t *testing.T) {
	big, bs, ba, bb := cappedInventory(0, 1_000_000)
	am, _, err := big.Abstract(5).Build()
	if err != nil {
		t.Fatal(err)
	}
	small, ss, sa, sb := cappedInventory(0, 20)
	em, _, err := small.Build()
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	events := em.Events()
	for run := 0; run < 200; run++ {
		x, y, z := rng.Intn(21), rng.Intn(21), rng.Intn(21)
		as := am.NewState().SetInt(bs, x).SetInt(ba, y).SetInt(bb, z)
		es := em.NewState().SetInt(ss, x).SetInt(sa, y).SetInt(sb, z)
		as, es = am.Normalize(as), em.Normalize(es)
		for k := 0; k < 6; k++ {
			e := events[rng.Intn(len(events))]
			as, es = am.Apply(as, e), em.Apply(es, e)
			if as.GetInt(bs) != es.GetInt(ss) || as.GetInt(ba) != es.GetInt(sa) || as.GetInt(bb) != es.GetInt(sb) {
				t.Fatalf("run %d: %s vs %s", run, as, es)
			}
		}
	}
}
