package gsm

import (
	"fmt"
	"strings"
	"testing"
)

// Tests for certified execution: an EmbedCertified sub's internal targets run from the
// certificate's verified tables (certificate_exec.go), not the live closures.

// certFixture is a certified sub-federation for the differential test.
type certFixture struct {
	name string
	sub  *Federation
	cert *Certificate
}

func mustCertify(t testing.TB, sub *Federation, ports ...Port) *Certificate {
	t.Helper()
	cert, err := sub.Certify(ports...)
	if err != nil {
		t.Fatalf("Certify(%s): %v", sub.name, err)
	}
	return cert
}

// twoBits returns a registry with two Bool variables raised by commuting events, read as the
// number 2*hi+lo in 0..3, and a function reading that number.
func twoBits(name string) (*Registry, func(State) int) {
	r := NewRegistry(name)
	hi := r.Bool("hi")
	lo := r.Bool("lo")
	r.DeclEvent("raise_hi", Raise(hi))
	r.DeclEvent("raise_lo", Raise(lo))
	return r, func(s State) int {
		v := 0
		if s.GetBool(hi) {
			v += 2
		}
		if s.GetBool(lo) {
			v++
		}
		return v
	}
}

// chainSub: a(0..3 from two flags) → b(y 0..3 shared, local flag) → c(z lo/hi shared, local n
// with an invariant), a three-component chain with an enum image and a repaired local variable.
func chainSub(t testing.TB) *Federation {
	a, x := twoBits("a")
	b := NewRegistry("b")
	y := b.Int("y", 0, 3)
	flag := b.Bool("flag")
	b.DeclEvent("raise", Raise(flag))
	c := NewRegistry("c")
	z := c.Enum("z", "lo", "hi")
	n := c.Int("n", 0, 2)
	c.DeclInvariant("not_one", IsNot(n, 1), SetTo(n, 2))
	c.DeclEvent("one", SetTo(n, 1))
	return NewFederation("chain").
		Morphism(a, b).Shared(y).Map(func(s, d State) State { return d.SetInt(y, x(s)) }).Add().
		Morphism(b, c).Shared(z).Map(func(s, d State) State {
		if s.GetInt(y) >= 2 {
			return d.Set(z, "hi")
		}
		return d.Set(z, "lo")
	}).Add()
}

// maxSub: p(0..3), q(0..3) → t(m 0..3 = max, local flag) through a resolver.
func maxSub(t testing.TB) *Federation {
	p, pv := twoBits("p")
	q, qv := twoBits("q")
	tg := NewRegistry("t")
	m := tg.Int("m", 0, 3)
	loc := tg.Bool("loc")
	tg.DeclEvent("touch", Toggle(loc))
	keep := func(_, d State) State { return d }
	return NewFederation("max").
		Morphism(p, tg).Shared(m).Map(keep).Add().
		Morphism(q, tg).Shared(m).Map(keep).Add().
		Resolve(tg, func(d State, src map[string]State) State {
			a, b := pv(src["p"]), qv(src["q"])
			if b > a {
				a = b
			}
			return d.SetInt(m, a)
		})
}

func certFixtures(t *testing.T) []certFixture {
	t.Helper()
	var out []certFixture
	add := func(name string, sub *Federation, ports ...Port) {
		out = append(out, certFixture{name: name, sub: sub, cert: mustCertify(t, sub, ports...)})
	}
	pc, _, _, _, _ := buildPricingCatalogSub()
	add("pricing-catalog", pc)
	good, _, cert := embedNonSharedSubs(t)
	out = append(out, certFixture{name: "src-dst-local", sub: good, cert: cert})
	add("chain", chainSub(t))
	add("max-resolver", maxSub(t))

	// The input-port sub (TestEmbedCertified_InputPortAccepted): an isolated port component.
	pricing := NewRegistry("pricing")
	tier := pricing.Int("tier", 0, 1)
	pricing.DeclEvent("upgrade", SetTo(tier, 1))
	catalog := NewRegistry("catalog")
	badge := catalog.Int("badge", 0, 1)
	inbox := NewRegistry("inbox")
	msg := inbox.Int("msg", 0, 1)
	ports := NewFederation("ports").Add(inbox).
		Morphism(pricing, catalog).Shared(badge).
		Map(func(s, d State) State { return d.SetInt(badge, s.GetInt(tier)) }).Add()
	add("input-port", ports, Port{Registry: inbox, Var: msg})
	return out
}

// verifiedStates lists the valid states of a table machine (the states the certificate covers).
func verifiedStates(m *Machine) []State {
	var out []State
	for p := range m.nf {
		if m.isVerifiedState(uint64(p)) {
			out = append(out, State{packed: uint64(p), vars: m.vars})
		}
	}
	return out
}

// domainStates lists every in-domain encoding of a table machine, valid or not.
func domainStates(m *Machine) []State {
	var out []State
	for p := range m.nf {
		if m.valid[p] {
			out = append(out, State{packed: uint64(p), vars: m.vars})
		}
	}
	return out
}

// forEachFedState calls fn on every FedState whose components range over sets (a product).
func forEachFedState(sets [][]State, fn func(FedState)) {
	if err := forEachCombo(sets, func(cs []State) error {
		fn(FedState{states: append([]State(nil), cs...)})
		return nil
	}); err != nil {
		panic(err)
	}
}

// TestCertifiedTables_DifferentialVsClosures: for every certified fixture, the machine built with
// EmbedCertified (tables) and the one built with Embed (closures) agree on
//   - repair of every table target, at every valid source combination x every valid target state;
//   - Normalize, IsValid, and Apply of every event of every component, at every FedState in the
//     product of the components' in-domain states (valid or not);
//   - SharedProjection along every single-source morphism, at every valid source state.
func TestCertifiedTables_DifferentialVsClosures(t *testing.T) {
	total := 0
	for _, fx := range certFixtures(t) {
		t.Run(fx.name, func(t *testing.T) {
			tab, rep, err := NewFederation("outer").EmbedCertified(fx.sub, fx.cert).Build()
			if err != nil {
				t.Fatalf("EmbedCertified Build: %v", err)
			}
			clo, _, err := NewFederation("outer").Embed(fx.sub).Build()
			if err != nil {
				t.Fatalf("Embed Build: %v", err)
			}
			if got := strings.Join(rep.Runtime, "\n"); !strings.Contains(got, "executes verified tables: "+
				fmt.Sprint(len(fx.cert.Tables))+" internal target(s)") {
				t.Fatalf("Runtime should say the sub executes its %d tables, got:\n%s", len(fx.cert.Tables), got)
			}
			if clo.certTab != nil {
				t.Fatal("the Embed machine has certificate tables")
			}
			tables, checks := 0, 0

			// Repair: every table target, every valid source combination x valid target state.
			for j, ct := range tab.certTab {
				if ct == nil {
					continue
				}
				tables++
				sets := make([][]State, len(tab.comps))
				for i, c := range tab.comps {
					sets[i] = []State{c.Normalize(c.NewState())}
				}
				for _, si := range ct.srcs {
					sets[si] = verifiedStates(tab.comps[si])
				}
				sets[j] = verifiedStates(tab.comps[j])
				forEachFedState(sets, func(fs FedState) {
					checks++
					if got, want := tab.repair(fs, j), clo.repair(fs, j); got.packed != want.packed {
						t.Fatalf("repair of %q at %v: table %s, closure %s", tab.comps[j].name, fs.states, got, want)
					}
				})
			}
			if tables != len(fx.cert.Tables) {
				t.Fatalf("%d table targets, want %d", tables, len(fx.cert.Tables))
			}

			// Normalize, IsValid, Apply: the product of in-domain states, every event.
			sets := make([][]State, len(tab.comps))
			for i, c := range tab.comps {
				sets[i] = domainStates(c)
			}
			forEachFedState(sets, func(fs FedState) {
				checks++
				if got, want := tab.Normalize(fs), clo.Normalize(fs); !sameFed(got, want) {
					t.Fatalf("Normalize(%v): table %v, closure %v", fs.states, got.states, want.states)
				}
				if got, want := tab.IsValid(fs), clo.IsValid(fs); got != want {
					t.Fatalf("IsValid(%v): table %v, closure %v", fs.states, got, want)
				}
				for _, c := range tab.comps {
					r := tab.byName[c.name]
					for _, ev := range c.Events() {
						checks++
						if got, want := tab.Apply(fs, r, ev), clo.Apply(fs, r, ev); !sameFed(got, want) {
							t.Fatalf("Apply(%v, %s.%s): table %v, closure %v", fs.states, c.name, ev, got.states, want.states)
						}
					}
				}
			})

			// SharedProjection along every morphism.
			for _, e := range tab.edges {
				src, dst := tab.byName[tab.comps[e.src].name], tab.byName[tab.comps[e.dst].name]
				for _, s := range verifiedStates(tab.comps[e.src]) {
					checks++
					got, gerr := tab.SharedProjection(s, src, dst)
					want, werr := clo.SharedProjection(s, src, dst)
					if gerr != nil || werr != nil || fmt.Sprint(got) != fmt.Sprint(want) {
						t.Fatalf("SharedProjection(%s) %s→%s: table %v (%v), closure %v (%v)",
							s, src.name, dst.name, got, gerr, want, werr)
					}
				}
			}
			t.Logf("%s: %d table target(s), %d comparisons", fx.name, tables, checks)
			total += checks
		})
	}
	t.Logf("total comparisons: %d", total)
}

func sameFed(a, b FedState) bool {
	if len(a.states) != len(b.states) {
		return false
	}
	for i := range a.states {
		if a.states[i].packed != b.states[i].packed {
			return false
		}
	}
	return true
}

// countingChain is the chain sub with Maps that count their calls.
func countingChain(calls *int) *Federation {
	a := NewRegistry("a")
	x := a.Int("x", 0, 3)
	a.DeclEvent("inc", SetTo(x, 3))
	b := NewRegistry("b")
	y := b.Int("y", 0, 3)
	b.DeclEvent("noop", Copy(y, y))
	return NewFederation("counting").
		Morphism(a, b).Shared(y).Map(func(s, d State) State { *calls++; return d.SetInt(y, s.GetInt(x)) }).Add()
}

// TestCertifiedTables_ClosuresNotRun: once built, a certified sub's Apply, Normalize, IsValid and
// SharedProjection call no Map; the same sub embedded with Embed calls it on every repair. A side
// effect in a certified closure therefore does not happen at run time.
func TestCertifiedTables_ClosuresNotRun(t *testing.T) {
	var calls int
	sub := countingChain(&calls)
	cert := mustCertify(t, sub)
	a, b := sub.comps[0], sub.comps[1]
	m, _, err := NewFederation("outer").EmbedCertified(sub, cert).Build()
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	s := m.Apply(m.NewState(), a, "inc")
	_ = m.IsValid(s)
	_ = m.Normalize(s)
	if _, err = m.SharedProjection(m.Of(s, a), a, b); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("the certified machine called the Map %d times; it should run the certificate's table", calls)
	}
	if got := m.Of(s, b).GetInt(b.vars[0]); got != 3 {
		t.Fatalf("b.y = %d, want 3", got)
	}

	full, _, err := NewFederation("outer").Embed(sub).Build()
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	_ = full.Apply(full.NewState(), a, "inc")
	if calls == 0 {
		t.Fatal("the Embed machine should run the Map (baseline)")
	}
}

// TestCertifiedTables_OutsideDomainPanics: a FedState with a target outside the certified domain
// (an encoding outside the variable's domain) panics with a message naming the state and the
// certificate, and IsValid reports false rather than panic.
func TestCertifiedTables_OutsideDomainPanics(t *testing.T) {
	// An in-range encoding that is not in the variable's domain: dst n 0..2 has a 2-bit field.
	sub2, _, dst := staleSub(t, false)
	cert2 := mustCertify(t, sub2)
	m2, _, err := NewFederation("outer").EmbedCertified(sub2, cert2).Build()
	if err != nil {
		t.Fatal(err)
	}
	fs := m2.NewState()
	j := m2.idx[dst]
	fs.states[j] = rawState(fs.states[j], dst.vars[0], 3)
	if m2.IsValid(fs) {
		t.Fatal("IsValid accepted a target outside the certified domain")
	}
	msg := catchPanic(func() { m2.Normalize(fs) })
	if !strings.Contains(msg, `target "dst"`) || !strings.Contains(msg, "outside the domain the certificate") ||
		!strings.Contains(msg, `sub-federation "sub"`) {
		t.Fatalf("Normalize panic should name the target and the certificate, got %q", msg)
	}
}

// TestCertifiedTables_SourceOutsideDomain: the source-side message, and SharedProjection's error.
func TestCertifiedTables_SourceOutsideDomain(t *testing.T) {
	src := NewRegistry("src")
	x := src.Int("x", 0, 2) // 2-bit field, raw 3 is out of domain
	src.DeclEvent("two", SetTo(x, 2))
	dst := NewRegistry("dst")
	y := dst.Int("y", 0, 2)
	sub := NewFederation("sub").Morphism(src, dst).Shared(y).
		Map(func(s, d State) State { return d.SetInt(y, s.GetInt(x)) }).Add()
	cert := mustCertify(t, sub)
	m, _, err := NewFederation("outer").EmbedCertified(sub, cert).Build()
	if err != nil {
		t.Fatal(err)
	}
	fs := m.NewState()
	fs.states[m.idx[src]] = rawState(fs.states[m.idx[src]], x, 3)
	msg := catchPanic(func() { m.Normalize(fs) })
	if !strings.Contains(msg, `source "src"`) || !strings.Contains(msg, "outside the domain the certificate") {
		t.Fatalf("Normalize panic should name the source, got %q", msg)
	}
	if _, err := m.SharedProjection(fs.states[m.idx[src]], src, dst); err == nil ||
		!strings.Contains(err.Error(), "outside the domain the certificate") {
		t.Fatalf("SharedProjection on an uncovered source = %v, want an error", err)
	}
}

// TestCertifiedTables_CertificateMutationAfterBuild: the machine runs the tables Build checked; a
// later change to the Certificate does not reach it.
func TestCertifiedTables_CertificateMutationAfterBuild(t *testing.T) {
	sub, pricing, catalog, _, badge := buildPricingCatalogSub()
	cert := mustCertify(t, sub)
	m, _, err := NewFederation("outer").EmbedCertified(sub, cert).Build()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cert.Tables[0].Rows {
		cert.Tables[0].Rows[i].Values[0] = 0
	}
	s := m.Apply(m.NewState(), pricing, "upgrade")
	if got := m.Of(s, catalog).GetInt(badge); got != 1 {
		t.Fatalf("badge = %d, want 1: a change to the certificate after Build reached the machine", got)
	}
}

// TestCertifiedTables_TablesBoundToDigest: tables changed without re-issuing the digest are
// refused, since they are what would run.
func TestCertifiedTables_TablesBoundToDigest(t *testing.T) {
	sub, _, _, _, _ := buildPricingCatalogSub()
	cert := mustCertify(t, sub)
	cert.Tables[0].Rows[1].Values[0] = 0 // tier=1 now maps to badge=0; digest left as issued
	if _, _, err := NewFederation("outer").EmbedCertified(sub, cert).Build(); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Build with tables the digest does not cover = %v, want a mismatch", err)
	}
}

// TestCertifiedTables_OuterInternalMorphismRejected: a morphism the outer federation adds between
// two components of a certified sub is not in the certificate, so Build refuses it rather than run
// the table without it.
func TestCertifiedTables_OuterInternalMorphismRejected(t *testing.T) {
	sub := maxSub(t)
	cert := mustCertify(t, sub)
	p, tg := sub.comps[0], sub.comps[2]
	outer := NewFederation("outer").EmbedCertified(sub, cert).
		Morphism(p, tg).Shared(tg.vars[0]).Map(func(_, d State) State { return d }).Add()
	if _, _, err := outer.Build(); err == nil || !strings.Contains(err.Error(), "is not one of its morphisms") {
		t.Fatalf("Build = %v, want an error naming the extra morphism", err)
	}
}

// TestCertifiedTables_OuterResolverRejected: a resolver declared outside the sub for a target
// whose sources are all inside it would be replaced by the certificate's table, so it is refused.
func TestCertifiedTables_OuterResolverRejected(t *testing.T) {
	sub, _, catalog, _, badge := buildPricingCatalogSub()
	cert := mustCertify(t, sub)
	outer := NewFederation("outer").EmbedCertified(sub, cert).
		Resolve(catalog, func(d State, _ map[string]State) State { return d.SetInt(badge, 0) })
	if _, _, err := outer.Build(); err == nil || !strings.Contains(err.Error(), "declared outside certified sub-federation") {
		t.Fatalf("Build = %v, want an error naming the outer resolver", err)
	}
}

// TestCertifiedTables_SeamTargetRunsClosures: a certified target that also receives an outer
// morphism (into an input port) is repaired by the outer resolver, verified at the seam; the
// report says the sub runs tables for the other targets only.
func TestCertifiedTables_SeamTargetRunsClosures(t *testing.T) {
	pricing := NewRegistry("pricing")
	tier := pricing.Int("tier", 0, 1)
	pricing.DeclEvent("upgrade", SetTo(tier, 1))
	catalog := NewRegistry("catalog")
	badge := catalog.Int("badge", 0, 1)
	promo := catalog.Int("promo", 0, 1)
	sub := NewFederation("sub").
		Morphism(pricing, catalog).Shared(badge).
		Map(func(s, d State) State { return d.SetInt(badge, s.GetInt(tier)) }).Add()
	cert := mustCertify(t, sub, Port{Registry: catalog, Var: promo})
	geo := NewRegistry("geo")
	loc := geo.Int("loc", 0, 1)
	geo.DeclEvent("set", SetTo(loc, 1))
	outer := NewFederation("outer").EmbedCertified(sub, cert).
		Morphism(geo, catalog).Shared(promo).
		Map(func(s, d State) State { return d.SetInt(promo, s.GetInt(loc)) }).Add().
		Resolve(catalog, func(d State, src map[string]State) State {
			return d.SetInt(badge, src["pricing"].GetInt(tier)).SetInt(promo, src["geo"].GetInt(loc))
		})
	m, rep, err := outer.Build()
	if err != nil {
		t.Fatal(err)
	}
	if m.certTab[m.idx[catalog]] != nil {
		t.Fatal("a target with a seam writer must run its (seam-verified) resolver, not a table")
	}
	if got := strings.Join(rep.Runtime, "\n"); !strings.Contains(got, "for 0 of its 1 internal target(s)") {
		t.Fatalf("Runtime should say the seam target runs closures, got:\n%s", got)
	}
	s := m.Apply(m.Apply(m.NewState(), geo, "set"), pricing, "upgrade")
	if m.Of(s, catalog).GetInt(badge) != 1 || m.Of(s, catalog).GetInt(promo) != 1 {
		t.Fatalf("catalog = %s, want badge=1 promo=1", m.Of(s, catalog))
	}
}

// TestCertifiedTables_CyclicRunsClosures: on a cyclic (monotone) network the Kleene iteration
// visits states the tables do not cover, so the closures run and the report says why.
func TestCertifiedTables_CyclicRunsClosures(t *testing.T) {
	a := NewRegistry("A")
	av := a.Bool("a")
	a.DeclEvent("raise_a", Raise(av))
	b := NewRegistry("B")
	bv := b.Bool("b")
	b.DeclEvent("raise_b", Raise(bv))
	sub := NewFederation("cyc").AllowMonotoneCycles().
		Morphism(a, b).Shared(bv).Map(func(s, d State) State { return d.SetBool(bv, s.GetBool(av)) }).Add().
		Morphism(b, a).Shared(av).Map(func(s, d State) State { return d.SetBool(av, s.GetBool(bv)) }).Add()
	cert := mustCertify(t, sub)
	m, rep, err := NewFederation("outer").AllowMonotoneCycles().EmbedCertified(sub, cert).Build()
	if err != nil {
		t.Fatal(err)
	}
	for j, ct := range m.certTab {
		if ct != nil {
			t.Fatalf("a cyclic network runs a table for %q", m.comps[j].name)
		}
	}
	if got := strings.Join(rep.Runtime, "\n"); !strings.Contains(got, "runs its morphism and resolver closures") {
		t.Fatalf("Runtime should say the cyclic sub runs closures, got:\n%s", got)
	}
	if !strings.Contains(rep.String(), "runtime: certified sub-federation") {
		t.Fatalf("String() should print the runtime lines:\n%s", rep)
	}
}

// TestFedReport_NoRuntimeWithoutCertificates: a federation with no certified embed has no
// Runtime lines.
func TestFedReport_NoRuntimeWithoutCertificates(t *testing.T) {
	_, rep, err := chainSub(t).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Runtime) != 0 {
		t.Fatalf("Runtime = %q, want none", rep.Runtime)
	}
}

// Benchmarks: the same sub (the max resolver, and the three-component chain) run from the
// certificate's tables (EmbedCertified) and from its closures (Embed).

func benchFed(b *testing.B, sub *Federation, certified bool) *FedMachine {
	b.Helper()
	f := NewFederation("outer")
	if certified {
		f.EmbedCertified(sub, mustCertify(b, sub))
	} else {
		f.Embed(sub)
	}
	m, _, err := f.Build()
	if err != nil {
		b.Fatal(err)
	}
	return m
}

func benchFedApply(b *testing.B, sub *Federation, certified bool, reg string, events []string) {
	m := benchFed(b, sub, certified)
	r := m.byName[reg]
	s := m.NewState()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = m.Apply(s, r, events[i%len(events)])
	}
}

func benchFedRepair(b *testing.B, sub *Federation, certified bool, target string) {
	m := benchFed(b, sub, certified)
	j := m.idx[m.byName[target]]
	fs := m.Normalize(m.NewState())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.states[j] = m.repair(fs, j)
	}
}

var benchSetEvents = []string{"raise_lo", "raise_hi"}

func BenchmarkFedApply_MaxResolver_Tables(b *testing.B) {
	benchFedApply(b, maxSub(b), true, "p", benchSetEvents)
}
func BenchmarkFedApply_MaxResolver_Closures(b *testing.B) {
	benchFedApply(b, maxSub(b), false, "p", benchSetEvents)
}
func BenchmarkFedApply_Chain_Tables(b *testing.B) {
	benchFedApply(b, chainSub(b), true, "a", benchSetEvents)
}
func BenchmarkFedApply_Chain_Closures(b *testing.B) {
	benchFedApply(b, chainSub(b), false, "a", benchSetEvents)
}
func BenchmarkFedRepair_MaxResolver_Tables(b *testing.B)   { benchFedRepair(b, maxSub(b), true, "t") }
func BenchmarkFedRepair_MaxResolver_Closures(b *testing.B) { benchFedRepair(b, maxSub(b), false, "t") }
func BenchmarkFedRepair_Single_Tables(b *testing.B)        { benchFedRepair(b, chainSub(b), true, "b") }
func BenchmarkFedRepair_Single_Closures(b *testing.B)      { benchFedRepair(b, chainSub(b), false, "b") }
