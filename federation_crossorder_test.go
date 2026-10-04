package gsm

import (
	"errors"
	"strings"
	"testing"
)

// recallFederation is the audit probe: the supplier's sell is guarded on listed_ok, a shared
// variable the manufacturer's morphism overwrites. recall,sell and sell,recall end with
// different sold counts.
func recallFederation() (*Federation, *Registry, *Registry, Var) {
	mfr := NewRegistry("manufacturer")
	recalled := mfr.Bool("recalled")
	mfr.Event("recall").Writes(recalled).Apply(func(s State) State { return s.SetBool(recalled, true) }).Add()

	sup := NewRegistry("supplier")
	listed := sup.Bool("listed_ok")
	sold := sup.Int("sold", 0, 3)
	sup.Event("sell").Writes(sold).
		Guard(func(s State) bool { return s.GetBool(listed) }).
		Apply(func(s State) State { return s.SetInt(sold, s.GetInt(sold)+1) }).Add()

	f := NewFederation("supply")
	f.Morphism(mfr, sup).Shared(listed).
		Map(func(src, dst State) State { return dst.SetBool(listed, !src.GetBool(recalled)) }).Add()
	return f, mfr, sup, sold
}

// TestCrossOrder_GuardReadsShared is the regression for the audit probe: Build must reject the
// federation, and the error must name the event, the target state and both diverging results.
func TestCrossOrder_GuardReadsShared(t *testing.T) {
	f, _, _, _ := recallFederation()
	_, _, err := f.Build()
	if err == nil {
		t.Fatal("Build accepted a federation whose target event reads a morphism-controlled variable")
	}
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CrossOrderError, got %T: %v", err, err)
	}
	if ce.Target != "supplier" || ce.Event != "sell" || ce.Morphism != "morphism manufacturer→supplier" {
		t.Fatalf("wrong failure: %+v", ce)
	}
	if ce.SourceFirst.ID() == ce.EventFirst.ID() {
		t.Fatalf("reported results do not diverge: %+v", ce)
	}
	for _, want := range []string{`"sell"`, `"supplier"`, "manufacturer→supplier", ce.State.String(),
		ce.SourceFirst.String(), ce.EventFirst.String(), ce.From, ce.To} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// TestCrossOrder_ProbeDivergesAtRuntime confirms the reported counterexample is real: both
// orders, recomputed from the reported state with the supplier's own machine and the morphism
// overwrite (the way FedMachine.Apply composes them), give the two reported results, with
// different sold counts.
func TestCrossOrder_ProbeDivergesAtRuntime(t *testing.T) {
	f, _, sup, _ := recallFederation()
	_, _, err := f.Build()
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CrossOrderError, got %v", err)
	}
	sm, _, berr := sup.Build()
	if berr != nil {
		t.Fatal(berr)
	}
	listed, sold := sup.vars[0], sup.vars[1]
	img := ce.SourceFirst.getRaw(listed)
	ow := func(s State) State { return s.setRaw(listed, img) }
	srcFirst := ow(sm.Apply(ow(ce.State), "sell"))
	evFirst := ow(sm.Apply(ce.State, "sell"))
	if srcFirst.ID() != ce.SourceFirst.ID() || evFirst.ID() != ce.EventFirst.ID() {
		t.Fatalf("report does not match runtime: got %s / %s, report %s / %s", srcFirst, evFirst, ce.SourceFirst, ce.EventFirst)
	}
	if srcFirst.GetInt(sold) == evFirst.GetInt(sold) {
		t.Fatalf("sold should differ between the orders: %s vs %s", srcFirst, evFirst)
	}
}

// TestCrossOrder_LocalOnlyEventsBuild: a target event that reads only local variables commutes
// with every source change, so the federation builds, and both delivery orders agree at runtime.
func TestCrossOrder_LocalOnlyEventsBuild(t *testing.T) {
	mfr := NewRegistry("manufacturer")
	recalled := mfr.Bool("recalled")
	mfr.Event("recall").Writes(recalled).Apply(func(s State) State { return s.SetBool(recalled, true) }).Add()

	sup := NewRegistry("supplier")
	listed := sup.Bool("listed_ok")
	sold := sup.Int("sold", 0, 3)
	sup.Event("sell").Writes(sold).
		Apply(func(s State) State { return s.SetInt(sold, s.GetInt(sold)+1) }).Add()

	f := NewFederation("supply")
	f.Morphism(mfr, sup).Shared(listed).
		Map(func(src, dst State) State { return dst.SetBool(listed, !src.GetBool(recalled)) }).Add()
	m, _, err := f.Build()
	if err != nil {
		t.Fatalf("legitimate federation rejected: %v", err)
	}
	s := m.Normalize(m.NewState())
	x := m.Apply(m.Apply(s, mfr, "recall"), sup, "sell")
	y := m.Apply(m.Apply(s, sup, "sell"), mfr, "recall")
	if m.Of(x, sup).ID() != m.Of(y, sup).ID() {
		t.Fatalf("orders diverge: %s vs %s", m.Of(x, sup), m.Of(y, sup))
	}
}

// TestCrossOrder_EventWritingSharedBuilds: a target event that writes a shared variable (and
// reads nothing shared) is overwritten by repair in both orders, so it commutes and builds.
func TestCrossOrder_EventWritingSharedBuilds(t *testing.T) {
	mfr := NewRegistry("manufacturer")
	recalled := mfr.Bool("recalled")
	mfr.Event("recall").Writes(recalled).Apply(func(s State) State { return s.SetBool(recalled, true) }).Add()

	sup := NewRegistry("supplier")
	listed := sup.Bool("listed_ok")
	sup.Event("delist").Writes(listed).Apply(func(s State) State { return s.SetBool(listed, false) }).Add()

	f := NewFederation("supply")
	f.Morphism(mfr, sup).Shared(listed).
		Map(func(src, dst State) State { return dst.SetBool(listed, !src.GetBool(recalled)) }).Add()
	if _, _, err := f.Build(); err != nil {
		t.Fatalf("legitimate federation rejected: %v", err)
	}
}

// TestCrossOrder_UnreachableSharedValueBuilds: the check quantifies only over target states
// whose shared component is an image the source can produce. A guard on a shared value no
// source normal form maps to (level == 3, images are 0 and 1) cannot race and is accepted.
func TestCrossOrder_UnreachableSharedValueBuilds(t *testing.T) {
	src := NewRegistry("src")
	on := src.Bool("on")
	src.Event("flip").Writes(on).Apply(func(s State) State { return s.SetBool(on, true) }).Add()

	dst := NewRegistry("dst")
	level := dst.Int("level", 0, 3)
	count := dst.Int("count", 0, 2)
	dst.Event("bump").Writes(count).
		Guard(func(s State) bool { return s.GetInt(level) == 3 }).
		Apply(func(s State) State { return s.SetInt(count, s.GetInt(count)+1) }).Add()

	f := NewFederation("levels")
	f.Morphism(src, dst).Shared(level).
		Map(func(a, b State) State {
			if a.GetBool(on) {
				return b.SetInt(level, 1)
			}
			return b.SetInt(level, 0)
		}).Add()
	if _, _, err := f.Build(); err != nil {
		t.Fatalf("federation rejected over an unreachable shared value: %v", err)
	}
}

// TestCrossOrder_ChainDownstream: in a chain A→B→C, an event on C that reads C's shared
// variable races with an event on A whose change propagates through B. The check on edge B→C
// catches it, because every valid B state's image is quantified.
func TestCrossOrder_ChainDownstream(t *testing.T) {
	a := NewRegistry("a")
	x := a.Bool("x")
	a.Event("set").Writes(x).Apply(func(s State) State { return s.SetBool(x, true) }).Add()

	b := NewRegistry("b")
	y := b.Bool("y")

	c := NewRegistry("c")
	z := c.Bool("z")
	n := c.Int("n", 0, 2)
	c.Event("tick").Writes(n).
		Guard(func(s State) bool { return !s.GetBool(z) }).
		Apply(func(s State) State { return s.SetInt(n, s.GetInt(n)+1) }).Add()

	f := NewFederation("chain")
	f.Morphism(a, b).Shared(y).Map(func(s, d State) State { return d.SetBool(y, s.GetBool(x)) }).Add()
	f.Morphism(b, c).Shared(z).Map(func(s, d State) State { return d.SetBool(z, s.GetBool(y)) }).Add()
	_, _, err := f.Build()
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CrossOrderError, got %v", err)
	}
	if ce.Target != "c" || ce.Event != "tick" || ce.Morphism != "morphism b→c" {
		t.Fatalf("wrong failure: %+v", ce)
	}
}

// resolvedDoor builds a two-source resolved target whose event either reads the merged shared
// variable (readsShared) or only a local one.
func resolvedDoor(readsShared bool) *Federation {
	hr := NewRegistry("hr")
	employed := hr.Bool("employed")
	hr.Event("hire").Writes(employed).Apply(func(s State) State { return s.SetBool(employed, true) }).Add()

	sec := NewRegistry("security")
	cleared := sec.Bool("cleared")
	sec.Event("clear").Writes(cleared).Apply(func(s State) State { return s.SetBool(cleared, true) }).Add()

	door := NewRegistry("door")
	access := door.Bool("access")
	opens := door.Int("opens", 0, 2)
	ev := door.Event("open").Writes(opens)
	if readsShared {
		ev = ev.Guard(func(s State) bool { return s.GetBool(access) })
	}
	ev.Apply(func(s State) State { return s.SetInt(opens, s.GetInt(opens)+1) }).Add()

	f := NewFederation("door")
	f.Morphism(hr, door).Shared(access).Map(func(s, d State) State { return d }).Add()
	f.Morphism(sec, door).Shared(access).Map(func(s, d State) State { return d }).Add()
	f.Resolve(door, func(d State, src map[string]State) State {
		return d.SetBool(access, src["hr"].GetBool(employed) && src["security"].GetBool(cleared))
	})
	return f
}

// TestCrossOrder_Resolver: the same race on a multi-source target is caught through its
// resolver's images, and a resolved target whose event reads only local state still builds.
func TestCrossOrder_Resolver(t *testing.T) {
	_, _, err := resolvedDoor(true).Build()
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CrossOrderError, got %v", err)
	}
	if ce.Target != "door" || ce.Event != "open" || ce.Morphism != `resolver for "door"` {
		t.Fatalf("wrong failure: %+v", ce)
	}
	if !strings.Contains(ce.From, "hr:") || !strings.Contains(ce.To, "security:") {
		t.Fatalf("resolver witness should name every source: from %q to %q", ce.From, ce.To)
	}
	if _, _, err := resolvedDoor(false).Build(); err != nil {
		t.Fatalf("legitimate resolved federation rejected: %v", err)
	}
}

// TestCrossOrder_CertifyRejects: Certify runs the same build, so a certificate cannot be issued
// for a federation with a cross-registry race.
func TestCrossOrder_CertifyRejects(t *testing.T) {
	f, _, _, _ := recallFederation()
	_, err := f.Certify()
	var ce *CrossOrderError
	if !errors.As(err, &ce) {
		t.Fatalf("Certify: want *CrossOrderError, got %v", err)
	}
}
