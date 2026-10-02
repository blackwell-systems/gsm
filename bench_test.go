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
