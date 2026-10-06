package gsm

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// Compositional checking by default (roadmap item 1c; normalization-confluence
// coq/CompositionalCheck.v). Build checks a registry too large to enumerate per footprint
// component when its rules are combinators; these tests pin the path it takes, the
// report, the refusals and fallbacks, and that the per-component check and the global
// check agree wherever both run.

// store is the theory's shop (shop_converges, shop_cost) with wider ranges: orders
// (shipped, paid, charged) and inventory (stock, reserved, backordered). The two components
// share no variable. With wide set, the inventory counters run 0..255 and the whole
// machine has 2^23 states, too many for Build to enumerate; without it, 0..3, and Build
// enumerates it.
func store(wide bool) *Registry {
	r := NewRegistry("store")
	top := 3
	if wide {
		top = 255
	}
	shipped := r.Bool("shipped")
	paid := r.Bool("paid")
	charged := r.Int("charged", 0, 15)
	stock := r.Int("stock", 0, top)
	reserved := r.Int("reserved", 0, top)
	backordered := r.Bool("backordered")

	// Orders: shipping unpaid is repaired by charging, and a charge marks the order paid.
	r.Rule("shipped_needs_payment").Require(Or(Is(shipped, 0), Is(paid, 1))).RepairWith(Raise(paid)).Add()
	r.Rule("charge_marks_paid").Require(Or(Is(paid, 1), AtMost(charged, 0))).RepairWith(Raise(paid)).Add()
	r.On("pay").Does(Raise(paid)).Add()
	r.On("ship").Does(Raise(shipped)).Add()
	r.On("charge").OnlyIf(Below(charged, 15)).Does(Inc(charged)).Add()

	// Inventory: the backorder flag is reserved > stock, recomputed by repair.
	r.Rule("flag_when_short").Require(Or(Is(backordered, 1), AtMostVar(reserved, stock))).RepairWith(Raise(backordered)).Add()
	r.Rule("clear_when_covered").Require(Or(Is(backordered, 0), AboveVar(reserved, stock))).RepairWith(Lower(backordered)).Add()
	r.On("reserve").OnlyIf(Below(reserved, top)).Does(Inc(reserved)).Add()
	r.On("restock").OnlyIf(Below(stock, top)).Does(Inc(stock)).Add()
	return r
}

func TestBuildDefault_OrdersAndInventoryPerComponent(t *testing.T) {
	m, rep, err := store(true).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	red := rep.Compositional
	if red == nil || rep.GlobalReason != "" {
		t.Fatalf("Build did not check the store per component:\n%s", rep)
	}
	if rep.Assurance != AssuranceOracleComponents || !m.lazy {
		t.Fatalf("Assurance = %v, lazy = %v; want AssuranceOracleComponents and a lazy machine", rep.Assurance, m.lazy)
	}
	want := [][]string{{"shipped", "paid", "charged"}, {"stock", "reserved", "backordered"}}
	if fmt.Sprint(red.Vars) != fmt.Sprint(want) {
		t.Fatalf("components %v, want %v", red.Vars, want)
	}
	// 2*2*16 = 64 and 256*256*2 = 131,072 states, instead of their product.
	if red.LargestStates != 131072 || red.StatesChecked != 131136 || red.GlobalStates.String() != "8388608" {
		t.Fatalf("cost: largest %d, checked %d, global %v", red.LargestStates, red.StatesChecked, red.GlobalStates)
	}
	// 5 events, 10 pairs: 3 within orders, 1 within inventory, 6 across.
	if red.CrossPairs != 6 || rep.PairsDisjoint != 6 || rep.PairsBrute != 4 || rep.PairsTotal != 10 {
		t.Fatalf("pairs: cross %d, disjoint %d, brute %d, total %d", red.CrossPairs, rep.PairsDisjoint, rep.PairsBrute, rep.PairsTotal)
	}
	// bound_sum: the machine's bound is the sum of the components' deepest chains.
	if fmt.Sprint(red.RepairBounds) != "[1 1]" || rep.MaxRepairLen != 2 || m.repairBound != 2 {
		t.Fatalf("repair bounds %v, MaxRepairLen %d, machine bound %d; want [1 1], 2, 2", red.RepairBounds, rep.MaxRepairLen, m.repairBound)
	}
	out := rep.String()
	for _, line := range []string{
		"  Convergence: GUARANTEED\n",
		"  Verified compositionally: 2 components (largest 131,072 states; 131,136 states checked instead of 8,388,608); " +
			"6 cross-component pairs need no check (disjoint footprints, reads included); footprints checked exactly (combinators)\n",
		"  Assurance: component tables certified by the verified table oracle;",
		"  Rules oracle: not run: checked per component",
		"  Delivery: exactly once for charge, reserve, restock",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("report lacks %q:\n%s", line, out)
		}
	}
	// The machine runs: shipping unpaid charges, reserving past stock flags a backorder.
	s := m.Apply(m.NewState(), "ship")
	s = m.Apply(s, "reserve")
	if s.packed != m.Apply(m.Apply(m.NewState(), "reserve"), "ship").packed {
		t.Fatal("ship and reserve do not commute")
	}
	if !s.GetBool(r0(m, "paid")) || !s.GetBool(r0(m, "backordered")) {
		t.Fatalf("repairs did not run: %s", s)
	}
}

// r0 returns machine m's variable name.
func r0(m *Machine, name string) Var {
	v, ok := m.varByName(name)
	if !ok {
		panic(name)
	}
	return v
}

// TestBuildDefault_SmallMachineStaysGlobal: a machine small enough to enumerate keeps
// the global check (step tables, both whole-machine oracles), and the report says why.
func TestBuildDefault_SmallMachineStaysGlobal(t *testing.T) {
	m, rep, err := store(false).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Compositional != nil || m.lazy || rep.Assurance != AssuranceOracleTablesAndRules {
		t.Fatalf("the small store was not checked globally:\n%s", rep)
	}
	if !strings.Contains(rep.GlobalReason, "the whole machine has 2,048 states, within Build's enumeration limit") {
		t.Fatalf("GlobalReason = %q", rep.GlobalReason)
	}
	if !strings.Contains(rep.String(), "  Checked globally: the whole machine has 2,048 states") {
		t.Fatalf("report does not say the check was global:\n%s", rep)
	}
	// The per-component check of the same machine agrees with it.
	assertPathsAgree(t, store(false))
}

// pathResult is what a build decides, for comparing the two paths.
type pathResult struct {
	ok, wfc, cc     bool
	failure         string
	maxRepair       int
	notIdempotent   string
	causal          string
	undeclared      int
	pairsTotal      int
	saturations     string
	compositional   bool
	globalReason    string
	componentBounds int
}

func summarize(rep *Report, err error) pathResult {
	p := pathResult{ok: err == nil}
	if rep == nil {
		return p
	}
	p.wfc, p.cc = rep.WFC, rep.CC
	if f := rep.CCFailure; f != nil {
		p.failure = fmt.Sprintf("(%s,%s) at %d: %d vs %d", f.Event1, f.Event2, f.State.packed, f.Result1.packed, f.Result2.packed)
	}
	if rep.WFC {
		p.maxRepair = rep.MaxRepairLen
	}
	p.notIdempotent = strings.Join(rep.NotIdempotent, ",")
	var causal []string
	for _, f := range rep.CausalOrderRequired {
		causal = append(causal, fmt.Sprintf("(%s,%s)@%d", f.Event1, f.Event2, f.State.packed))
	}
	p.causal = strings.Join(causal, " ")
	p.undeclared = rep.PairsUndeclared
	if rep.CC {
		p.pairsTotal = rep.PairsTotal
	}
	// Which rules saturate which variables; the counts differ, since the global check
	// counts every state of the machine and the per-component check those of a component.
	var sats []string
	for _, s := range rep.Saturations {
		sats = append(sats, s.Rule+"/"+s.Var)
	}
	sort.Strings(sats)
	p.saturations = strings.Join(sats, " ")
	p.compositional = rep.Compositional != nil
	p.globalReason = rep.GlobalReason
	if rep.Compositional != nil {
		for _, d := range rep.Compositional.RepairBounds {
			p.componentBounds += d
		}
	}
	return p
}

// assertPathsAgree builds r both ways, the per-component check forced where it applies,
// and fails unless they decide the same: pass or fail, the CC witness, the repair depth,
// the delivery obligations and the saturating rules. It reports whether the
// per-component check ran.
func assertPathsAgree(t *testing.T, r *Registry) bool {
	t.Helper()
	_, grep, gerr := r.buildWith(buildOpts{})
	_, crep, cerr := r.buildWith(buildOpts{compositionalFirst: true})
	g, c := summarize(grep, gerr), summarize(crep, cerr)
	if g.compositional {
		t.Fatalf("%s: the default build of a small machine ran per component", r.name)
	}
	if !c.compositional {
		return false
	}
	if c.wfc && c.componentBounds != c.maxRepair {
		t.Fatalf("%s: MaxRepairLen %d is not the sum of the component bounds %d", r.name, c.maxRepair, c.componentBounds)
	}
	g.compositional, g.globalReason, g.componentBounds = false, "", 0
	c.compositional, c.globalReason, c.componentBounds = false, "", 0
	if g != c {
		t.Fatalf("%s: the paths disagree\nglobal:        %+v (%v)\nper component: %+v (%v)\nglobal report:\n%s\nper-component report:\n%s",
			r.name, g, gerr, c, cerr, grep, crep)
	}
	return true
}

// randomDecomposable builds a combinator registry of two to four groups of variables,
// each with its own events and invariants, sometimes with an event that reads another
// group (which merges the two, as a read must) and sometimes with declared pairs.
func randomDecomposable(rng *rand.Rand, name string) *Registry {
	r := NewRegistry(name)
	groups := 2 + rng.Intn(3)
	var vars [][]Var
	for g := 0; g < groups; g++ {
		var vs []Var
		for k := 0; k < 1+rng.Intn(2); k++ {
			vn := fmt.Sprintf("g%dv%d", g, k)
			switch rng.Intn(3) {
			case 0:
				vs = append(vs, r.Bool(vn))
			case 1:
				vs = append(vs, r.Enum(vn, "a", "b", "c"))
			default:
				vs = append(vs, r.Int(vn, 0, 1+rng.Intn(3)))
			}
		}
		vars = append(vars, vs)
	}
	pick := func(vs []Var) Var { return vs[rng.Intn(len(vs))] }
	expr := func(vs []Var) Expr {
		switch rng.Intn(3) {
		case 0:
			return Lit(rng.Intn(3))
		case 1:
			return V(pick(vs))
		}
		return Add(V(pick(vs)), Lit(1))
	}
	pred := func(vs []Var) Pred {
		p := Le(V(pick(vs)), expr(vs))
		switch rng.Intn(3) {
		case 0:
			return Not(p)
		case 1:
			return Or(p, Eq(V(pick(vs)), Lit(0)))
		}
		return p
	}
	ev := 0
	for g, vs := range vars {
		for k := 0; k < 1+rng.Intn(2); k++ {
			reads := vs
			if rng.Intn(6) == 0 { // a read of another group: the groups must merge
				reads = vars[(g+1)%groups]
			}
			effect := Do(Set(pick(vs), expr(vs)))
			if rng.Intn(2) == 0 {
				r.DeclEventGuarded(fmt.Sprintf("e%d", ev), pred(reads), effect)
			} else {
				r.DeclEvent(fmt.Sprintf("e%d", ev), effect)
			}
			ev++
		}
		if rng.Intn(3) > 0 {
			x := pick(vs)
			r.DeclInvariant(fmt.Sprintf("i%d", g), Or(Le(V(x), Lit(1)), pred(vs)), Do(Set(x, Lit(rng.Intn(2)))))
		}
	}
	if rng.Intn(4) == 0 && ev >= 2 {
		r.Independent("e0", fmt.Sprintf("e%d", ev-1))
	}
	return r
}

// TestCompositional_DifferentialRandom: on many random decomposable registries, the
// per-component check and the global check decide the same, passing and failing, with
// the same witness (compositional_exact, cc1_same_iff) and the same repair depth.
func TestCompositional_DifferentialRandom(t *testing.T) {
	n := 400
	if testing.Short() {
		n = 100
	}
	ran, pass, fail := 0, 0, 0
	for i := 0; i < n; i++ {
		r := randomDecomposable(rand.New(rand.NewSource(int64(i))), fmt.Sprintf("decomp_%d", i))
		if assertPathsAgree(t, r) {
			ran++
			if _, _, err := r.buildWith(buildOpts{compositionalFirst: true}); err == nil {
				pass++
			} else {
				fail++
			}
		}
	}
	t.Logf("%d of %d random registries checked both ways: %d pass, %d fail", ran, n, pass, fail)
	if ran < n/2 || pass < n/20 || fail < n/20 {
		t.Fatalf("too few comparisons: %d ran, %d pass, %d fail", ran, pass, fail)
	}
}

// pad adds independent counters to r until the whole machine is too large for Build to
// enumerate, so Build's default path is the per-component one.
func pad(r *Registry) {
	for k := 0; r.totalBits <= 20; k++ {
		c := r.Int(fmt.Sprintf("pad%d", k), 0, 2047)
		r.On(fmt.Sprintf("tick%d", k)).OnlyIf(Below(c, 2047)).Does(Inc(c)).Add()
	}
}

// payShip is the theory's ws machine with combinators: pay writes paid, and ship,
// guarded on paid, writes shipped.
func payShipComb(name string) *Registry {
	r := NewRegistry(name)
	paid := r.Bool("paid")
	shipped := r.Bool("shipped")
	r.On("pay").Does(Raise(paid)).Add()
	r.On("ship").OnlyIf(Is(paid, 1)).Does(Raise(shipped)).Add()
	return r
}

// TestCompositional_ReadsMergeComponents mirrors ws_diverges: with writes-only footprints
// pay and ship would sit in separate components, each would pass, and the machine
// diverges. Reads are in the footprint, so paid and shipped land in one component
// (inside_merge, ws_true_footprint), and the per-component check fails the machine as
// the global check does.
func TestCompositional_ReadsMergeComponents(t *testing.T) {
	small := payShipComb("pay_ship")
	if !assertPathsAgree(t, small) {
		_, rep, _ := small.buildWith(buildOpts{compositionalFirst: true})
		if !strings.Contains(rep.GlobalReason, "one footprint component") {
			t.Fatalf("GlobalReason = %q, want one component", rep.GlobalReason)
		}
	}
	_, rep, err := small.BuildCompositional()
	if err == nil || rep.Components != 1 || rep.CCFailure == nil {
		t.Fatalf("BuildCompositional: want one component and a CC failure, got %v:\n%s", err, rep)
	}

	wide := payShipComb("pay_ship_padded")
	pad(wide)
	_, rep, err = wide.Build()
	if err == nil {
		t.Fatalf("Build certified pay/ship:\n%s", rep)
	}
	if rep.Compositional == nil || fmt.Sprint(rep.Compositional.Vars[0]) != "[paid shipped]" {
		t.Fatalf("want paid and shipped in one component, checked per component:\n%s", rep)
	}
	if f := rep.CCFailure; f == nil || f.Event1 != "pay" || f.Event2 != "ship" || f.State.packed != 0 {
		t.Fatalf("want the (pay, ship) failure from the zero state:\n%s", rep)
	}
	if !strings.Contains(rep.String(), "  Checked compositionally: 3 components") {
		t.Fatalf("the failing report does not say the check was per component:\n%s", rep)
	}
}

// rcComb is the theory's rc machine with combinators: the invariant "stock is not 2" is
// repaired by resetting stock and setting reserved, so its repair writes reserved.
func rcComb(name string) *Registry {
	r := NewRegistry(name)
	stock := r.Int("stock", 0, 2)
	reserved := r.Int("reserved", 0, 2)
	r.DeclInvariant("stock_not_2", Ne(V(stock), Lit(2)), Do(Set(stock, Lit(0)), Set(reserved, Lit(1))))
	r.DeclEvent("raise_stock", Do(Set(stock, Lit(2))))
	r.DeclEvent("set_reserved", Do(Set(reserved, Lit(2))))
	return r
}

// TestCompositional_RepairCrossingComponents mirrors rc_diverges. A combinator repair's
// writes are in its footprint, so stock and reserved merge and the machine fails as the
// global check fails it. A closure repair that writes outside its declared Watches is
// refused, naming the invariant and the variable.
func TestCompositional_RepairCrossingComponents(t *testing.T) {
	if _, _, err := rcComb("rc").Build(); err == nil {
		t.Fatal("Build certified the rc machine")
	}
	_, rep, err := rcComb("rc").BuildCompositional()
	if err == nil || rep.Components != 1 || rep.CCFailure == nil {
		t.Fatalf("BuildCompositional: want one component and a CC failure, got %v:\n%s", err, rep)
	}
	wide := rcComb("rc_padded")
	pad(wide)
	if _, rep, err := wide.Build(); err == nil || rep.Compositional == nil || rep.CCFailure == nil {
		t.Fatalf("Build of the padded rc machine: want a per-component CC failure, got %v:\n%s", err, rep)
	}

	r := NewRegistry("rc_closure")
	stock := r.Int("stock", 0, 2)
	reserved := r.Int("reserved", 0, 2)
	r.Invariant("stock_not_2").Watches(stock).
		Holds(func(s State) bool { return s.GetInt(stock) != 2 }).
		Repair(func(s State) State { return s.SetInt(stock, 0).SetInt(reserved, 1) }).Add()
	r.Event("raise_stock").Writes(stock).Apply(func(s State) State { return s.SetInt(stock, 2) }).Add()
	r.Event("set_reserved").Writes(reserved).Apply(func(s State) State { return s.SetInt(reserved, 2) }).Add()
	_, _, err = r.BuildCompositional(TrustClosureFootprints())
	if err == nil || !strings.Contains(err.Error(), `invariant repair "stock_not_2" writes variable "reserved" outside its declared footprint`) {
		t.Fatalf("want the repair refused, naming it and reserved; got %v", err)
	}
	if _, _, err := r.Build(); err == nil {
		t.Fatal("Build certified the closure rc machine")
	}
}

// TestCompositional_WrittenSharedVariableMerges mirrors sw_diverges: paid is written by
// pay and read by both ship (orders) and reserve (inventory). gsm has no shared
// variables: the reads merge all three into one component, and the machine fails as the
// global check fails it.
func TestCompositional_WrittenSharedVariableMerges(t *testing.T) {
	build := func(name string) *Registry {
		r := NewRegistry(name)
		paid := r.Bool("paid")
		shipped := r.Bool("shipped")
		reserved := r.Bool("reserved")
		r.On("pay").Does(Raise(paid)).Add()
		r.On("ship").OnlyIf(Is(paid, 1)).Does(Raise(shipped)).Add()
		r.On("reserve").OnlyIf(Is(paid, 1)).Does(Raise(reserved)).Add()
		return r
	}
	_, rep, err := build("sw").BuildCompositional()
	if err == nil || rep.Components != 1 || rep.CCFailure == nil {
		t.Fatalf("want one component and a CC failure, got %v:\n%s", err, rep)
	}
	wide := build("sw_padded")
	pad(wide)
	_, rep, err = wide.Build()
	if err == nil || rep.Compositional == nil || fmt.Sprint(rep.Compositional.Vars[0]) != "[paid shipped reserved]" {
		t.Fatalf("want paid, shipped and reserved in one component, failing:\n%v\n%s", err, rep)
	}
}

// TestCompositional_OversizedComponentFallsBack: a component above the per-component
// limit sends Build to the global check, which decides the machine, and the report says
// why. When the global check cannot run either, the error gives both reasons.
func TestCompositional_OversizedComponentFallsBack(t *testing.T) {
	r := store(false)
	_, rep, err := r.buildWith(buildOpts{compositionalFirst: true, componentBits: 5})
	if err != nil || rep.Compositional != nil {
		t.Fatalf("want the global check, got %v:\n%s", err, rep)
	}
	if !strings.Contains(rep.GlobalReason, "component {shipped, paid, charged} needs 6 bits, above the per-component limit of 5") {
		t.Fatalf("GlobalReason = %q", rep.GlobalReason)
	}

	big := NewRegistry("big_component")
	a := big.Int("a", 0, 2047)
	b := big.Int("b", 0, 2047)
	big.On("copy").Does(Do(Set(a, V(b)))).Add()
	flag := big.Bool("flag")
	big.On("raise").Does(Raise(flag)).Add()
	_, _, err = big.Build()
	if err == nil || !strings.Contains(err.Error(), "state space too large (23 bits, max 20)") ||
		!strings.Contains(err.Error(), "component {a, b} needs 22 bits, above the per-component limit of 20") {
		t.Fatalf("want both reasons, got %v", err)
	}
}

// TestCompositional_ClosuresFallBackToGlobal: Build never decomposes a closure rule,
// whose footprint gsm can only test. A small closure machine is checked globally (and
// the report names the closure when the per-component check would otherwise run); a
// closure machine too large for the global check is refused with both reasons, and
// BuildCompositional with TrustClosureFootprints is the opt-in.
func TestCompositional_ClosuresFallBackToGlobal(t *testing.T) {
	_, rep, err := wideCounters(3).buildWith(buildOpts{compositionalFirst: true})
	if err != nil || rep.Compositional != nil {
		t.Fatalf("want the global check, got %v:\n%s", err, rep)
	}
	if !strings.Contains(rep.GlobalReason, `event "inc0" is a Go closure`) {
		t.Fatalf("GlobalReason = %q", rep.GlobalReason)
	}
	_, _, err = wideCounters(11).Build()
	if err == nil || !strings.Contains(err.Error(), "state space too large (22 bits, max 20)") ||
		!strings.Contains(err.Error(), `event "inc0" is a Go closure`) {
		t.Fatalf("want both reasons, got %v", err)
	}
	if _, rep, err := wideCounters(11).BuildCompositional(TrustClosureFootprints()); err != nil ||
		rep.Assurance != AssuranceOracleComponentsTested || rep.Compositional.FootprintsExact {
		t.Fatalf("BuildCompositional with the opt-in: %v\n%s", err, rep)
	}
}

// TestCompositional_RepairBoundIsTheSum: the machine's repair bound and MaxRepairLen are
// the sum of the components' deepest chains (bound_sum), which is also the global
// check's longest chain.
func TestCompositional_RepairBoundIsTheSum(t *testing.T) {
	r := NewRegistry("caps")
	for k := 0; k < 3; k++ {
		v := r.Int(fmt.Sprintf("c%d", k), 0, 3)
		r.Rule(fmt.Sprintf("cap%d", k)).Require(AtMost(v, 2)).RepairWith(SetTo(v, 2)).Add()
		r.On(fmt.Sprintf("inc%d", k)).Does(Inc(v)).Add()
	}
	if !assertPathsAgree(t, r) {
		t.Fatal("the per-component check did not run")
	}
	m, rep, err := r.buildWith(buildOpts{compositionalFirst: true})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rep.Compositional.RepairBounds) != "[1 1 1]" || rep.MaxRepairLen != 3 || m.repairBound != 3 {
		t.Fatalf("bounds %v, MaxRepairLen %d, machine %d; want [1 1 1], 3, 3", rep.Compositional.RepairBounds, rep.MaxRepairLen, m.repairBound)
	}
	s := m.NewState()
	for k := 0; k < 3; k++ {
		s = s.SetInt(r0(m, fmt.Sprintf("c%d", k)), 3)
	}
	if n := m.Normalize(s); n.GetInt(r0(m, "c0")) != 2 || n.GetInt(r0(m, "c2")) != 2 {
		t.Fatalf("Normalize(%s) = %s", s, n)
	}
}

// TestCompositional_DeclaredPairs: in declared-only mode the undeclared pairs are checked
// the same way on both paths; a pair in different components commutes (cc1_cross), and a
// same-component pair that does not is listed with the global check's witness.
func TestCompositional_DeclaredPairs(t *testing.T) {
	r := payShipComb("declared")
	other := r.Bool("other")
	r.On("touch").Does(Raise(other)).Add()
	r.Independent("pay", "touch")
	if !assertPathsAgree(t, r) {
		t.Fatal("the per-component check did not run")
	}
	_, rep, err := r.buildWith(buildOpts{compositionalFirst: true})
	if err != nil || rep.PairsUndeclared != 2 || len(rep.CausalOrderRequired) != 1 ||
		rep.CausalOrderRequired[0].Event1 != "pay" || rep.CausalOrderRequired[0].Event2 != "ship" {
		t.Fatalf("want (pay, ship) listed for causal order, got %v:\n%s", err, rep)
	}
}

// TestCompositional_CollectionTemplateStaysWhole: the theory states no combined theorem
// for symmetry with compositional checking, so a collection template is checked as a
// whole; one too large to enumerate is refused, saying so.
func TestCompositional_CollectionTemplateStaysWhole(t *testing.T) {
	if _, rep, err := NewCollection[string]("OrderID", store(false)).Build(); err != nil || rep.Compositional != nil || rep.Symmetry == nil {
		t.Fatalf("small template: %v\n%s", err, rep)
	}
	_, _, err := NewCollection[string]("OrderID", store(true)).Build()
	if err == nil || !strings.Contains(err.Error(), "it is a collection template") {
		t.Fatalf("want the template refused as too large, naming the collection, got %v", err)
	}
	// Built on its own, the same registry is checked per component.
	if _, rep, err := store(true).Build(); err != nil || rep.Compositional == nil {
		t.Fatalf("store(true).Build(): %v\n%s", err, rep)
	}
}

// TestCompositional_AbstractionTakesPrecedence: a registry declared with Abstract is
// verified by abstraction even when it also decomposes; the two reductions are not
// combined (no combined theorem is stated).
func TestCompositional_AbstractionTakesPrecedence(t *testing.T) {
	r := NewRegistry("abs_two")
	a := r.Int("a", 0, 1_000_000)
	b := r.Int("b", 0, 1_000_000)
	r.Rule("cap_a").Require(AtMost(a, 5)).RepairWith(SetTo(a, 5)).Add()
	r.Rule("cap_b").Require(AtMost(b, 5)).RepairWith(SetTo(b, 5)).Add()
	r.On("bump_a").OnlyIf(AtMost(a, 4)).Does(SetTo(a, 5)).Add()
	r.On("bump_b").OnlyIf(AtMost(b, 4)).Does(SetTo(b, 5)).Add()
	_, rep, err := r.Abstract(4, 5).Build()
	if err != nil || rep.Abstraction == nil || rep.Compositional != nil {
		t.Fatalf("want abstraction, not per-component: %v\n%s", err, rep)
	}
}

// TestCompositional_FederationComponentStaysWhole: a federation checks its components on
// their step tables, so a component is checked as a whole.
func TestCompositional_FederationComponentStaysWhole(t *testing.T) {
	_, _, err := NewFederation("shop").Add(store(true)).Build()
	if err == nil || !strings.Contains(err.Error(), "it is a federation component") {
		t.Fatalf("want the component refused as too large, naming the federation, got %v", err)
	}
	if _, _, err := NewFederation("shop").Add(store(false)).Build(); err != nil {
		t.Fatalf("small component: %v", err)
	}
}

// TestCompositional_IdentityEventNeedsNoComponent: a combinator event that reads and
// writes nothing is the identity; it lies in no component and commutes with every event.
func TestCompositional_IdentityEventNeedsNoComponent(t *testing.T) {
	r := store(false)
	r.DeclEvent("noop", Do())
	if !assertPathsAgree(t, r) {
		t.Fatal("the per-component check did not run")
	}
}
