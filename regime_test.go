package gsm

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"
)

// regimeTheorems is the allowlist of theorem names a regime summary may cite. Every name
// was checked against normalization-confluence coq/verify.sh (each is gated there with
// "Print Assumptions", possibly module-qualified) and is already cited by gsm's code or
// docs for the same claim. A summary that cites any other name fails
// TestRegime_CitesOnlyAllowlistedTheorems: add a name here only after checking verify.sh.
var regimeTheorems = map[string]bool{
	// Single registry: the oracle-certified tables and rules (TableCheck.v, AstChecker.v, Trace.v).
	"check_tables_converges": true, "check_tables_converges_all": true, "checkBuild_converges": true,
	"run_tequiv": true, "perm_tequiv_total": true, "causal_tequiv": true,
	// Per footprint component (CompositionalCheck.v).
	"compositional_exact": true, "cc1_cross": true, "cc1_same_iff": true,
	// Collections (SymmetryCutoff.v) and abstraction (AbstractionGsm.v).
	"run_proj": true, "cross_commute": true, "alo_cutoff": true,
	"gsm_abs_sound": true, "gsm_abs_sound_all": true, "idem_valid_abs": true,
	// Delivery (AtLeastOnce*.v).
	"alo_exact": true, "dalo_unlisted_converge": true, "fl_retry_order_needed": true, "late_duplicate_diverges": true,
	// Federations (FederationEvents*.v, Chaotic.v, CoordinatedCycles.v).
	"fed_events_commute": true, "static_c1_c2_gc": true, "cyc_check_gc_lfp": true, "chaotic_reaches_lfp": true,
	"root_choice_matters": true,
	// Projection deployments (FederationEvents.v, ProjectionChannels.v, DistributedDelivery.v, DistributedCycles.v).
	"dist_interleavings_converge": true, "vsettle_xu_c2": true, "no_final_send_counterexample": true,
	"version_order_counterexample": true, "plain_stale_counterexample": true, "dist_alo_exact": true,
	"dist_cyc_ghost": true, "vchan_cyc_ghost": true,
	// Migration (Reconfiguration.v, ReconfigurationClosure.v).
	"det_live_exact": true, "det_live_implies_barrier": true, "det_barrier_exact": true, "det_barrier_faithful": true,
	"det_barrier_closure_exact": true, "amodm_closure_exact": true, "gsm_closure_exact": true, "amodm_witness_exact": true,
}

// snakeToken matches an identifier with an underscore: how a theorem name looks in the text.
var snakeToken = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9]*_[a-zA-Z0-9_]+\b`)

// checkRegimeShape asserts the structural rules every summary obeys: four parts, every
// Guaranteed line cites a theorem, every cited name is allowlisted, and every underscore
// identifier in the printed block is an allowlisted theorem (the shape tests use event,
// variable and registry names without underscores, so any such token is a citation).
func checkRegimeShape(t *testing.T, s *RegimeSummary) {
	t.Helper()
	if s == nil {
		t.Fatal("Regime is nil")
	}
	if len(s.Regime) == 0 {
		t.Fatal("Regime part is empty")
	}
	for _, l := range s.Guaranteed {
		if len(l.Theorems) == 0 {
			t.Errorf("Guaranteed line without a theorem: %q", l.Text)
		}
	}
	for _, name := range s.Theorems() {
		if !regimeTheorems[name] {
			t.Errorf("cites %q, which is not in the allowlist", name)
		}
	}
	for _, tok := range snakeToken.FindAllString(s.String(), -1) {
		if !regimeTheorems[tok] {
			t.Errorf("printed block names %q, which is not an allowlisted theorem", tok)
		}
	}
}

// lineWith returns the first line of lines whose text contains sub.
func lineWith(lines []RegimeLine, sub string) (RegimeLine, bool) {
	for _, l := range lines {
		if strings.Contains(l.Text, sub) {
			return l, true
		}
	}
	return RegimeLine{}, false
}

func cites(l RegimeLine, name string) bool {
	for _, t := range l.Theorems {
		if t == name {
			return true
		}
	}
	return false
}

func mustLine(t *testing.T, part string, lines []RegimeLine, sub string) RegimeLine {
	t.Helper()
	l, ok := lineWith(lines, sub)
	if !ok {
		var got []string
		for _, l := range lines {
			got = append(got, l.String())
		}
		t.Fatalf("%s has no line containing %q; it has:\n  %s", part, sub, strings.Join(got, "\n  "))
	}
	return l
}

func mustNotLine(t *testing.T, part string, lines []RegimeLine, sub string) {
	t.Helper()
	if l, ok := lineWith(lines, sub); ok {
		t.Fatalf("%s has a line containing %q: %s", part, sub, l)
	}
}

func regimeHas(s *RegimeSummary, sub string) bool {
	for _, r := range s.Regime {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// flags: combinator flags, every event idempotent, every pair commuting.
func regimeFlags() *Registry {
	r := NewRegistry("flags")
	paid, shipped := r.Bool("paid"), r.Bool("shipped")
	r.Rule("shippedneedspaid").Require(Or(Is(shipped, 0), Is(paid, 1))).RepairWith(Raise(paid)).Add()
	r.On("pay").Does(Raise(paid)).Add()
	r.On("ship").Does(Raise(shipped)).Add()
	return r
}

// counter: an increment (not idempotent) and a reset (which does not commute with it).
func regimeCounter(name string) *Registry {
	r := NewRegistry(name)
	c := r.Int("count", 0, 3)
	r.On("inc").OnlyIf(Below(c, 3)).Does(Inc(c)).Add()
	r.On("reset").Does(SetTo(c, 0)).Add()
	r.On("noop").Does(SetTo(c, 0)).Add()
	return r
}

func TestRegime_SingleRegistry(t *testing.T) {
	_, rep, err := regimeFlags().Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	for _, want := range []string{"single registry", "checked globally", "every event pair independent"} {
		if !regimeHas(s, want) {
			t.Errorf("Regime %q lacks %q", s.Regime, want)
		}
	}
	conv := mustLine(t, "Guaranteed", s.Guaranteed, "every order of the same events")
	if !cites(conv, "check_tables_converges_all") || !cites(conv, "checkBuild_converges") {
		t.Errorf("convergence line cites %v", conv.Theorems)
	}
	if rep.Assurance != AssuranceOracleTablesAndRules {
		t.Fatalf("assurance %v: the flags machine is a combinator machine", rep.Assurance)
	}
	alo := mustLine(t, "Guaranteed", s.Guaranteed, "at-least-once delivery")
	if !cites(alo, "alo_exact") {
		t.Errorf("at-least-once line cites %v", alo.Theorems)
	}
	if len(s.MustProvide) != 0 || len(s.NotCovered) != 0 {
		t.Errorf("a plain registry needs nothing and leaves nothing uncovered:\n%s", s)
	}
	if !strings.Contains(rep.String(), "\n  Regime: single registry; checked globally; every event pair independent\n") ||
		!strings.Contains(rep.String(), "  You must provide: none\n  Not covered: none\n") {
		t.Errorf("printed report:\n%s", rep)
	}
}

func TestRegime_NotIdempotent(t *testing.T) {
	r := NewRegistry("wallet")
	bal := r.Int("balance", 0, 3)
	r.On("deposit").OnlyIf(Below(bal, 3)).Does(Inc(bal)).Add()
	_, rep, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	mustNotLine(t, "Guaranteed", s.Guaranteed, "at-least-once")
	l := mustLine(t, "You must provide", s.MustProvide, "if your transport can redeliver: deduplicate deposit")
	if !cites(l, "alo_exact") {
		t.Errorf("dedupe line cites %v", l.Theorems)
	}
}

func TestRegime_DeclaredPairs(t *testing.T) {
	r := regimeCounter("declared")
	r.Independent("reset", "noop")
	_, rep, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.CausalOrderRequired) == 0 {
		t.Fatalf("inc and reset should need causal order:\n%s", rep)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "declared Independent pairs") {
		t.Errorf("Regime %q", s.Regime)
	}
	conv := mustLine(t, "Guaranteed", s.Guaranteed, "declared-independent events")
	if !cites(conv, "check_tables_converges") || cites(conv, "check_tables_converges_all") {
		t.Errorf("declared-mode convergence cites %v", conv.Theorems)
	}
	mustNotLine(t, "Guaranteed", s.Guaranteed, "every undeclared pair commutes")
	mustNotLine(t, "Guaranteed", s.Guaranteed, "at-least-once")
	causal := mustLine(t, "You must provide", s.MustProvide, "causal order for the undeclared pair(s) (")
	if !strings.Contains(causal.Text, "inc") || !cites(causal, "causal_tequiv") {
		t.Errorf("causal line: %s", causal)
	}
	mustLine(t, "You must provide", s.MustProvide, "a retry keeps its causal place")
	dd := mustLine(t, "You must provide", s.MustProvide, "deduplicate inc")
	if !cites(dd, "dalo_unlisted_converge") {
		t.Errorf("declared-mode dedupe line cites %v", dd.Theorems)
	}
	mustLine(t, "Not covered", s.NotCovered, "CheckMigration refuses a registry with Independent pairs")

	// Declared pairs whose undeclared partners all commute: any order converges.
	r2 := regimeFlags()
	r2.Independent("pay", "pay")
	_, rep2, err := r2.Build()
	if err != nil {
		t.Fatal(err)
	}
	if rep2.PairsUndeclared == 0 || len(rep2.CausalOrderRequired) != 0 {
		t.Fatalf("want undeclared pairs that all commute: %d undeclared, %d causal", rep2.PairsUndeclared, len(rep2.CausalOrderRequired))
	}
	checkRegimeShape(t, rep2.Regime)
	mustLine(t, "Guaranteed", rep2.Regime.Guaranteed, "every undeclared pair commutes too")
	mustLine(t, "Guaranteed", rep2.Regime.Guaranteed, "at-least-once delivery")
	mustNotLine(t, "You must provide", rep2.Regime.MustProvide, "causal")
}

func TestRegime_Collection(t *testing.T) {
	tpl0 := NewRegistry("item")
	c := tpl0.Int("count", 0, 3)
	seen := tpl0.Bool("seen")
	tpl0.On("inc").OnlyIf(Below(c, 3)).Does(Inc(c)).Add()
	tpl0.On("touch").Does(Raise(seen)).Add()
	_, rep, err := NewCollection[string]("ProductID", tpl0).Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "keyed collection over ProductID") {
		t.Errorf("Regime %q", s.Regime)
	}
	conv := mustLine(t, "Guaranteed", s.Guaranteed, "at every key, for any number of keys")
	if !cites(conv, "run_proj") {
		t.Errorf("collection convergence cites %v", conv.Theorems)
	}
	if l := mustLine(t, "Guaranteed", s.Guaranteed, "events on different keys commute"); !cites(l, "cross_commute") {
		t.Errorf("cross-key line cites %v", l.Theorems)
	}
	mustLine(t, "You must provide", s.MustProvide, "deduplicate inc per ProductID")
	mustLine(t, "Not covered", s.NotCovered, "CheckMigration takes single registries")

	// Per-key causal wording on a declared template.
	tpl := regimeCounter("item")
	tpl.Independent("reset", "noop")
	_, rep2, err := NewCollection[string]("CustomerID", tpl).Build()
	if err != nil {
		t.Fatal(err)
	}
	checkRegimeShape(t, rep2.Regime)
	mustLine(t, "You must provide", rep2.Regime.MustProvide, "when both address the same CustomerID")
}

func TestRegime_Abstraction(t *testing.T) {
	r := NewRegistry("inventory")
	stock := r.Int("stock", 0, 1_000_000)
	shipA := r.Int("shipa", 0, 1_000_000)
	r.Rule("cap").Require(AtMost(stock, 5)).RepairWith(SetTo(stock, 5)).Add()
	r.On("receive").OnlyIf(BelowVar(stock, shipA)).Does(Copy(stock, shipA)).Add()
	_, rep, err := r.Abstract(5).Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "verified by abstraction over stock, shipa") {
		t.Errorf("Regime %q", s.Regime)
	}
	conv := mustLine(t, "Guaranteed", s.Guaranteed, "for every value in the declared ranges")
	if !cites(conv, "gsm_abs_sound_all") {
		t.Errorf("abstraction convergence cites %v", conv.Theorems)
	}
	mustLine(t, "Not covered", s.NotCovered, "abstraction's arithmetic route")
	mustLine(t, "Not covered", s.NotCovered, "CheckMigration refuses a registry declared with Abstract")
}

func TestRegime_PerComponent(t *testing.T) {
	_, rep, err := store(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Compositional == nil {
		t.Fatal("the wide store should be checked per footprint component")
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "checked per footprint component (2)") {
		t.Errorf("Regime %q", s.Regime)
	}
	conv := mustLine(t, "Guaranteed", s.Guaranteed, "every order of the same events")
	if !cites(conv, "compositional_exact") {
		t.Errorf("per-component convergence cites %v", conv.Theorems)
	}

	// Closure footprints, tested: no whole-machine claim.
	_, rep2, err := wideCounters(3).BuildCompositional(TrustClosureFootprints())
	if err != nil {
		t.Fatal(err)
	}
	s2 := rep2.Regime
	checkRegimeShape(t, s2)
	conv2 := mustLine(t, "Guaranteed", s2.Guaranteed, "within each footprint component")
	if cites(conv2, "compositional_exact") || len(s2.Guaranteed) != 1 {
		t.Errorf("tested footprints must not claim the whole machine:\n%s", s2)
	}
	mustLine(t, "You must provide", s2.MustProvide, "TrustClosureFootprints acknowledges")
	mustLine(t, "Not covered", s2.NotCovered, "closure footprints are tested by perturbation, not proved")
}

// chainFed: hub (alarm) -> node (flag, shared; note, local). node's event writes only a
// local, so XU holds and the federation is projection-safe.
func chainFed(name string) *Federation {
	hub := NewRegistry("hub")
	alarm := hub.Bool("alarm")
	hub.On("raise").Does(Raise(alarm)).Add()
	node := NewRegistry("node")
	flag, note := node.Bool("flag"), node.Bool("note")
	node.On("ack").Does(Raise(note)).Add()
	return NewFederation(name).Morphism(hub, node).Shared(flag).
		Map(func(s, d State) State { return d.SetBool(flag, s.GetBool(alarm)) }).Add()
}

func TestRegime_AcyclicFederation(t *testing.T) {
	_, rep, err := chainFed("chain").Build()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ProjectionSafe {
		t.Fatalf("chain should be projection-safe: %s", rep.ProjectionLine)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	for _, want := range []string{"acyclic federation", "2 component(s), 1 morphism(s)", "projection merging certified (XU)"} {
		if !regimeHas(s, want) {
			t.Errorf("Regime %q lacks %q", s.Regime, want)
		}
	}
	fm := mustLine(t, "Guaranteed", s.Guaranteed, "the FedMachine")
	if !cites(fm, "fed_events_commute") {
		t.Errorf("FedMachine line cites %v", fm.Theorems)
	}
	if l := mustLine(t, "Guaranteed", s.Guaranteed, "merges its sources' projections"); !cites(l, "dist_interleavings_converge") {
		t.Errorf("projection line cites %v", l.Theorems)
	}
	if l := mustLine(t, "Guaranteed", s.Guaranteed, "once the channels drain"); !cites(l, "vsettle_xu_c2") {
		t.Errorf("channel line cites %v", l.Theorems)
	}
	mustLine(t, "You must provide", s.MustProvide, "a send after every source change")
	mustLine(t, "You must provide", s.MustProvide, "stamp versions in send order on the snapshot sent")
	if l := mustLine(t, "You must provide", s.MustProvide, "federated step"); !cites(l, "dist_alo_exact") {
		t.Errorf("federated-step line cites %v", l.Theorems)
	}
	mustLine(t, "Not covered", s.NotCovered, "CheckMigration takes single registries")
	if out := rep.String(); !strings.Contains(out, "  Regime: acyclic federation;") {
		t.Errorf("FedReport.String lacks the regime block:\n%s", out)
	}
	for _, c := range rep.Components {
		if c.Regime != nil {
			t.Errorf("component %s carries its own regime; the federation's covers it", c.Name)
		}
	}
}

func TestRegime_ProjectionNotSafe(t *testing.T) {
	f, _, _, _, _ := flipFederation()
	_, rep, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	if rep.ProjectionSafe {
		t.Fatal("flips: XU should fail")
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "projection merging not certified") {
		t.Errorf("Regime %q", s.Regime)
	}
	mustNotLine(t, "Guaranteed", s.Guaranteed, "projection")
	mustLine(t, "You must provide", s.MustProvide, "run the FedMachine itself")
	mustNotLine(t, "You must provide", s.MustProvide, "projection rules")
	mustLine(t, "Not covered", s.NotCovered, "XU is not certified")

	// RequireProjectionSafe: no machine, so no regime.
	f2, _, _, _, _ := flipFederation()
	if _, rep2, err := f2.RequireProjectionSafe().Build(); err == nil || rep2.Regime != nil {
		t.Fatalf("RequireProjectionSafe on flips: err %v, regime %v", err, rep2.Regime)
	}
	// RequireProjectionSafe on a safe federation: named in the regime.
	_, rep3, err := chainFed("chainreq").RequireProjectionSafe().Build()
	if err != nil {
		t.Fatal(err)
	}
	checkRegimeShape(t, rep3.Regime)
	if !regimeHas(rep3.Regime, "projection deployment (RequireProjectionSafe)") {
		t.Errorf("Regime %q", rep3.Regime.Regime)
	}
	mustLine(t, "Guaranteed", rep3.Regime.Guaranteed, "once the channels drain")
}

func TestRegime_MultiSource(t *testing.T) {
	f := accessControlFed()
	_, rep, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "multi-source target(s) with a resolver: door") {
		t.Errorf("Regime %q", s.Regime)
	}
	mustNotLine(t, "Guaranteed", s.Guaranteed, "projection")
	mustLine(t, "Not covered", s.NotCovered, "multi-source target(s) door")
}

// regimeMesh is the monotone mesh of federation_test.go: a 3-cycle of max morphisms.
func regimeMesh() *Federation {
	names := [3]string{"A", "B", "C"}
	var regs [3]*Registry
	var req, sv [3]Var
	for i := range names {
		r := NewRegistry(names[i])
		req[i], sv[i] = r.Int("req", 0, 3), r.Int("s", 0, 3)
		val, rq := i+1, req[i]
		r.Event(fmt.Sprintf("req%d", val)).Writes(rq).Apply(func(st State) State { return st.SetInt(rq, val) }).Add()
		regs[i] = r
	}
	fed := NewFederation("mesh").AllowMonotoneCycles()
	for i := range names {
		pred := (i + 2) % 3
		pReq, pS, dS := req[pred], sv[pred], sv[i]
		fed.Morphism(regs[pred], regs[i]).Shared(dS).Map(func(src, d State) State {
			mx := src.GetInt(pReq)
			if v := src.GetInt(pS); v > mx {
				mx = v
			}
			return d.SetInt(dS, mx)
		}).Add()
	}
	return fed
}

func TestRegime_MonotoneCycles(t *testing.T) {
	fed := regimeMesh()
	_, rep, err := fed.Build()
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	if !regimeHas(s, "federation with monotone cycles") {
		t.Errorf("Regime %q", s.Regime)
	}
	if l := mustLine(t, "Guaranteed", s.Guaranteed, "least fixed point"); !cites(l, "chaotic_reaches_lfp") {
		t.Errorf("lfp line cites %v", l.Theorems)
	}
	if l := mustLine(t, "Guaranteed", s.Guaranteed, "every interleaving"); !cites(l, "cyc_check_gc_lfp") || cites(l, "fed_events_commute") {
		t.Errorf("cyclic event line cites %v", l.Theorems)
	}
	mustNotLine(t, "Guaranteed", s.Guaranteed, "projection")
	if l := mustLine(t, "Not covered", s.NotCovered, "ghost"); !cites(l, "dist_cyc_ghost") {
		t.Errorf("ghost line cites %v", l.Theorems)
	}
}

func TestRegime_Coordinated(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
	plan := fed.CoordinationPlan()
	_, rep, err := fed.BuildCoordinated(plan)
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Regime
	checkRegimeShape(t, s)
	auth := plan[0].Dst
	if !regimeHas(s, "coordinated edges removed: "+plan[0].String()+" (authority "+auth+")") {
		t.Errorf("Regime %q", s.Regime)
	}
	mustLine(t, "Guaranteed", s.Guaranteed, "the residual network")
	mustNotLine(t, "Guaranteed", s.Guaranteed, "projection")
	if rep.ProjectionSafe {
		mustLine(t, "Not covered", s.NotCovered, "a projection deployment with coordinated inputs")
	}
	l := mustLine(t, "You must provide", s.MustProvide, "coordinate "+plan[0].String())
	if !strings.Contains(l.Text, auth+" is the authority root") || !cites(l, "root_choice_matters") {
		t.Errorf("authority line: %s", l)
	}
	mustLine(t, "Not covered", s.NotCovered, "the coordination itself")
}

// migCart is the docs' cart: a capped counter with a closure invariant.
func migCart(name string, limit int, clear bool) (*Registry, Var) {
	r := NewRegistry(name)
	items := r.Int("items", 0, limit+1)
	r.Invariant("cap").Watches(items).
		Holds(func(s State) bool { return s.GetInt(items) <= limit }).
		Repair(func(s State) State { return s.SetInt(items, limit) }).Add()
	r.Event("add").Writes(items).Apply(func(s State) State { return s.SetInt(items, s.GetInt(items)+1) }).Add()
	if clear {
		r.Event("clear").Writes(items).Apply(func(s State) State { return s.SetInt(items, 0) }).Add()
	}
	return r, items
}

func TestRegime_Migration(t *testing.T) {
	check := func(name string, toLimit int, toClear bool, want MigrationOutcome) *MigrationReport {
		t.Helper()
		v1, i1 := migCart("cartv1", 5, false)
		v2, i2 := migCart("cartv2", toLimit, toClear)
		rep, err := CheckMigration(v1, v2, func(old, blank State) State { return blank.SetInt(i2, old.GetInt(i1)) }, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rep.Outcome != want {
			t.Fatalf("%s: outcome %v, want %v\n%s", name, rep.Outcome, want, rep)
		}
		checkRegimeShape(t, rep.Regime)
		if !strings.Contains(rep.String(), "  Regime: change of a single registry, cartv1 -> cartv2;") {
			t.Errorf("%s: printed report lacks the regime block:\n%s", name, rep)
		}
		mustLine(t, "Not covered", rep.Regime.NotCovered, "declared Independent pairs")
		return rep
	}

	online := check("same cap", 5, false, MigrationSafeOnline).Regime
	if l := mustLine(t, "Guaranteed", online.Guaranteed, "a switch at any time"); !cites(l, "det_live_exact") {
		t.Errorf("online line cites %v", l.Theorems)
	}
	mustNotLine(t, "You must provide", online.MustProvide, "drain before switching")

	barrier := check("raised cap", 10, false, MigrationSafeBehindBarrier).Regime
	l := mustLine(t, "Guaranteed", barrier.Guaranteed, "a switch after draining")
	if cites(l, "det_live_exact") || len(barrier.Guaranteed) != 1 {
		t.Errorf("barrier guarantee: %v", barrier.Guaranteed)
	}
	mustLine(t, "You must provide", barrier.MustProvide, "drain before switching")

	unsafe := check("clear added", 5, true, MigrationUnsafe).Regime
	if len(unsafe.Guaranteed) != 0 || len(unsafe.MustProvide) != 0 {
		t.Errorf("unsafe change guarantees nothing:\n%s", unsafe)
	}
}

// accessControlFed is buildAccessControl's federation, unbuilt: door has two sources and a
// resolver.
func accessControlFed() *Federation {
	hr := NewRegistry("hr")
	employed := hr.Bool("employed")
	hr.On("hire").Does(Raise(employed)).Add()
	sec := NewRegistry("sec")
	cleared := sec.Bool("cleared")
	sec.On("clear").Does(Raise(cleared)).Add()
	door := NewRegistry("door")
	okHR, okSec := door.Bool("okhr"), door.Bool("oksec")
	f := NewFederation("access").
		Morphism(hr, door).Shared(okHR).Map(func(s, d State) State { return d.SetBool(okHR, s.GetBool(employed)) }).Add().
		Morphism(sec, door).Shared(okSec).Map(func(s, d State) State { return d.SetBool(okSec, s.GetBool(cleared)) }).Add()
	f.Resolve(door, func(d State, src map[string]State) State {
		return d.SetBool(okHR, src["hr"].GetBool(employed)).SetBool(okSec, src["sec"].GetBool(cleared))
	})
	return f
}

// reportBacked maps each theorem a Guaranteed line may cite to the check whose pass it
// requires, read from the report's own fields. A line citing a theorem whose check did not
// pass is a guarantee the build did not establish.
func reportBacked(rep *Report, thm string) bool {
	certified := rep.WFC && rep.CC && rep.Assurance != AssuranceNone
	exactComp := rep.Compositional != nil && rep.Compositional.FootprintsExact
	allPairs := rep.PairsUndeclared == 0
	noCausal := len(rep.CausalOrderRequired) == 0
	switch thm {
	case "check_tables_converges_all":
		return certified && rep.Compositional == nil && rep.Abstraction == nil && allPairs
	case "check_tables_converges":
		return certified && rep.Abstraction == nil
	case "checkBuild_converges":
		return certified && rep.Assurance == AssuranceOracleTablesAndRules
	case "compositional_exact":
		return certified && exactComp && allPairs
	case "cc1_cross", "cc1_same_iff":
		return certified && exactComp
	case "gsm_abs_sound_all":
		return certified && rep.Abstraction != nil && allPairs
	case "gsm_abs_sound", "idem_valid_abs":
		return certified && rep.Abstraction != nil
	case "run_proj", "cross_commute", "alo_cutoff":
		return certified && rep.Symmetry != nil
	case "run_tequiv", "perm_tequiv_total":
		return certified && rep.Abstraction == nil && (rep.Compositional == nil || exactComp)
	case "alo_exact":
		return certified && len(rep.NotIdempotent) == 0 && noCausal &&
			(rep.Compositional == nil || exactComp) && (allPairs || rep.Abstraction == nil)
	}
	return false
}

func fedBacked(rep *FedReport, thm string) bool {
	built := rep.Assurance != ""
	for _, c := range rep.Components {
		built = built && c.WFC && c.CC && c.Assurance != AssuranceNone
	}
	switch thm {
	case "fed_events_commute", "static_c1_c2_gc":
		return built && !rep.shape.cyclic
	case "chaotic_reaches_lfp", "cyc_check_gc_lfp":
		return built && rep.shape.cyclic
	case "dist_interleavings_converge", "vsettle_xu_c2":
		coordinated := false
		for _, c := range rep.Components {
			coordinated = coordinated || len(c.Coordinated) > 0
		}
		return built && rep.ProjectionSafe && !rep.shape.cyclic && len(rep.shape.multiSource) == 0 && !coordinated
	}
	return false
}

// TestRegime_GuaranteesOnlyWhatPassed is the guard: across many built configurations,
// passing and failing, a report carries a summary only when its build returned a machine,
// and every Guaranteed line rests on a check that passed.
func TestRegime_GuaranteesOnlyWhatPassed(t *testing.T) {
	checkReport := func(label string, m any, rep *Report, err error) {
		t.Helper()
		if rep == nil {
			return
		}
		if err != nil || m == nil {
			if rep.Regime != nil {
				t.Errorf("%s: a failed build carries a regime:\n%s", label, rep.Regime)
			}
			return
		}
		if rep.Regime == nil {
			t.Errorf("%s: a built machine has no regime", label)
			return
		}
		for _, l := range rep.Regime.Guaranteed {
			if len(l.Theorems) == 0 {
				t.Errorf("%s: unbacked line %q", label, l.Text)
			}
			for _, thm := range l.Theorems {
				if !reportBacked(rep, thm) {
					t.Errorf("%s: Guaranteed %q cites %s, whose check did not pass for this report", label, l.Text, thm)
				}
			}
		}
	}
	fedBuilt, fedSafe, fedCyclic := 0, 0, 0
	checkFed := func(label string, m *FedMachine, rep *FedReport, err error) {
		t.Helper()
		if rep == nil {
			return
		}
		if err == nil && m != nil {
			fedBuilt++
			if rep.ProjectionSafe {
				fedSafe++
			}
			if rep.shape.cyclic {
				fedCyclic++
			}
		}
		if err != nil || m == nil {
			if rep.Regime != nil {
				t.Errorf("%s: a failed federation build carries a regime", label)
			}
			return
		}
		if rep.Regime == nil {
			t.Errorf("%s: a built federation has no regime", label)
			return
		}
		for _, l := range rep.Regime.Guaranteed {
			for _, thm := range l.Theorems {
				if !fedBacked(rep, thm) {
					t.Errorf("%s: Guaranteed %q cites %s, whose check did not pass", label, l.Text, thm)
				}
			}
		}
		if !rep.ProjectionSafe {
			if _, ok := lineWith(rep.Regime.Guaranteed, "projection"); ok {
				t.Errorf("%s: ProjectionSafe is false and a projection guarantee is listed", label)
			}
		}
	}

	n := 150
	if testing.Short() {
		n = 40
	}
	built, failed := 0, 0
	for i := 0; i < n; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		r := randomDecomposable(rng, fmt.Sprintf("rand%d", i))
		m, rep, err := r.Build()
		checkReport(fmt.Sprintf("Build %d", i), m, rep, err)
		if err == nil {
			built++
		} else {
			failed++
		}
		r2 := randomDecomposable(rand.New(rand.NewSource(int64(i))), fmt.Sprintf("rcomp%d", i))
		m2, rep2, err2 := r2.BuildCompositional()
		checkReport(fmt.Sprintf("BuildCompositional %d", i), m2, rep2, err2)
		r3 := randomDecomposable(rand.New(rand.NewSource(int64(i))), fmt.Sprintf("rcoll%d", i))
		cm, rep3, err3 := NewCollection[int]("Key", r3).Build()
		checkReport(fmt.Sprintf("Collection %d", i), cm, rep3, err3)
		if i < 60 {
			r4 := randomDecomposable(rand.New(rand.NewSource(int64(i))), fmt.Sprintf("rpad%d", i))
			pad(r4)
			m4, rep4, err4 := r4.Build()
			checkReport(fmt.Sprintf("Build padded %d", i), m4, rep4, err4)
		}

		// A random two-registry federation: a source drives one target variable; sometimes
		// a back edge (with the cycle opt-in), sometimes RequireProjectionSafe.
		src := randomDecomposable(rand.New(rand.NewSource(int64(1000+i))), fmt.Sprintf("src%d", i))
		dst := randomDecomposable(rand.New(rand.NewSource(int64(2000+i))), fmt.Sprintf("dst%d", i))
		x, y := src.vars[rng.Intn(len(src.vars))], dst.vars[rng.Intn(len(dst.vars))]
		// Clamping a source value into the target's range is monotone in the source.
		clamp := func(v uint64, to Var) uint64 { return min(v, uint64(to.domain-1)) }
		f := NewFederation(fmt.Sprintf("fed%d", i)).Morphism(src, dst).Shared(y).
			Map(func(s, d State) State { return d.setRaw(y, clamp(s.getRaw(x), y)) }).Add()
		if rng.Intn(3) == 0 {
			x2, y2 := dst.vars[rng.Intn(len(dst.vars))], src.vars[rng.Intn(len(src.vars))]
			if y2.index != x.index {
				f.Morphism(dst, src).Shared(y2).
					Map(func(s, d State) State { return d.setRaw(y2, clamp(s.getRaw(x2), y2)) }).Add()
				f.AllowMonotoneCycles()
			}
		}
		if rng.Intn(3) == 0 {
			f.RequireProjectionSafe()
		}
		fm, frep, ferr := f.Build()
		checkFed(fmt.Sprintf("Federation %d", i), fm, frep, ferr)
	}
	t.Logf("%d random registries built, %d failed; %d random federations built (%d projection-safe, %d cyclic)",
		built, failed, fedBuilt, fedSafe, fedCyclic)
	if built < n/10 || failed < n/10 {
		t.Fatalf("too few of each: %d built, %d failed", built, failed)
	}

	// The shaped configurations, through the same guard.
	for _, f := range []*Federation{chainFed("g1"), chainFed("g2").RequireProjectionSafe(), accessControlFed(), regimeMesh()} {
		fm, frep, ferr := f.Build()
		checkFed(f.name, fm, frep, ferr)
	}
	{
		a := NewRegistry("A")
		fa := a.Int("fa", 0, 1)
		b := NewRegistry("B")
		fb := b.Int("fb", 0, 1)
		loop := NewFederation("loop").
			Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
			Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
		fm, frep, ferr := loop.BuildCoordinated(loop.CoordinationPlan())
		checkFed("loop coordinated", fm, frep, ferr)
		fm, frep, ferr = loop.Build()
		checkFed("loop uncoordinated", fm, frep, ferr)
	}
	ff, _, _, _, _ := flipFederation()
	fm, frep, ferr := ff.Build()
	checkFed("flips", fm, frep, ferr)
	ff2, _, _, _, _ := flipFederation()
	fm, frep, ferr = ff2.RequireProjectionSafe().Build()
	checkFed("flips required", fm, frep, ferr)
	m, rep, err := wideCounters(3).BuildCompositional(TrustClosureFootprints())
	checkReport("wide counters tested", m, rep, err)
}

// TestRegime_CitesOnlyAllowlistedTheorems: every theorem name the summaries can cite is in
// the allowlist. checkRegimeShape asserts it for each summary the shape tests build; this
// test asserts it for every name the code can emit, read from the string literals of
// regime.go (the summaries) and of migration.go's Theorems (the migration outcome, which the
// migration summary cites).
func TestRegime_CitesOnlyAllowlistedTheorems(t *testing.T) {
	lit := regexp.MustCompile(`"([a-zA-Z][a-zA-Z0-9]*_[a-zA-Z0-9_]+)"`)
	for _, file := range []string{"regime.go", "migration.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if file == "migration.go" {
			// Only the outcome's citations: the rep.Theorems assignments.
			var keep []string
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "rep.Theorems") || strings.HasPrefix(strings.TrimSpace(line), "\"gsm_closure_exact\"") {
					keep = append(keep, line)
				}
			}
			text = strings.Join(keep, "\n")
		}
		found := 0
		for _, m := range lit.FindAllStringSubmatch(text, -1) {
			found++
			if !regimeTheorems[m[1]] {
				t.Errorf("%s cites %q, which is not in the allowlist", file, m[1])
			}
		}
		if found == 0 {
			t.Errorf("%s: no theorem literals found; the scan is broken", file)
		}
	}
	for name := range regimeTheorems {
		if !snakeToken.MatchString(name) {
			t.Errorf("allowlist entry %q does not look like a theorem name", name)
		}
	}
}
