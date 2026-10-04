package gsm

import (
	"strings"
	"testing"
)

// invalidOnlyCycle is the trust-audit probe: A's Map to B is non-monotone only on a state where
// A's invariant fails (l && a == 0), which is never a valid state but is exactly where the Kleene
// iteration starts once a is reset to bottom. abFirst controls the declaration order of the two
// morphisms, which fixes the component indices and so the iteration's visiting order.
func invalidOnlyCycle(abFirst bool) (*Federation, *Registry, *Registry, Var, Var) {
	A := NewRegistry("A")
	l := A.Bool("l")
	a := A.Int("a", 0, 2)
	A.Invariant("l_needs_a").Watches(l, a).
		Holds(func(s State) bool { return !s.GetBool(l) || s.GetInt(a) >= 1 }).
		Repair(func(s State) State { return s.SetInt(a, 1) }).Add()
	A.Event("setL").Writes(l).Apply(func(s State) State { return s.SetBool(l, true) }).Add()

	B := NewRegistry("B")
	b := B.Int("b", 0, 2)

	f := NewFederation("cyc").AllowMonotoneCycles()
	ab := func() {
		f.Morphism(A, B).Shared(b).Map(func(src, dst State) State {
			if src.GetInt(a) == 0 && src.GetBool(l) {
				return dst.SetInt(b, 2)
			}
			return dst.SetInt(b, src.GetInt(a))
		}).Add()
	}
	ba := func() {
		f.Morphism(B, A).Shared(a).Map(func(src, dst State) State {
			v := src.GetInt(b)
			if v < 1 {
				v = 1
			}
			return dst.SetInt(a, v)
		}).Add()
	}
	if abFirst {
		ab()
		ba()
	} else {
		ba()
		ab()
	}
	return f, A, B, a, b
}

// TestMonotoneCycles_DeclaredOrderInvariant: the same federation declared with its morphisms in
// either order must either build in both orders and reach the same normal form, or be rejected
// in both. Before the visited-domain check it built in both and reached (a=1, b=1) in one order
// and (a=2, b=2) in the other.
func TestMonotoneCycles_DeclaredOrderInvariant(t *testing.T) {
	type result struct {
		err  string
		a, b int
	}
	run := func(abFirst bool) result {
		f, A, B, a, b := invalidOnlyCycle(abFirst)
		m, _, err := f.Build()
		if err != nil {
			return result{err: err.Error()}
		}
		s := m.Apply(m.NewState(), A, "setL")
		if !m.IsValid(s) {
			t.Fatalf("abFirst=%v: normal form is not valid", abFirst)
		}
		return result{a: m.Of(s, A).GetInt(a), b: m.Of(s, B).GetInt(b)}
	}
	r1, r2 := run(true), run(false)
	if (r1.err == "") != (r2.err == "") {
		t.Fatalf("declaration order changes whether Build accepts: %+v vs %+v", r1, r2)
	}
	if r1.err == "" && r1 != r2 {
		t.Fatalf("declaration order changes the normal form: %+v vs %+v", r1, r2)
	}
	// This federation is not monotone on the states the iteration visits (with l set, raising a
	// from 0 to 1 lowers B's image from 2 to 1), so both orders must be rejected for that reason.
	for _, r := range []result{r1, r2} {
		if !strings.Contains(r.err, "not monotone on the states the monotone-cycle iteration visits") {
			t.Fatalf("want the visited-domain monotonicity rejection, got: %q", r.err)
		}
	}
}

// TestMonotoneCycles_InvalidLeastFixedPointRejected: both Maps are monotone and pass M1 on valid
// states (from a valid source they always write 1), but from the reset bottom, where l is set and
// the shared value is 0, they write 0, so (0, 0) is a fixed point that violates both invariants.
// Build must reject it rather than produce an invalid normal form.
func TestMonotoneCycles_InvalidLeastFixedPointRejected(t *testing.T) {
	A := NewRegistry("A")
	la := A.Bool("l")
	a := A.Int("a", 0, 1)
	A.Invariant("la").Watches(la, a).
		Holds(func(s State) bool { return !s.GetBool(la) || s.GetInt(a) == 1 }).
		Repair(func(s State) State { return s.SetInt(a, 1) }).Add()
	B := NewRegistry("B")
	lb := B.Bool("l")
	b := B.Int("b", 0, 1)
	B.Invariant("lb").Watches(lb, b).
		Holds(func(s State) bool { return !s.GetBool(lb) || s.GetInt(b) == 1 }).
		Repair(func(s State) State { return s.SetInt(b, 1) }).Add()
	f := NewFederation("bottom").AllowMonotoneCycles().
		Morphism(A, B).Shared(b).Map(func(src, d State) State {
		if src.GetBool(la) && src.GetInt(a) == 0 {
			return d.SetInt(b, 0)
		}
		return d.SetInt(b, 1)
	}).Add().
		Morphism(B, A).Shared(a).Map(func(src, d State) State {
		if src.GetBool(lb) && src.GetInt(b) == 0 {
			return d.SetInt(a, 0)
		}
		return d.SetInt(a, 1)
	}).Add()
	_, _, err := f.Build()
	if err == nil || !strings.Contains(err.Error(), "fixed point could then be an invalid federated state") {
		t.Fatalf("want the visited-domain validity rejection, got: %v", err)
	}
}

// TestMonotoneCycles_LegitOrderIndependent: a monotone cycle with local requests builds in both
// declaration orders and reaches the same normal form for the same events.
func TestMonotoneCycles_LegitOrderIndependent(t *testing.T) {
	build := func(abFirst bool) (*FedMachine, *Registry, *Registry) {
		A := NewRegistry("A")
		ra := A.Int("req", 0, 3)
		sa := A.Int("s", 0, 3)
		A.Event("req2").Writes(ra).Apply(func(s State) State { return s.SetInt(ra, 2) }).Add()
		B := NewRegistry("B")
		rb := B.Int("req", 0, 3)
		sb := B.Int("s", 0, 3)
		B.Event("req3").Writes(rb).Apply(func(s State) State { return s.SetInt(rb, 3) }).Add()
		maxOf := func(src State, r, s Var) int {
			if src.GetInt(r) > src.GetInt(s) {
				return src.GetInt(r)
			}
			return src.GetInt(s)
		}
		f := NewFederation("mx").AllowMonotoneCycles()
		ab := func() {
			f.Morphism(A, B).Shared(sb).Map(func(src, d State) State { return d.SetInt(sb, maxOf(src, ra, sa)) }).Add()
		}
		ba := func() {
			f.Morphism(B, A).Shared(sa).Map(func(src, d State) State { return d.SetInt(sa, maxOf(src, rb, sb)) }).Add()
		}
		if abFirst {
			ab()
			ba()
		} else {
			ba()
			ab()
		}
		m, rep, err := f.Build()
		if err != nil {
			t.Fatalf("abFirst=%v: %v\n%s", abFirst, err, rep)
		}
		return m, A, B
	}
	m1, A1, B1 := build(true)
	m2, A2, B2 := build(false)
	s1 := m1.Apply(m1.Apply(m1.NewState(), A1, "req2"), B1, "req3")
	s2 := m2.Apply(m2.Apply(m2.NewState(), A2, "req2"), B2, "req3")
	if m1.Of(s1, A1).ID() != m2.Of(s2, A2).ID() || m1.Of(s1, B1).ID() != m2.Of(s2, B2).ID() {
		t.Fatalf("declaration order changes the normal form: A %s vs %s, B %s vs %s",
			m1.Of(s1, A1), m2.Of(s2, A2), m1.Of(s1, B1), m2.Of(s2, B2))
	}
}

// TestMonotoneCycles_CapIsAnError: a Map that turns impure after Build (it alternates its output)
// keeps the iteration from settling. Reaching kleeneCap must panic with an explanation, never
// return a non-fixed point.
func TestMonotoneCycles_CapIsAnError(t *testing.T) {
	A := NewRegistry("A")
	a := A.Int("a", 0, 1)
	B := NewRegistry("B")
	b := B.Int("b", 0, 1)
	impure, n := false, 0
	f := NewFederation("flaky").AllowMonotoneCycles().
		Morphism(A, B).Shared(b).Map(func(src, d State) State {
		if impure {
			n++
			return d.SetInt(b, n%2)
		}
		return d.SetInt(b, src.GetInt(a))
	}).Add().
		Morphism(B, A).Shared(a).Map(func(src, d State) State { return d.SetInt(a, src.GetInt(b)) }).Add()
	m, _, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	impure = true
	msg := catchPanic(func() { m.Normalize(m.NewState()) })
	if !strings.Contains(msg, "did not reach a fixed point") {
		t.Fatalf("want a panic at the iteration cap, got: %q", msg)
	}
}
