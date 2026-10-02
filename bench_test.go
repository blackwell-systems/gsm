package gsm

import (
	"fmt"
	"testing"
)

// Build cost benchmarks. The wide machines are the worst case for Build's CC phase:
// every event pair is independent and touches a different variable, so every pair
// is checked over the whole state space.

// wideCounters: n independent Int(0..3) counters (2 bits each), each with a cap
// invariant and an increment event. 2n bits of state; n(n-1)/2 event pairs.
func wideCounters(n int) *Registry {
	r := NewRegistry(fmt.Sprintf("wide_counters_%d", n))
	for k := 0; k < n; k++ {
		v := r.Int(fmt.Sprintf("c%d", k), 0, 3)
		r.Invariant(fmt.Sprintf("cap%d", k)).Watches(v).
			Holds(func(s State) bool { return s.GetInt(v) <= 2 }).
			Repair(func(s State) State { return s.SetInt(v, 2) }).Add()
		r.Event(fmt.Sprintf("inc%d", k)).Writes(v).
			Apply(func(s State) State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
	}
	return r
}

// wideFlags: n independent Bool flags, each raised by its own event, no invariants.
// n bits of state; n(n-1)/2 event pairs.
func wideFlags(n int) *Registry {
	r := NewRegistry(fmt.Sprintf("wide_flags_%d", n))
	for k := 0; k < n; k++ {
		v := r.Bool(fmt.Sprintf("f%d", k))
		r.Event(fmt.Sprintf("raise%d", k)).Writes(v).
			Apply(func(s State) State { return s.SetBool(v, true) }).Add()
	}
	return r
}

// combFlags: n Bool flags written with combinators, so Build's rules oracle
// can read it, and events raising them in turn (event e raises flag e mod n);
// pairs >= 0 declares that many pairs independent (raise0.., in order), -1
// leaves every pair checked.
func combFlags(n, events, pairs int) *Registry {
	r := NewRegistry(fmt.Sprintf("comb_flags_%d_%d", n, events))
	var fs []Var
	for k := 0; k < n; k++ {
		fs = append(fs, r.Bool(fmt.Sprintf("f%d", k)))
	}
	for e := 0; e < events; e++ {
		r.DeclEvent(fmt.Sprintf("raise%d", e), Raise(fs[e%n]))
	}
	for i, c := 0, 0; i < events && c < pairs; i++ {
		for j := i + 1; j < events && c < pairs; j, c = j+1, c+1 {
			r.Independent(fmt.Sprintf("raise%d", i), fmt.Sprintf("raise%d", j))
		}
	}
	return r
}

// orderFulfillment is the README's order machine (see example_test.go).
func benchOrder() *Registry {
	r := NewRegistry("order")
	status := r.Enum("status", "pending", "paid", "shipped")
	paid := r.Bool("paid")
	r.Invariant("ship_requires_payment").Watches(status, paid).
		Holds(func(s State) bool { return s.Get(status) != "shipped" || s.GetBool(paid) }).
		Repair(func(s State) State { return s.SetBool(paid, true) }).Add()
	r.Event("pay").Writes(status, paid).
		Apply(func(s State) State {
			if s.Get(status) == "pending" {
				s = s.Set(status, "paid")
			}
			return s.SetBool(paid, true)
		}).Add()
	r.Event("ship").Writes(status).
		Apply(func(s State) State { return s.Set(status, "shipped") }).Add()
	return r
}

func benchBuild(b *testing.B, mk func() *Registry) {
	b.Helper()
	r := mk()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, rep, err := r.Build(); err != nil {
			b.Fatalf("Build: %v\n%s", err, rep)
		}
	}
}

func BenchmarkBuild_Order(b *testing.B) { benchBuild(b, benchOrder) }
func BenchmarkBuild_WideCounters5(b *testing.B) {
	benchBuild(b, func() *Registry { return wideCounters(5) })
}
func BenchmarkBuild_WideCounters10(b *testing.B) {
	benchBuild(b, func() *Registry { return wideCounters(10) })
}
func BenchmarkBuild_WideFlags10(b *testing.B) {
	benchBuild(b, func() *Registry { return wideFlags(10) })
}
func BenchmarkBuild_WideFlags20(b *testing.B) {
	benchBuild(b, func() *Registry { return wideFlags(20) })
}

// The rules oracle's cost near RulesOracleMaxWork (2^29; the work is
// oracle.RulesCost): the most states (2^19 states, one event: work 4.4 x
// 10^8), the most pairs (2^12 states, 20 events, every pair: 5.3 x 10^8),
// and above the cap (2^20 states, 20 events, one pair: table oracle only).
// TestRulesOracleCostPerStep (GSM_RULES_COST) measures the rules oracle
// alone.
func BenchmarkBuild_CombMostStates(b *testing.B) {
	benchBuild(b, func() *Registry { return combFlags(19, 1, 0) })
}
func BenchmarkBuild_CombMostPairs(b *testing.B) {
	benchBuild(b, func() *Registry { return combFlags(12, 20, -1) })
}
func BenchmarkBuild_CombAboveCap(b *testing.B) {
	benchBuild(b, func() *Registry { return combFlags(20, 20, 1) })
}

func benchCompositional(b *testing.B, n int) {
	b.Helper()
	r := wideCounters(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, rep, err := r.BuildCompositional(); err != nil {
			b.Fatalf("BuildCompositional: %v\n%s", err, rep)
		}
	}
}

func BenchmarkBuildCompositional_WideCounters10(b *testing.B) { benchCompositional(b, 10) }
func BenchmarkBuildCompositional_WideCounters20(b *testing.B) { benchCompositional(b, 20) }
func BenchmarkBuildCompositional_WideCounters32(b *testing.B) { benchCompositional(b, 32) }

// Lazy Apply cost: a BuildCompositional machine computes Apply from the rules at
// runtime (effect, then repairs until every invariant holds), so this is the hot
// path for machines too large to tabulate. Each step increments one counter; the
// cap repair fires once every counter has wrapped past 2.
func BenchmarkLazyApply_WideCounters32(b *testing.B) { benchLazyApply(b, wideCounters(32)) }

// tightCounters is wideCounters with Int(0..2) counters: a domain of 3 in a 2-bit field,
// so every variable needs a range comparison in the runtime domain check (the worst case
// for it; wideCounters' power-of-two domains need only the mask test).
func tightCounters(n int) *Registry {
	r := NewRegistry(fmt.Sprintf("tight_counters_%d", n))
	for k := 0; k < n; k++ {
		v := r.Int(fmt.Sprintf("c%d", k), 0, 2)
		r.Invariant(fmt.Sprintf("cap%d", k)).Watches(v).
			Holds(func(s State) bool { return s.GetInt(v) <= 1 }).
			Repair(func(s State) State { return s.SetInt(v, 1) }).Add()
		r.Event(fmt.Sprintf("inc%d", k)).Writes(v).
			Apply(func(s State) State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
	}
	return r
}

func benchLazyApply(b *testing.B, r *Registry) {
	b.Helper()
	m, rep, err := r.BuildCompositional()
	if err != nil {
		b.Fatalf("BuildCompositional: %v\n%s", err, rep)
	}
	events := m.Events()
	s := m.NewState()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = m.Apply(s, events[i%len(events)])
	}
}

func BenchmarkLazyApply_TightCounters32(b *testing.B) { benchLazyApply(b, tightCounters(32)) }
func BenchmarkBuildCompositional_TightCounters32(b *testing.B) {
	r := tightCounters(32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, rep, err := r.BuildCompositional(); err != nil {
			b.Fatalf("BuildCompositional: %v\n%s", err, rep)
		}
	}
}

// Table Apply cost: a Build machine's Apply is one step-table lookup, so any per-call
// work shows up here. Order is the README machine (3 vars); WideCounters8 has 8 vars
// with power-of-two domains and TightCounters8 8 vars that each need a range check.
func benchTableApply(b *testing.B, r *Registry) {
	b.Helper()
	m, rep, err := r.Build()
	if err != nil {
		b.Fatalf("Build: %v\n%s", err, rep)
	}
	events := m.Events()
	s := m.NewState()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = m.Apply(s, events[i%len(events)])
	}
}

func BenchmarkApply_Order(b *testing.B)          { benchTableApply(b, benchOrder()) }
func BenchmarkApply_WideCounters8(b *testing.B)  { benchTableApply(b, wideCounters(8)) }
func BenchmarkApply_TightCounters8(b *testing.B) { benchTableApply(b, tightCounters(8)) }
