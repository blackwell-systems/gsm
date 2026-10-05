package gsm

import (
	"fmt"
	"strings"
	"testing"
)

// TestCoordinationPlan_NegationLoop: the two-registry negation loop is a non-monotone cycle that
// Build rejects. CoordinationPlan names one morphism edge to coordinate, and BuildCoordinated then
// accepts the federation (the residual, with that edge externally coordinated, is acyclic and
// converges).
func TestCoordinationPlan_NegationLoop(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("loop").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
		Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()

	if _, _, err := fed.Build(); err == nil {
		t.Fatal("expected Build to reject the non-monotone cycle")
	}

	plan := fed.CoordinationPlan()
	if len(plan) != 1 {
		t.Fatalf("expected a 1-edge coordination plan for a single 2-cycle, got %d: %v", len(plan), plan)
	}
	if len(plan[0].Shared) == 0 {
		t.Fatalf("coordination point should name the shared variables it controls: %v", plan[0])
	}

	m, _, err := fed.BuildCoordinated(plan)
	if err != nil {
		t.Fatalf("BuildCoordinated should accept the federation given the coordination, got %v", err)
	}
	if m == nil {
		t.Fatal("BuildCoordinated returned a nil machine")
	}
	// The coordinated residual is acyclic and drives the remaining morphism.
	if len(m.Registries()) != 2 {
		t.Fatalf("coordinated machine has %d components, want 2", len(m.Registries()))
	}
}

// TestCoordinationPlan_AcyclicIsEmpty: an acyclic federation needs no coordination, and
// BuildCoordinated with the empty plan is exactly Build.
func TestCoordinationPlan_AcyclicIsEmpty(t *testing.T) {
	a := NewRegistry("A")
	fa := a.Int("fa", 0, 1)
	b := NewRegistry("B")
	fb := b.Int("fb", 0, 1)
	fed := NewFederation("chain").
		Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add()

	if plan := fed.CoordinationPlan(); plan != nil {
		t.Fatalf("acyclic federation should need no coordination, got %v", plan)
	}
	if _, _, err := fed.BuildCoordinated(nil); err != nil {
		t.Fatalf("BuildCoordinated(nil) on an acyclic federation should build, got %v", err)
	}
}

// Example_acceptWithCoordination walks the full loop: a federation that cannot converge, the exact
// adjustment to make, applying it, and then watching it converge. A "primary" and a "mirror" hold a
// bit; the mirror copies the primary, and the primary is forced to disagree with the mirror (an
// antitone constraint). As a cycle this oscillates and Build rejects it. CoordinationPlan names the
// one edge to coordinate (make the primary a single writer); BuildCoordinated accepts the residual,
// and driving it shows the mirror faithfully tracking the primary, valid and stable.
func Example_acceptWithCoordination() {
	primary := NewRegistry("primary")
	pv := primary.Int("v", 0, 1)
	primary.Event("set").Writes(pv).Apply(func(s State) State { return s.SetInt(pv, 1) }).Add()
	mirror := NewRegistry("mirror")
	mv := mirror.Int("v", 0, 1)

	fed := NewFederation("mirrors").
		Morphism(primary, mirror).Shared(mv).
		Map(func(s, d State) State { return d.SetInt(mv, s.GetInt(pv)) }).Add().
		Morphism(mirror, primary).Shared(pv).
		Map(func(s, d State) State { return d.SetInt(pv, 1-s.GetInt(mv)) }).Add()

	// 1. As is, the cycle cannot converge.
	if _, _, err := fed.Build(); err != nil {
		fmt.Println("build: rejected (non-monotone cycle)")
	}
	d, err := fed.DiagnoseCycle()
	if err != nil {
		fmt.Println("diagnose error:", err)
		return
	}
	fmt.Println("diagnose: converges =", d.Converges)

	// 2. The exact adjustment.
	plan := fed.CoordinationPlan()
	fmt.Println("coordinate:", plan)
	fmt.Println("authority:", plan[0].Authority) // the root the coordinated cycle is driven from

	// 3. Make it, then 4. watch it converge.
	m, _, err := fed.BuildCoordinated(plan)
	if err != nil {
		fmt.Println("build error:", err)
		return
	}
	s := m.Apply(m.NewState(), primary, "set")
	fmt.Printf("after set: primary=%d mirror=%d valid=%v\n",
		m.Of(s, primary).GetInt(pv), m.Of(s, mirror).GetInt(mv), m.IsValid(s))

	// Output:
	// build: rejected (non-monotone cycle)
	// diagnose: converges = false
	// coordinate: [mirror->primary[v]]
	// authority: primary
	// after set: primary=1 mirror=1 valid=true
}

// TestCoordinationPlan_NamesAuthority mirrors root_choice_matters and copyback_without_authority
// (normalization-confluence coq/CoordinatedCycles.v) on the copy-back loop A <-> B (each copies
// the other's bit). Build rejects it: with both edges kept as writers there is no authority, and
// two consistent states (A = B = 0 and A = B = 1) compete. CoordinationPlan cuts one edge, and the
// target of the cut edge is the root: given the plan the normal form is unique, but it depends on
// which edge was cut. From the same start A = 0, B = 1, rooting at A reaches A = B = 0 and rooting
// at B reaches A = B = 1. The plan and the report name that root (Authority).
func TestCoordinationPlan_NamesAuthority(t *testing.T) {
	type loop struct {
		fed  *Federation
		a, b *Registry
		fa   Var
		fb   Var
	}
	mk := func(aFirst bool) loop {
		a := NewRegistry("A")
		fa := a.Int("fa", 0, 1)
		b := NewRegistry("B")
		fb := b.Int("fb", 0, 1)
		ab := func(f *Federation) *Federation {
			return f.Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add()
		}
		ba := func(f *Federation) *Federation {
			return f.Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, s.GetInt(fb)) }).Add()
		}
		f := NewFederation("copyback")
		if aFirst {
			f = ba(ab(f))
		} else {
			f = ab(ba(f))
		}
		return loop{f, a, b, fa, fb}
	}

	for _, tc := range []struct {
		aFirst bool
		root   string
		want   int // the common value of A and B in the normal form from A = 0, B = 1
	}{
		{aFirst: true, root: "A", want: 0},
		{aFirst: false, root: "B", want: 1},
	} {
		l := mk(tc.aFirst)
		if _, _, err := l.fed.Build(); err == nil {
			t.Fatal("expected Build to reject the copy-back cycle (no authority)")
		}
		plan := l.fed.CoordinationPlan()
		if len(plan) != 1 || plan[0].Authority != tc.root || plan[0].Dst != tc.root {
			t.Fatalf("aFirst=%v: want one point rooted at %s, got %+v", tc.aFirst, tc.root, plan)
		}
		m, rep, err := l.fed.BuildCoordinated(plan)
		if err != nil {
			t.Fatalf("BuildCoordinated(%v): %v", plan, err)
		}
		if !strings.Contains(rep.String(), tc.root+" is the authority") {
			t.Fatalf("report should name the authority %s:\n%s", tc.root, rep)
		}

		fs := m.NewState()
		fs.states[m.idx[l.a]] = fs.states[m.idx[l.a]].SetInt(l.fa, 0)
		fs.states[m.idx[l.b]] = fs.states[m.idx[l.b]].SetInt(l.fb, 1)
		nf := m.Normalize(fs)
		if ga, gb := m.Of(nf, l.a).GetInt(l.fa), m.Of(nf, l.b).GetInt(l.fb); ga != tc.want || gb != tc.want {
			t.Fatalf("rooted at %s: want A = B = %d, got A = %d, B = %d", tc.root, tc.want, ga, gb)
		}
		if again := m.Normalize(nf); again.states[0].ID() != nf.states[0].ID() || again.states[1].ID() != nf.states[1].ID() {
			t.Fatalf("rooted at %s: the normal form is not a fixed point of Normalize", tc.root)
		}
	}

	// An authority other than the target cannot be honored by removing that edge.
	l := mk(true)
	_, _, err := l.fed.BuildCoordinated([]CoordinationPoint{{Src: "B", Dst: "A", Shared: []string{"fa"}, Authority: "B"}})
	if err == nil || !strings.Contains(err.Error(), `authority "B" is not the target "A"`) {
		t.Fatalf("want a rejection of an authority that is not the target, got %v", err)
	}
	// The root is a choice the caller can make: cutting A->B instead roots the same network at B.
	if _, _, err := l.fed.BuildCoordinated([]CoordinationPoint{{Src: "A", Dst: "B", Shared: []string{"fb"}}}); err != nil {
		t.Fatalf("cutting the other edge should build too: %v", err)
	}
}
