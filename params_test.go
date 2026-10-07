package gsm_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "github.com/blackwell-systems/gsm"
)

// Events with parameters (params.go): a parameterized event is the family of its
// instances, each checked as an ordinary event, or under Abstract over representative
// parameter values.

// panicMsg runs fn and returns its panic message, failing if it does not panic.
func panicMsg(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		msg = r.(string)
	}()
	fn()
	return ""
}

// naiveWallet: deposit(amount) adds, withdraw(amount) subtracts when the balance covers it.
func naiveWallet() *Registry {
	r := NewRegistry("wallet")
	bal := r.Int("balance", 0, 10)
	r.On("deposit").Param("amount", 1, 3).Does(IncByArg(bal, "amount")).Add()
	r.On("withdraw").Param("amount", 1, 3).OnlyIf(Ge(V(bal), Arg("amount"))).Does(DecByArg(bal, "amount")).Add()
	return r
}

func TestParams_NaiveWalletFailsWithInstanceWitness(t *testing.T) {
	_, rep, err := naiveWallet().Build()
	if err == nil {
		t.Fatal("the guarded withdraw against deposit should fail CC")
	}
	f := rep.CCFailure
	if f == nil || f.Event1 != "deposit(1)" || f.Event2 != "withdraw(1)" {
		t.Fatalf("CCFailure = %+v, want the pair (deposit(1), withdraw(1))", f)
	}
	if rep.EventCount != 6 {
		t.Errorf("EventCount = %d, want 6 instances", rep.EventCount)
	}
	if !strings.Contains(rep.String(), "Events: (deposit(1), withdraw(1))") {
		t.Errorf("report does not name the instances:\n%s", rep)
	}
}

// factWallet records facts that only rise (deposited, requested) and derives the
// overdraft flag, the fact-recording encoding of a wallet.
func factWallet(max int) (*Registry, Var, Var, Var) {
	r := NewRegistry("ledger")
	dep := r.Int("deposited", 0, max)
	req := r.Int("requested", 0, max)
	over := r.Int("overdraft", 0, 1)
	r.On("deposit").Param("amount", 1, 3).Does(IncByArg(dep, "amount")).Add()
	r.On("withdraw").Param("amount", 1, 3).Does(IncByArg(req, "amount")).Add()
	r.Rule("flagon").Require(Or(Le(V(req), V(dep)), Is(over, 1))).RepairWith(SetTo(over, 1)).Add()
	r.Rule("flagoff").Require(Or(Gt(V(req), V(dep)), Is(over, 0))).RepairWith(SetTo(over, 0)).Add()
	return r, dep, req, over
}

func TestParams_FactWalletPasses(t *testing.T) {
	r, dep, req, over := factWallet(12)
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	want := []string{"deposit(1)", "deposit(2)", "deposit(3)", "withdraw(1)", "withdraw(2)", "withdraw(3)"}
	if !reflect.DeepEqual(rep.NotIdempotent, want) {
		t.Errorf("NotIdempotent = %v, want %v", rep.NotIdempotent, want)
	}
	if len(rep.Families) != 2 || rep.Families[1].Signature() != "withdraw(amount)" || rep.Families[1].Instances != 3 ||
		len(rep.Families[1].NotIdempotent) != 3 {
		t.Errorf("Families = %+v", rep.Families)
	}
	if out := rep.String(); !strings.Contains(out, "exactly once for deposit(amount) (every value), withdraw(amount) (every value)") {
		t.Errorf("report does not group the instances:\n%s", out)
	}
	// Saturation near the bounds: the counters clamp at 12, so an overdraft past the range
	// is not seen. Build reports it.
	if len(rep.Saturations) == 0 {
		t.Error("expected saturations near the bounds")
	}
	s := m.ApplyWith(m.NewState(), "deposit", 2)
	s = m.ApplyWith(s, "withdraw", 3)
	if s.GetInt(dep) != 2 || s.GetInt(req) != 3 || s.GetInt(over) != 1 {
		t.Errorf("state %s", s)
	}
	if got := m.Apply(s, Instance("deposit", 1)); got.GetInt(over) != 0 {
		t.Errorf("deposit(1) should clear the overdraft: %s", got)
	}
	// Every permutation reaches one state.
	a := m.ApplyWith(m.ApplyWith(m.ApplyWith(m.NewState(), "withdraw", 3), "deposit", 1), "deposit", 2)
	b := m.ApplyWith(m.ApplyWith(m.ApplyWith(m.NewState(), "deposit", 2), "deposit", 1), "withdraw", 3)
	if a.ID() != b.ID() {
		t.Errorf("orders disagree: %s vs %s", a, b)
	}
	if ps, ok := m.Params("withdraw"); !ok || !reflect.DeepEqual(ps, []Param{{Name: "amount", Min: 1, Max: 3}}) {
		t.Errorf("Params = %v, %v", ps, ok)
	}
	if !reflect.DeepEqual(m.Families(), []string{"deposit", "withdraw"}) {
		t.Errorf("Families = %v", m.Families())
	}
}

func TestParams_InventoryReserveRestock(t *testing.T) {
	// Naive: reserve qty only when stock covers it; restock adds. Fails.
	naive := NewRegistry("inventory")
	stock := naive.Int("stock", 0, 8)
	naive.On("restock").Param("qty", 1, 2).Does(IncByArg(stock, "qty")).Add()
	naive.On("reserve").Param("qty", 1, 2).OnlyIf(Ge(V(stock), Arg("qty"))).Does(DecByArg(stock, "qty")).Add()
	if _, rep, err := naive.Build(); err == nil || rep.CCFailure == nil ||
		!strings.HasPrefix(rep.CCFailure.Event1, "restock(") || !strings.HasPrefix(rep.CCFailure.Event2, "reserve(") {
		t.Fatalf("naive inventory: %v %+v", err, rep)
	}
	// Facts: received and reserved only rise; short is derived.
	r := NewRegistry("inventory")
	recv, resv, short := r.Int("received", 0, 9), r.Int("reserved", 0, 9), r.Int("short", 0, 1)
	r.On("restock").Param("qty", 1, 2).Does(IncByArg(recv, "qty")).Add()
	r.On("reserve").Param("qty", 1, 2).Does(IncByArg(resv, "qty")).Add()
	r.Rule("shorton").Require(Or(Le(V(resv), V(recv)), Is(short, 1))).RepairWith(SetTo(short, 1)).Add()
	r.Rule("shortoff").Require(Or(Gt(V(resv), V(recv)), Is(short, 0))).RepairWith(SetTo(short, 0)).Add()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	s := m.ApplyWith(m.NewState(), "reserve", 2)
	if s.GetInt(short) != 1 {
		t.Errorf("reserve before restock should be short: %s", s)
	}
	if s = m.ApplyWith(s, "restock", 2); s.GetInt(short) != 0 {
		t.Errorf("restock should cover it: %s", s)
	}
}

func TestParams_BookingUnderCap(t *testing.T) {
	const cap = 4
	// Naive: book n only while it fits the cap; cancel n frees n. Fails.
	naive := NewRegistry("booking")
	held := naive.Int("held", 0, cap)
	naive.On("book").Param("n", 1, 2).OnlyIf(Le(Add(V(held), Arg("n")), Lit(cap))).Does(IncByArg(held, "n")).Add()
	naive.On("cancel").Param("n", 1, 2).OnlyIf(Ge(V(held), Arg("n"))).Does(DecByArg(held, "n")).Add()
	if _, _, err := naive.Build(); err == nil {
		t.Fatal("naive booking should fail CC")
	}
	// Facts: booked and cancelled only rise; overbooked is derived from their difference.
	r := NewRegistry("booking")
	booked, cancelled, overbooked := r.Int("booked", 0, 10), r.Int("cancelled", 0, 10), r.Int("overbooked", 0, 1)
	r.On("book").Param("n", 1, 2).Does(IncByArg(booked, "n")).Add()
	r.On("cancel").Param("n", 1, 2).Does(IncByArg(cancelled, "n")).Add()
	over := Gt(Sub(V(booked), V(cancelled)), Lit(cap))
	r.Rule("overon").Require(Or(Not(over), Is(overbooked, 1))).RepairWith(SetTo(overbooked, 1)).Add()
	r.Rule("overoff").Require(Or(over, Is(overbooked, 0))).RepairWith(SetTo(overbooked, 0)).Add()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	s := m.ApplyWith(m.ApplyWith(m.ApplyWith(m.NewState(), "book", 2), "book", 2), "book", 1)
	if s.GetInt(overbooked) != 1 {
		t.Errorf("5 booked over a cap of 4: %s", s)
	}
	if s = m.ApplyWith(s, "cancel", 1); s.GetInt(overbooked) != 0 {
		t.Errorf("a cancellation brings it back under the cap: %s", s)
	}
}

func TestParams_ClosureFamily(t *testing.T) {
	r := NewRegistry("closures")
	n := r.Int("n", 0, 6)
	r.Event("raise").Param("to", 0, 6).Writes(n).
		GuardArgs(func(s State, a Args) bool { return s.GetInt(n) < a["to"] }).
		ApplyArgs(func(s State, a Args) State { return s.SetInt(n, a["to"]) }).Add()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if len(rep.NotIdempotent) != 0 {
		t.Errorf("a max-register is idempotent: %v", rep.NotIdempotent)
	}
	if s := m.ApplyWith(m.ApplyWith(m.NewState(), "raise", 5), "raise", 2); s.GetInt(n) != 5 {
		t.Errorf("state %s", s)
	}
}

func TestParams_Errors(t *testing.T) {
	// A family too wide for Build without Abstract.
	r := NewRegistry("wide")
	x := r.Int("x", 0, 3)
	r.On("set").Param("v", 0, 5000).OnlyIf(Lt(V(x), Arg("v"))).Does(SetToArg(x, "v")).Add()
	_, _, err := r.Build()
	if err == nil || !strings.Contains(err.Error(), "above the limit of 1024 instances") ||
		!strings.Contains(err.Error(), "declare Abstract") {
		t.Fatalf("wide family: %v", err)
	}
	if _, err := r.Synthesize(); err == nil {
		t.Error("Synthesize accepted a wide family")
	}
	// Reading an undeclared parameter, or a parameter in an invariant.
	r2 := NewRegistry("bad")
	y := r2.Int("y", 0, 3)
	if msg := panicMsg(t, func() { r2.On("e").Param("a", 0, 1).Does(SetToArg(y, "b")).Add() }); !strings.Contains(msg, `Arg("b")`) {
		t.Errorf("panic %q", msg)
	}
	if msg := panicMsg(t, func() { r2.On("e").Does(SetToArg(y, "a")).Add() }); !strings.Contains(msg, "only an event that declares") {
		t.Errorf("panic %q", msg)
	}
	if msg := panicMsg(t, func() { r2.Rule("inv").Require(Le(V(y), Arg("a"))).RepairWith(SetTo(y, 0)).Add() }); !strings.Contains(msg, "invariant") {
		t.Errorf("panic %q", msg)
	}
	panicMsg(t, func() { r2.On("e").Param("a", 2, 1) })
	panicMsg(t, func() { r2.On("e").Param("a", 0, 1).Param("a", 0, 1) })
	panicMsg(t, func() { r2.Event("c").Writes(y).ApplyArgs(func(s State, _ Args) State { return s }).Add() })
	// A family and an event of one name.
	r3 := NewRegistry("dup")
	z := r3.Int("z", 0, 3)
	r3.On("e").Does(Inc(z)).Add()
	r3.On("e").Param("a", 0, 1).Does(SetToArg(z, "a")).Add()
	if _, _, err := r3.Build(); err == nil || !strings.Contains(err.Error(), "duplicate event name") {
		t.Errorf("duplicate: %v", err)
	}
	// ApplyWith misuse.
	f, _, _, _ := factWallet(6)
	m, _, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	s := m.NewState()
	for _, c := range []struct {
		fn   func()
		want string
	}{
		{func() { m.ApplyWith(s, "deposit") }, "takes 1 argument"},
		{func() { m.ApplyWith(s, "deposit", 9) }, "outside 1..3"},
		{func() { m.ApplyWith(s, "nope", 1) }, "unknown event"},
		{func() { m.Apply(s, "deposit(9)") }, "outside 1..3"},
		{func() { m.Apply(s, "deposit") }, "unknown event"},
	} {
		if msg := panicMsg(t, c.fn); !strings.Contains(msg, c.want) {
			t.Errorf("panic %q, want %q", msg, c.want)
		}
	}
}

func TestParams_Independent(t *testing.T) {
	r := NewRegistry("indep")
	a, b := r.Int("a", 0, 4), r.Int("b", 0, 4)
	r.On("seta").Param("v", 0, 2).OnlyIf(Lt(V(a), Arg("v"))).Does(SetToArg(a, "v")).Add()
	r.On("setb").Param("v", 0, 2).Does(SetToArg(b, "v")).Add()
	r.Independent("seta", "setb").Independent("seta", "seta")
	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	// Declared: 3*3 cross pairs and 3 within seta. Undeclared: the 3 pairs within setb,
	// which overwrite b in either order and so need causal order.
	if rep.PairsTotal != 12 || rep.PairsUndeclared != 3 || len(rep.CausalOrderRequired) != 3 {
		t.Fatalf("pairs %d, undeclared %d, causal %d", rep.PairsTotal, rep.PairsUndeclared, len(rep.CausalOrderRequired))
	}
	if f := rep.CausalOrderRequired[0]; f.Event1 != "setb(0)" || f.Event2 != "setb(1)" {
		t.Errorf("causal pair %s, %s", f.Event1, f.Event2)
	}
	// One instance on its own.
	r2 := NewRegistry("indep2")
	c := r2.Int("c", 0, 4)
	r2.On("setc").Param("v", 0, 2).Does(SetToArg(c, "v")).Add()
	r2.Independent(Instance("setc", 0), Instance("setc", 0))
	if _, rep, err := r2.Build(); err != nil || rep.PairsTotal != 1 {
		t.Fatalf("instance Independent: %v %+v", err, rep)
	}
}

// Manual instance events and a parameterized event give the same report.
func TestParams_SameAsManualInstances(t *testing.T) {
	build := func(param bool) (*Report, error) {
		r := NewRegistry("cmp")
		x, y := r.Int("x", 0, 5), r.Int("y", 0, 5)
		if param {
			r.On("put").Param("v", 1, 3).OnlyIf(Lt(V(x), Arg("v"))).Does(Do(Set(x, Arg("v")), Set(y, Add(V(y), Lit(1))))).Add()
		} else {
			for v := 1; v <= 3; v++ {
				r.On(Instance("put", v)).OnlyIf(Lt(V(x), Lit(v))).Does(Do(Set(x, Lit(v)), Set(y, Add(V(y), Lit(1))))).Add()
			}
		}
		r.Rule("cap").Require(AtMost(y, 4)).RepairWith(SetTo(y, 4)).Add()
		_, rep, err := r.Build()
		return rep, err
	}
	p, perr := build(true)
	q, qerr := build(false)
	if (perr == nil) != (qerr == nil) || p.CC != q.CC || p.WFC != q.WFC ||
		!reflect.DeepEqual(p.NotIdempotent, q.NotIdempotent) || p.PairsTotal != q.PairsTotal {
		t.Fatalf("parameterized %v %+v\nmanual %v %+v", perr, p, qerr, q)
	}
	if (p.CCFailure == nil) != (q.CCFailure == nil) || (p.CCFailure != nil &&
		(p.CCFailure.Event1 != q.CCFailure.Event1 || p.CCFailure.State.ID() != q.CCFailure.State.ID())) {
		t.Fatalf("failures differ: %+v vs %+v", p.CCFailure, q.CCFailure)
	}
}

func TestParams_Collection(t *testing.T) {
	item := NewRegistry("sku")
	recv, resv, short := item.Int("received", 0, 6), item.Int("reserved", 0, 6), item.Int("short", 0, 1)
	item.On("restock").Param("qty", 1, 3).Does(IncByArg(recv, "qty")).Add()
	item.On("reserve").Param("qty", 1, 3).Does(IncByArg(resv, "qty")).Add()
	item.Rule("shorton").Require(Or(Le(V(resv), V(recv)), Is(short, 1))).RepairWith(SetTo(short, 1)).Add()
	item.Rule("shortoff").Require(Or(Gt(V(resv), V(recv)), Is(short, 0))).RepairWith(SetTo(short, 0)).Add()
	cm, rep, err := NewCollection[string]("sku", item).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Symmetry == nil || len(rep.Families) != 2 {
		t.Fatalf("report %+v", rep)
	}
	s := cm.NewState()
	cm.ApplyWith(s, "apple", "reserve", 2)
	cm.Apply(s, "pear", Instance("restock", 3))
	if s.Item("apple").GetInt(short) != 1 || s.Item("pear").GetInt(short) != 0 {
		t.Errorf("apple %s, pear %s", s.Item("apple"), s.Item("pear"))
	}
	if msg := panicMsg(t, func() { cm.ApplyWith(s, "apple", "reserve", 7) }); !strings.Contains(msg, "outside 1..3") {
		t.Errorf("panic %q", msg)
	}
}

func TestParams_Federation(t *testing.T) {
	src := NewRegistry("pricing")
	price := src.Int("price", 0, 4)
	src.On("raise").Param("to", 0, 4).OnlyIf(Lt(V(price), Arg("to"))).Does(SetToArg(price, "to")).Add()
	dst := NewRegistry("shop")
	shown := dst.Int("shown", 0, 4)
	dst.On("tick").Does(Do()).Add()
	fed := NewFederation("prices").Morphism(src, dst).Shared(shown).
		Map(func(srcNF, d State) State { return d.SetInt(shown, srcNF.GetInt(price)) }).Add()
	fm, rep, err := fed.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%v", err, rep)
	}
	fs := fm.NewState()
	if fs, err = fm.ApplyNamed(fs, "pricing", Instance("raise", 3)); err != nil {
		t.Fatal(err)
	}
	if fm.Of(fs, dst).GetInt(shown) != 3 {
		t.Errorf("shown %s", fm.Of(fs, dst))
	}
	if fm.Component(src).ApplyWith(fm.Of(fs, src), "raise", 1).GetInt(price) != 3 {
		t.Error("a lower raise changed the price")
	}
}

func TestParams_Migration(t *testing.T) {
	mk := func(name, event string, max int) (*Registry, Var) {
		r := NewRegistry(name)
		x := r.Int("x", 0, 4)
		r.On(event).Param("v", 0, max).OnlyIf(Lt(V(x), Arg("v"))).Does(SetToArg(x, "v")).Add()
		return r, x
	}
	from, x1 := mk("v1", "raise", 3)
	to, x2 := mk("v2", "bump", 4)
	copyX := func(old, blank State) State { return blank.SetInt(x2, old.GetInt(x1)) }
	copyBack := func(old, blank State) State { return blank.SetInt(x1, old.GetInt(x2)) }
	rep, err := CheckMigration(from, to, copyX, map[string]string{"raise": "bump"})
	if err != nil {
		t.Fatalf("CheckMigration: %v", err)
	}
	if rep.Outcome != MigrationSafeOnline {
		t.Errorf("outcome %v\n%s", rep.Outcome, rep)
	}
	// A narrower target range is refused.
	if _, err := CheckMigration(to, from, copyBack, map[string]string{"bump": "raise"}); err == nil ||
		!strings.Contains(err.Error(), "has values outside") {
		t.Errorf("narrower range: %v", err)
	}
}

func TestParams_ExportNamesInstances(t *testing.T) {
	r, _, _, _ := factWallet(5)
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "m.json")
	if err := m.Export(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Version int      `json:"version"`
		Events  []string `json:"events"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != 2 || !reflect.DeepEqual(out.Events[:2], []string{"deposit(1)", "deposit(2)"}) {
		t.Errorf("export %d %v", out.Version, out.Events)
	}
}

// ---- stage 2: parameters under Abstract ----

// cappedRestock is AbstractionCutoff.v's capped inventory with its parameter: Restock(level)
// sets stock := max(stock, level); the invariant stock <= 5 clamps to the cap.
func cappedRestock(hi int) (*Registry, Var) {
	r := NewRegistry("capped")
	stock := r.Int("stock", 0, hi)
	r.Rule("cap").Require(AtMost(stock, 5)).RepairWith(SetTo(stock, 5)).Add()
	r.On("restock").Param("level", 0, hi).OnlyIf(Lt(V(stock), Arg("level"))).Does(SetToArg(stock, "level")).Add()
	return r, stock
}

func TestParamsAbstract_Capped(t *testing.T) {
	r, stock := cappedRestock(1_000_000_000)
	m, rep, err := r.Abstract(5).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	// N = n + 2m = 3. The constants are the declared cap and the parameter's bounds, which the
	// check's range test compares against.
	want := &AbstractionReduction{Over: []string{"stock"}, Constants: []int{0, 5, 1_000_000_000}, Cutoff: 3, Params: 1,
		Representatives: []int{-3, -2, -1, 0, 1, 2, 3, 5, 6, 7, 8, 1_000_000_000, 1_000_000_001, 1_000_000_002,
			1_000_000_003}, States: 15}
	if !reflect.DeepEqual(rep.Abstraction, want) {
		t.Fatalf("Abstraction = %+v", rep.Abstraction)
	}
	if len(rep.NotIdempotent) != 0 || len(rep.Families) != 1 || !rep.Families[0].Representative ||
		rep.Families[0].Instances != 10 {
		t.Fatalf("report %+v", rep)
	}
	if rep.Regime == nil || !strings.Contains(rep.Regime.String(), "every variable and event parameter") {
		t.Errorf("regime %s", rep.Regime)
	}
	s := m.ApplyWith(m.NewState(), "restock", 999_999_999)
	if s.GetInt(stock) != 5 {
		t.Errorf("state %s", s)
	}
	if got := m.Apply(s, Instance("restock", 3)); got.ID() != s.ID() {
		t.Errorf("a lower restock moved the stock: %s", got)
	}
	if msg := panicMsg(t, func() { m.ApplyWith(s, "restock", -1) }); !strings.Contains(msg, "outside 0..1000000000") {
		t.Errorf("panic %q", msg)
	}
}

func TestParamsAbstract_LastWriterWins(t *testing.T) {
	r := NewRegistry("register")
	val := r.Int("value", 0, 1<<20)
	ts := r.Int("ts", 0, 1<<30)
	r.On("write").Param("v", 0, 1<<20).Param("t", 1, 1<<30).
		OnlyIf(Or(Lt(V(ts), Arg("t")), And(Eq(V(ts), Arg("t")), Lt(V(val), Arg("v"))))).
		Does(Do(Set(val, Arg("v")), Set(ts, Arg("t")))).Add()
	m, rep, err := r.Abstract().Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if rep.Abstraction.Cutoff != 6 || len(rep.NotIdempotent) != 0 {
		t.Errorf("report %+v", rep.Abstraction)
	}
	a := m.ApplyWith(m.ApplyWith(m.NewState(), "write", 7, 100), "write", 3, 200)
	b := m.ApplyWith(m.ApplyWith(m.NewState(), "write", 3, 200), "write", 7, 100)
	if a.ID() != b.ID() || a.GetInt(val) != 3 {
		t.Errorf("%s vs %s", a, b)
	}
	// Without the tie-break on equal times, two writes at one time diverge.
	r2 := NewRegistry("register")
	val2, ts2 := r2.Int("value", 0, 1<<20), r2.Int("ts", 0, 1<<30)
	r2.On("write").Param("v", 0, 1<<20).Param("t", 1, 1<<30).
		OnlyIf(Le(V(ts2), Arg("t"))).Does(Do(Set(val2, Arg("v")), Set(ts2, Arg("t")))).Add()
	_, rep2, err := r2.Abstract().Build()
	if err == nil || rep2.CCFailure == nil || rep2.CCFailure.Abstract == nil || !rep2.CCFailure.Abstract.InRange {
		t.Fatalf("no tie-break: %v %+v", err, rep2)
	}
	if !strings.HasPrefix(rep2.CCFailure.Event1, "write(") {
		t.Errorf("witness %s", rep2.CCFailure.Event1)
	}
}

func TestParamsAbstract_NotIdempotentWitness(t *testing.T) {
	r := NewRegistry("shift")
	x, y := r.Int("x", 0, 100), r.Int("y", 0, 100)
	r.On("push").Param("v", 0, 100).Does(Do(Set(y, V(x)), Set(x, Arg("v")))).Add()
	r.Independent("push", "push").OnlyDeclaredPairs()
	_, rep, err := r.Abstract().Build()
	if err == nil {
		t.Fatal("pushes of different values do not commute")
	}
	// Declared-only, with push not declared against itself: it builds, and push is listed.
	r2 := NewRegistry("shift")
	x2, y2 := r2.Int("x", 0, 100), r2.Int("y", 0, 100)
	r2.On("push").Param("v", 0, 100).Does(Do(Set(y2, V(x2)), Set(x2, Arg("v")))).Add()
	r2.On("noop").Does(Do()).Add()
	r2.Independent("push", "noop")
	_, rep, err = r2.Abstract().Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if !reflect.DeepEqual(rep.NotIdempotent, []string{"push(v)"}) || len(rep.Families[0].NotIdempotent) != 1 {
		t.Fatalf("NotIdempotent %v, families %+v", rep.NotIdempotent, rep.Families)
	}
	if len(rep.CausalOrderRequired) != 1 {
		t.Errorf("one witness for the pair (push, push): %d", len(rep.CausalOrderRequired))
	}
	if !strings.Contains(rep.String(), "push(v) (every value; witness push(") {
		t.Errorf("report:\n%s", rep)
	}
}

func TestParamsAbstract_Refusals(t *testing.T) {
	cases := []struct {
		name string
		mk   func(r *Registry)
		want string
	}{
		{"arithmetic", func(r *Registry) {
			b := r.Int("balance", 0, 1000)
			r.On("withdraw").Param("amount", 1, 1000).OnlyIf(Ge(V(b), Arg("amount"))).Does(DecByArg(b, "amount")).Add()
		}, `arithmetic on the parameter "amount"`},
		{"guard arithmetic", func(r *Registry) {
			b := r.Int("held", 0, 1000)
			r.On("book").Param("n", 1, 10).OnlyIf(Le(Add(V(b), Arg("n")), Lit(5))).Does(SetTo(b, 5)).Add()
		}, `arithmetic on the parameter "n"`},
		{"saturating copy", func(r *Registry) {
			x := r.Int("x", 0, 10)
			r.On("set").Param("v", 0, 20).Does(SetToArg(x, "v")).Add()
		}, `copies the parameter "v" (0..20) into "x" (0..10)`},
		{"undeclared literal", func(r *Registry) {
			x := r.Int("x", 0, 10)
			r.On("set").Param("v", 0, 10).OnlyIf(Eq(Arg("v"), Lit(7))).Does(SetToArg(x, "v")).Add()
		}, "literal 7, which is not a declared constant"},
		{"closure", func(r *Registry) {
			x := r.Int("x", 0, 10)
			r.Event("set").Param("v", 0, 10).Writes(x).ApplyArgs(func(s State, a Args) State { return s.SetInt(x, a["v"]) }).Add()
		}, "is a Go closure"},
		{"instance Independent", func(r *Registry) {
			x := r.Int("x", 0, 10)
			r.On("set").Param("v", 0, 2).Does(SetToArg(x, "v")).Add()
			r.Independent(Instance("set", 0), Instance("set", 1))
		}, "one instance of an event with parameters"},
	}
	for _, c := range cases {
		r := NewRegistry("refuse")
		c.mk(r)
		_, rep, err := r.Abstract().Build()
		var ae *AbstractionError
		if !errors.As(err, &ae) || !strings.Contains(err.Error(), c.want) || rep.AbstractionRefused == "" {
			t.Errorf("%s: %v; want a refusal containing %q", c.name, err, c.want)
		}
	}
}

// A family too wide for Build is accepted by Abstract, and its runtime applies any value.
func TestParamsAbstract_WideFamilyRuntime(t *testing.T) {
	r, stock := cappedRestock(1 << 40)
	if _, _, err := r.Build(); err == nil || !strings.Contains(err.Error(), "declare Abstract") {
		// Abstract is not declared yet, so Build refuses the family.
		t.Fatalf("Build: %v", err)
	}
	m, _, err := r.Abstract(5).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Events()) != 0 {
		t.Errorf("a wide family has no listed instances: %v", m.Events())
	}
	s := m.Apply(m.NewState(), "restock(3)")
	if s.GetInt(stock) != 3 {
		t.Errorf("state %s", s)
	}
}
