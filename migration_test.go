package gsm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// Tests for CheckMigration (migration.go), mirroring the instances of normalization-confluence
// coq/Reconfiguration.v. gsm's model is the deterministic one (section Det): Apply repairs
// before it returns, so a switch never migrates a state between an event and its repair.
// Where an instance's outcome in gsm's model differs from the rewriting model's, the test
// says so.

// capRegistry is a counter capped at limit: Int count in 0..max, the invariant count <= limit
// repaired to limit, and one event per entry of adds adding its amount.
func capRegistry(name string, limit, max int, adds map[string]int) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	count := r.Int("count", 0, max)
	r.Invariant("cap").Watches(count).
		Holds(func(s gsm.State) bool { return s.GetInt(count) <= limit }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(count, limit) }).Add()
	for _, ev := range sortedKeys(adds) {
		k := adds[ev]
		r.Event(ev).Writes(count).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(count, s.GetInt(count)+k) }).Add()
	}
	return r, count
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// lwwRegistry is last-writer-wins on Int x in 0..2: set1 writes 1, set2 writes 2.
func lwwRegistry(name string) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	x := r.Int("x", 0, 2)
	r.Event("set1").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()
	r.Event("set2").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 2) }).Add()
	return r, x
}

func mustCheck(t *testing.T, from, to *gsm.Registry, m gsm.Migration, events map[string]string, opts ...gsm.MigrationOption) *gsm.MigrationReport {
	t.Helper()
	rep, err := gsm.CheckMigration(from, to, m, events, opts...)
	if err != nil {
		t.Fatalf("CheckMigration: %v", err)
	}
	t.Logf("\n%s", rep)
	return rep
}

func mustBuild(t *testing.T, r *gsm.Registry) *gsm.Machine {
	t.Helper()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	return m
}

func cond(t *testing.T, rep *gsm.MigrationReport, key string) gsm.MigrationCondition {
	t.Helper()
	c, ok := rep.Condition(key)
	if !ok {
		t.Fatalf("condition %s not evaluated", key)
	}
	return c
}

func hasTheorem(rep *gsm.MigrationReport, name string) bool {
	for _, th := range rep.Theorems {
		if th == name {
			return true
		}
	}
	return false
}

// cap_raise: a cap of 5 raised to 10 with an add in flight. Safe behind a barrier only. An add
// applied under A at 5 is clamped and stays 5; the same add in flight across the switch is
// applied under B and gives 6. In the rewriting model this is the (S2) failure at the raw 6
// (f (rhoA 6) = 5, f 6 = 6); gsm's Apply never leaves the raw 6 to migrate, so in gsm's model
// the same two results are the DS1 failure at 5.
func TestCheckMigration_CapRaise(t *testing.T) {
	a, ca := capRegistry("cap5", 5, 6, map[string]int{"add": 1})
	b, cb := capRegistry("cap10", 10, 11, map[string]int{"add": 1})
	mig := func(old, blank gsm.State) gsm.State { return blank.SetInt(cb, old.GetInt(ca)) }
	rep := mustCheck(t, a, b, mig, nil)

	if rep.Outcome != gsm.MigrationSafeBehindBarrier {
		t.Fatalf("outcome %v, want SAFE BEHIND A BARRIER", rep.Outcome)
	}
	if rep.Failed != gsm.MigrationDS1 || cond(t, rep, gsm.MigrationDS1).Holds {
		t.Fatalf("failed %q, want DS1", rep.Failed)
	}
	for _, k := range []string{gsm.MigrationPermBStart, gsm.MigrationPermBEvery, gsm.MigrationPermA, gsm.MigrationFaithful} {
		if !cond(t, rep, k).Holds {
			t.Errorf("%s fails, want it to hold", k)
		}
	}
	w := rep.LiveWitness
	if w == nil || rep.BarrierWitness != nil {
		t.Fatalf("want a live witness and no barrier witness")
	}
	if got1, got2 := w.Result1.GetInt(cb), w.Result2.GetInt(cb); got1 != 5 || got2 != 6 {
		t.Errorf("witness results %d versus %d, want 5 versus 6", got1, got2)
	}
	if len(w.Run1.Before) != 6 || len(w.Run1.After) != 0 || len(w.Run2.Before) != 5 ||
		strings.Join(w.Run2.After, ",") != "add" || strings.Join(w.Run2.InFlight, ",") != "add" {
		t.Errorf("witness runs %+v and %+v, want six adds under cap5 against five then one in flight", w.Run1, w.Run2)
	}
	if !rep.Faithful || !hasTheorem(rep, "det_barrier_faithful") || !hasTheorem(rep, "det_live_exact") {
		t.Errorf("faithful %v, theorems %v", rep.Faithful, rep.Theorems)
	}
	out := rep.String()
	for _, want := range []string{"Migration cap5 -> cap10: SAFE BEHIND A BARRIER", "[DS1]: FAIL", "{count=5}", "{count=6}"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

// doubling_migration: m doubles the state, the event adds 1 on both sides. DS1 fails at 0:
// add under A then migrate gives 2, migrate then add gives 1.
func TestCheckMigration_DoublingMigration(t *testing.T) {
	a := gsm.NewRegistry("succ")
	x := a.Int("x", 0, 7)
	a.Event("inc").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, s.GetInt(x)+1) }).Add()
	b := gsm.NewRegistry("succ2")
	y := b.Int("y", 0, 15)
	b.Event("inc").Writes(y).Apply(func(s gsm.State) gsm.State { return s.SetInt(y, s.GetInt(y)+1) }).Add()
	rep := mustCheck(t, a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(y, 2*old.GetInt(x)) }, nil)

	if rep.Outcome != gsm.MigrationSafeBehindBarrier || rep.Failed != gsm.MigrationDS1 {
		t.Fatalf("outcome %v failed %q, want SAFE BEHIND A BARRIER on DS1", rep.Outcome, rep.Failed)
	}
	w := rep.LiveWitness
	if w.Start.GetInt(x) != 0 || w.Result1.GetInt(y) != 2 || w.Result2.GetInt(y) != 1 {
		t.Errorf("witness from %s: %s versus %s, want from 0: 2 versus 1", w.Start, w.Result1, w.Result2)
	}
}

// migrated_transient: in the rewriting model, A's event moves 0 to the invalid 1 (repaired to
// 0), and B treats the migrated raw 1 differently (its event true moves 1 to 2), so condition
// (B) fails at m 1, a state only a switch between an event and its repair migrates, and the
// change is BarrierOnly. gsm's Apply repairs before it returns, so no gsm run migrates the
// raw 1: in gsm's model the change is safe online. The test also confirms that the rewriting
// model's divergence is real for these rules (B's event on the raw 1 against on its repair).
func TestCheckMigration_MigratedTransientIsOnlineInGsmModel(t *testing.T) {
	a := gsm.NewRegistry("trA")
	x := a.Int("x", 0, 1)
	a.Invariant("zero").Watches(x).
		Holds(func(s gsm.State) bool { return s.GetInt(x) == 0 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(x, 0) }).Add()
	a.Event("e").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()

	b := gsm.NewRegistry("trB")
	y := b.Int("y", 0, 2)
	notOne := func(s gsm.State) bool { return s.GetInt(y) != 1 }
	repairB := func(s gsm.State) gsm.State { return s.SetInt(y, 0) }
	evTrue := func(s gsm.State) gsm.State {
		if s.GetInt(y) == 1 {
			return s.SetInt(y, 2)
		}
		return s
	}
	b.Invariant("not_one").Watches(y).Holds(notOne).Repair(repairB).Add()
	b.Event("true").Writes(y).Apply(evTrue).Add()
	b.Event("false").Writes(y).Apply(func(s gsm.State) gsm.State { return s }).Add()
	mig := func(old, blank gsm.State) gsm.State { return blank.SetInt(y, old.GetInt(x)) }

	rep := mustCheck(t, a, b, mig, map[string]string{"e": "false"})
	if rep.Outcome != gsm.MigrationSafeOnline || rep.LiveWitness != nil {
		t.Fatalf("outcome %v, want SAFE ONLINE in gsm's model", rep.Outcome)
	}
	// From the invalid start 1 (a state restored from storage): A's Apply normalizes it first.
	m, _, err := a.Build()
	if err != nil {
		t.Fatal(err)
	}
	one := m.NewState().SetInt(x, 1)
	if rep := mustCheck(t, a, b, mig, map[string]string{"e": "false"}, gsm.MigrationFrom(one)); rep.Outcome != gsm.MigrationSafeOnline {
		t.Fatalf("from x=1: outcome %v, want SAFE ONLINE", rep.Outcome)
	}

	// The rewriting model's (B) failure at m 1: B's event applied to the raw 1 then repaired
	// gives 2; applied after repairing 1 gives 0. gsm never applies an event to the raw 1.
	mb, _, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	raw := mb.NewState().SetInt(y, 1)
	if notOne(raw) {
		t.Fatal("1 should be invalid in trB")
	}
	if got := mb.Normalize(evTrue(raw)).GetInt(y); got != 2 {
		t.Errorf("event on the raw 1 then repair: %d, want 2", got)
	}
	if got := mb.Apply(raw, "true").GetInt(y); got != 0 {
		t.Errorf("gsm's Apply on the raw 1 (normalized first): %d, want 0", got)
	}
}

// forgetful_migration: A is last-writer-wins (it diverges); the migration forgets A's state;
// B has one state. Every live run converges: A's own condition is not needed online.
func TestCheckMigration_ForgetfulMigration(t *testing.T) {
	a, _ := lwwRegistry("lwwA")
	b := gsm.NewRegistry("unitB")
	b.Bool("u")
	b.Event("tick").Apply(func(s gsm.State) gsm.State { return s }).Add()
	rep := mustCheck(t, a, b, func(old, blank gsm.State) gsm.State { return blank },
		map[string]string{"set1": "tick", "set2": "tick"})

	if rep.Outcome != gsm.MigrationSafeOnline {
		t.Fatalf("outcome %v, want SAFE ONLINE", rep.Outcome)
	}
	if cond(t, rep, gsm.MigrationPermA).Holds {
		t.Error("PermA holds; lwwA should diverge on its own")
	}
	if rep.Faithful || rep.Collision == nil {
		t.Error("the forgetful migration should not be faithful")
	}
	if !hasTheorem(rep, "det_live_exact") {
		t.Errorf("theorems %v", rep.Theorems)
	}
}

// rescaled_cap (non-vacuity): a cap of 5 becomes a cap of 10, m doubles the state, each A add
// becomes an add of 2, next to native adds of 1. Safe online from every start.
func TestCheckMigration_RescaledCap(t *testing.T) {
	a, ca := capRegistry("cap5", 5, 6, map[string]int{"add": 1})
	b, cb := capRegistry("cap10x2", 10, 12, map[string]int{"add1": 1, "add2": 2})
	mig := func(old, blank gsm.State) gsm.State { return blank.SetInt(cb, 2*old.GetInt(ca)) }
	m, _, err := a.Build()
	if err != nil {
		t.Fatal(err)
	}
	var starts []gsm.State
	for v := 0; v <= 5; v++ {
		starts = append(starts, m.NewState().SetInt(ca, v))
	}
	rep := mustCheck(t, a, b, mig, map[string]string{"add": "add2"}, gsm.MigrationFrom(starts...))
	if rep.Outcome != gsm.MigrationSafeOnline {
		t.Fatalf("outcome %v, want SAFE ONLINE", rep.Outcome)
	}
	for _, c := range rep.Conditions {
		if c.Evaluated && !c.Holds {
			t.Errorf("%s fails; every condition holds in rescaled_cap", c.Key)
		}
	}
	if !strings.Contains(rep.String(), "SAFE ONLINE (in-flight events commute with the migration; cap10x2 converges from every migrated state)") {
		t.Errorf("summary line:\n%s", rep)
	}
}

// lww_target: B is last-writer-wins. Unsafe: two barrier runs, with nothing in flight,
// diverge after the switch.
func TestCheckMigration_LWWTargetUnsafe(t *testing.T) {
	a := gsm.NewRegistry("succ")
	x := a.Int("x", 0, 2)
	a.Event("inc").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, s.GetInt(x)+1) }).Add()
	b, bx := lwwRegistry("lwwB")
	rep := mustCheck(t, a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(bx, old.GetInt(x)) },
		map[string]string{"inc": "set1"})

	if rep.Outcome != gsm.MigrationUnsafe || rep.Failed != gsm.MigrationPermBEvery {
		t.Fatalf("outcome %v failed %q, want UNSAFE on PermB-every", rep.Outcome, rep.Failed)
	}
	w := rep.BarrierWitness
	if w == nil || !w.Barrier || len(w.Run1.InFlight) != 0 || len(w.Run2.InFlight) != 0 {
		t.Fatalf("want a barrier witness, got %+v", w)
	}
	if w.Result1.GetInt(bx) == w.Result2.GetInt(bx) {
		t.Errorf("witness does not diverge: %s versus %s", w.Result1, w.Result2)
	}
	if !strings.Contains(rep.String(), "UNSAFE") || !strings.Contains(rep.String(), "Why unsafe") {
		t.Errorf("report:\n%s", rep)
	}
}

// A diverges and a faithful migration keeps the difference: unsafe, by two barrier runs whose
// A results differ (the A part of det_barrier_faithful).
func TestCheckMigration_FaithfulADivergenceUnsafe(t *testing.T) {
	a, ax := lwwRegistry("lwwA")
	b := gsm.NewRegistry("copy")
	y := b.Int("y", 0, 2)
	b.Event("noop").Writes(y).Apply(func(s gsm.State) gsm.State { return s }).Add()
	rep := mustCheck(t, a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(y, old.GetInt(ax)) },
		map[string]string{"set1": "noop", "set2": "noop"})
	if rep.Outcome != gsm.MigrationUnsafe || rep.Failed != gsm.MigrationAmodM || !rep.Faithful {
		t.Fatalf("outcome %v failed %q faithful %v, want UNSAFE on AmodM, faithful", rep.Outcome, rep.Failed, rep.Faithful)
	}
	if !hasTheorem(rep, "det_barrier_faithful") {
		t.Errorf("theorems %v", rep.Theorems)
	}
}

// unknownPair is A = last-writer-wins x plus a counter c capped at 2, B = a counter capped at
// 3; the migration keeps c, adjusted by keep(x). The cap raise makes the change unsafe online.
func unknownPair(keep func(x int) int) (a, b *gsm.Registry, mig gsm.Migration, events map[string]string) {
	a = gsm.NewRegistry("lwwcap2")
	x := a.Int("x", 0, 2)
	c := a.Int("c", 0, 3)
	a.Invariant("cap").Watches(c).
		Holds(func(s gsm.State) bool { return s.GetInt(c) <= 2 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(c, 2) }).Add()
	a.Event("set1").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()
	a.Event("set2").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 2) }).Add()
	a.Event("add").Writes(c).Apply(func(s gsm.State) gsm.State { return s.SetInt(c, s.GetInt(c)+1) }).Add()
	b = gsm.NewRegistry("cap3")
	d := b.Int("c", 0, 5)
	b.Invariant("cap").Watches(d).
		Holds(func(s gsm.State) bool { return s.GetInt(d) <= 3 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(d, 3) }).Add()
	b.Event("add").Writes(d).Apply(func(s gsm.State) gsm.State { return s.SetInt(d, s.GetInt(d)+1) }).Add()
	b.Event("noop").Writes(d).Apply(func(s gsm.State) gsm.State { return s }).Add()
	mig = func(old, blank gsm.State) gsm.State { return blank.SetInt(d, old.GetInt(c)+keep(old.GetInt(x))) }
	return a, b, mig, map[string]string{"set1": "noop", "set2": "noop"}
}

// closureTheorems are the theorems that make an exhausted AmodM search a certificate
// (normalization-confluence coq/ReconfigurationClosure.v).
var closureTheorems = []string{"det_barrier_closure_exact", "amodm_closure_exact", "gsm_closure_exact", "amodm_witness_exact"}

// A non-injective migration that absorbs A's divergence: the migration drops x, and c
// converges. A diverges on its own and the migration is not faithful, so PermA does not
// decide the barrier; the AmodM search exhausts the pair closure with no witness, which
// certifies it (det_barrier_closure_exact). Safe behind a barrier, with the live witness.
func TestCheckMigration_NonInjectiveAbsorbingSafeBehindBarrier(t *testing.T) {
	a, b, mig, events := unknownPair(func(int) int { return 0 })
	rep := mustCheck(t, a, b, mig, events)
	if rep.Outcome != gsm.MigrationSafeBehindBarrier || rep.SearchStopped != "" {
		t.Fatalf("outcome %v, search %q, want SAFE BEHIND A BARRIER", rep.Outcome, rep.SearchStopped)
	}
	if rep.Faithful || cond(t, rep, gsm.MigrationPermA).Holds || !cond(t, rep, gsm.MigrationPermBEvery).Holds {
		t.Errorf("want PermA failing, PermB-every holding, not faithful")
	}
	if c := cond(t, rep, gsm.MigrationAmodM); !c.Decided || !c.Holds || !strings.Contains(c.Detail, "closure is exhausted") {
		t.Errorf("AmodM: %+v", c)
	}
	if rep.LiveWitness == nil || rep.LiveWitness.Condition != gsm.MigrationDS1 || rep.BarrierWitness != nil {
		t.Errorf("want a DS1 live witness and no barrier witness")
	}
	for _, th := range closureTheorems {
		if !hasTheorem(rep, th) {
			t.Errorf("theorems %v lack %s", rep.Theorems, th)
		}
	}
	if !strings.Contains(rep.String(), "[AmodM]: PASS") {
		t.Errorf("report:\n%s", rep)
	}
}

// The closure instances of normalization-confluence coq/ReconfigurationClosure.v: A is
// last-writer-wins on option bool (x = 0 is None, the start; set1 writes Some true, x = 1;
// set2 writes Some false, x = 2), so A diverges; B is one Bool with one event, tick, that
// every A event translates to.
func closureInstance(stepB func(y gsm.Var) func(gsm.State) gsm.State, m func(x int) bool) (a, b *gsm.Registry, mig gsm.Migration, events map[string]string) {
	a, ax := lwwRegistry("lwwA")
	b = gsm.NewRegistry("boolB")
	y := b.Bool("y")
	b.Event("tick").Writes(y).Apply(stepB(y)).Add()
	mig = func(old, blank gsm.State) gsm.State { return blank.SetBool(y, m(old.GetInt(ax))) }
	return a, b, mig, map[string]string{"set1": "tick", "set2": "tick"}
}

// flagB sets the flag; idleB leaves it.
func flagB(y gsm.Var) func(gsm.State) gsm.State {
	return func(s gsm.State) gsm.State { return s.SetBool(y, true) }
}
func idleB(gsm.Var) func(gsm.State) gsm.State { return func(s gsm.State) gsm.State { return s } }

// mergeM merges the two writes (None to false, either write to true); partialM merges the
// start with one write (None and Some true to true, Some false to false).
func mergeM(x int) bool   { return x != 0 }
func partialM(x int) bool { return x != 2 }

// merged_barrier: B ignores the translated events, so an in-flight write is lost and the live
// switch diverges (DS1 fails at the start). A diverges and mergeM is not injective, but every
// pair of the closure is two writes, which mergeM merges: safe behind a barrier, certified by
// the exhausted closure search. Before the closure theorems this case was Unknown.
func TestCheckMigration_ClosureMergedBarrier(t *testing.T) {
	a, b, mig, events := closureInstance(idleB, mergeM)
	rep := mustCheck(t, a, b, mig, events)
	if rep.Outcome != gsm.MigrationSafeBehindBarrier || rep.Failed != gsm.MigrationDS1 {
		t.Fatalf("outcome %v failed %q, want SAFE BEHIND A BARRIER on DS1", rep.Outcome, rep.Failed)
	}
	if rep.Faithful || cond(t, rep, gsm.MigrationPermA).Holds || !cond(t, rep, gsm.MigrationPermBEvery).Holds {
		t.Errorf("want PermA failing, PermB-every holding, not faithful")
	}
	if c := cond(t, rep, gsm.MigrationAmodM); !c.Decided || !c.Holds {
		t.Errorf("AmodM: %+v", c)
	}
	if rep.BarrierWitness != nil || rep.LiveWitness == nil {
		t.Errorf("want a live witness and no barrier witness")
	}
	for _, th := range closureTheorems {
		if !hasTheorem(rep, th) {
			t.Errorf("theorems %v lack %s", rep.Theorems, th)
		}
	}

	// From the start and from Some true (a twin declaration, since lwwA does not Build). From
	// Some true every write migrates to true either way, so that start is safe online and the
	// search runs from the other; the combined report is safe behind a barrier.
	twin := gsm.NewRegistry("twin")
	tx := twin.Int("x", 0, 2)
	tm := mustBuild(t, twin)
	rep = mustCheck(t, a, b, mig, events, gsm.MigrationFrom(tm.NewState(), tm.NewState().SetInt(tx, 1)))
	if rep.Outcome != gsm.MigrationSafeBehindBarrier {
		t.Fatalf("from two starts: outcome %v, want SAFE BEHIND A BARRIER", rep.Outcome)
	}
	if c := cond(t, rep, gsm.MigrationAmodM); !c.Decided || !c.Holds || !strings.HasPrefix(c.Detail, "from {x=0}: ") {
		t.Errorf("from two starts, AmodM: %+v", c)
	}
}

// partial_merge: the same, with partialM. The closure has a pair partialM separates, at the
// start: set1 then set2 gives Some false, set2 then set1 gives Some true, which migrate to
// false and true. Unsafe, with that barrier witness.
func TestCheckMigration_ClosurePartialMerge(t *testing.T) {
	a, b, mig, events := closureInstance(idleB, partialM)
	rep := mustCheck(t, a, b, mig, events)
	if rep.Outcome != gsm.MigrationUnsafe || rep.Failed != gsm.MigrationAmodM || rep.Faithful {
		t.Fatalf("outcome %v failed %q faithful %v, want UNSAFE on AmodM, not faithful", rep.Outcome, rep.Failed, rep.Faithful)
	}
	if c := cond(t, rep, gsm.MigrationAmodM); !c.Decided || c.Holds {
		t.Errorf("AmodM: %+v", c)
	}
	w := rep.BarrierWitness
	if w == nil || !w.Barrier || len(w.Run1.After) != 0 || len(w.Run2.After) != 0 ||
		strings.Join(w.Run1.Before, ",") != "set1,set2" || strings.Join(w.Run2.Before, ",") != "set2,set1" {
		t.Fatalf("barrier witness %+v", w)
	}
	if w.Result1.ID() == w.Result2.ID() {
		t.Errorf("witness does not diverge: %s versus %s", w.Result1, w.Result2)
	}
}

// merged_online: B sets a flag, and every write becomes the flag event. Safe online, though A
// diverges and mergeM is not injective.
func TestCheckMigration_ClosureMergedOnline(t *testing.T) {
	a, b, mig, events := closureInstance(flagB, mergeM)
	rep := mustCheck(t, a, b, mig, events)
	if rep.Outcome != gsm.MigrationSafeOnline || rep.LiveWitness != nil {
		t.Fatalf("outcome %v, want SAFE ONLINE", rep.Outcome)
	}
	if rep.Faithful || cond(t, rep, gsm.MigrationPermA).Holds {
		t.Errorf("want PermA failing and not faithful")
	}
	if _, ok := rep.Condition(gsm.MigrationAmodM); ok {
		t.Errorf("AmodM evaluated; it is searched only when the change is not safe online")
	}
}

// The same shape, but the migration keeps part of x: not injective, and still a witness: two
// orders of set1 and set2 migrate apart. Unsafe, certified by the replayed runs.
func TestCheckMigration_NonInjectiveWithWitnessUnsafe(t *testing.T) {
	a, b, mig, events := unknownPair(func(x int) int {
		if x == 2 {
			return 1
		}
		return 0
	})
	rep := mustCheck(t, a, b, mig, events)
	if rep.Outcome != gsm.MigrationUnsafe || rep.Failed != gsm.MigrationAmodM || rep.Faithful {
		t.Fatalf("outcome %v failed %q faithful %v, want UNSAFE on AmodM, not faithful", rep.Outcome, rep.Failed, rep.Faithful)
	}
	w := rep.BarrierWitness
	if w == nil || !w.Barrier || len(w.Run1.After) != 0 || w.Result1.ID() == w.Result2.ID() {
		t.Fatalf("barrier witness %+v", w)
	}
}

func TestCheckMigration_Refusals(t *testing.T) {
	a, ca := capRegistry("cap5", 5, 6, map[string]int{"add": 1})
	b, cb := capRegistry("cap10", 10, 11, map[string]int{"add": 1})
	mig := func(old, blank gsm.State) gsm.State { return blank.SetInt(cb, old.GetInt(ca)) }

	if _, err := gsm.CheckMigration(a, b, mig, map[string]string{"nope": "add"}); err == nil ||
		!strings.Contains(err.Error(), `names "nope"`) {
		t.Errorf("unknown event in the map: %v", err)
	}
	if _, err := gsm.CheckMigration(a, b, mig, map[string]string{"add": "nope"}); err == nil ||
		!strings.Contains(err.Error(), `translates to "nope"`) {
		t.Errorf("unknown translation: %v", err)
	}
	if _, err := gsm.CheckMigration(a, b, nil, nil); err == nil {
		t.Error("nil migration accepted")
	}
	foreign, fv := capRegistry("other", 1, 30, map[string]int{"add": 1})
	fm := mustBuild(t, foreign)
	if _, err := gsm.CheckMigration(a, b, func(old, blank gsm.State) gsm.State {
		return fm.NewState().SetInt(fv, 20)
	}, nil); err == nil || !strings.Contains(err.Error(), "not a state of") {
		t.Errorf("foreign migration result: %v", err)
	}

	// Declared pairs are checked under exactly-once delivery (declared independence across
	// the switch) and refused under at-least-once delivery, where the combination is not
	// covered.
	ind, _ := capRegistry("ind", 5, 6, map[string]int{"add": 1, "add2": 2})
	ind.Independent("add", "add2")
	if _, err := gsm.CheckMigration(ind, b, mig, map[string]string{"add2": "add"}); err != nil {
		t.Errorf("declared pairs under exactly-once delivery: %v", err)
	}
	if _, err := gsm.CheckMigration(ind, b, mig, map[string]string{"add2": "add"},
		gsm.MigrationDeliveryClass(gsm.MigrationAtLeastOnce)); err == nil || !strings.Contains(err.Error(), "Independent") {
		t.Errorf("declared pairs under at-least-once delivery: %v", err)
	}
	abs := gsm.NewRegistry("abs")
	v := abs.Int("v", 0, 9)
	abs.DeclEvent("add", gsm.Do(gsm.Set(v, gsm.V(v))))
	abs.Abstract()
	if _, err := gsm.CheckMigration(abs, b, func(old, blank gsm.State) gsm.State { return blank }, map[string]string{}); err == nil ||
		!strings.Contains(err.Error(), "Abstract") {
		t.Errorf("abstraction: %v", err)
	}
	other, _ := capRegistry("other", 5, 6, map[string]int{"add": 1})
	m := mustBuild(t, other)
	if _, err := gsm.CheckMigration(a, b, mig, nil, gsm.MigrationFrom(m.NewState().SetInt(ca, 1))); err != nil {
		t.Errorf("a start from an identically declared registry should be accepted: %v", err)
	}
	ob := gsm.NewRegistry("ob")
	ob.Bool("flag")
	obm := mustBuild(t, ob)
	if _, err := gsm.CheckMigration(a, b, mig, nil, gsm.MigrationFrom(obm.NewState())); err == nil ||
		!strings.Contains(err.Error(), "not a state of") {
		t.Errorf("foreign start: %v", err)
	}
}

// A repair that does not terminate on a state a run reaches is an error, not an outcome.
func TestCheckMigration_RepairCycleIsAnError(t *testing.T) {
	a := gsm.NewRegistry("cycle")
	x := a.Int("x", 0, 2)
	a.Invariant("never").Watches(x).
		Holds(func(s gsm.State) bool { return s.GetInt(x) == 0 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(x, 3-s.GetInt(x)) }).Add()
	a.Event("go").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()
	b, cb := capRegistry("cap10", 10, 11, map[string]int{"go": 1})
	_, err := gsm.CheckMigration(a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(cb, 0) }, nil)
	if err == nil || !strings.Contains(err.Error(), "does not terminate") {
		t.Fatalf("err = %v", err)
	}
}

// ExampleCheckMigration checks raising a cart's item cap from 5 to 10 before deploying it.
// The change needs a barrier: an add in flight at the switch is clamped under the old cap by
// one replica and applied under the new cap by another.
func ExampleCheckMigration() {
	cart := func(name string, limit int) (*gsm.Registry, gsm.Var) {
		r := gsm.NewRegistry(name)
		items := r.Int("items", 0, limit+1)
		r.Invariant("cap").Watches(items).
			Holds(func(s gsm.State) bool { return s.GetInt(items) <= limit }).
			Repair(func(s gsm.State) gsm.State { return s.SetInt(items, limit) }).Add()
		r.Event("add").Writes(items).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(items, s.GetInt(items)+1) }).Add()
		return r, items
	}
	v1, items1 := cart("cart v1", 5)
	v2, items2 := cart("cart v2", 10)
	migrate := func(old, blank gsm.State) gsm.State { return blank.SetInt(items2, old.GetInt(items1)) }

	report, err := gsm.CheckMigration(v1, v2, migrate, nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%v: %s\n", report.Outcome, report.Reason)
	w := report.LiveWitness
	fmt.Printf("%v in flight: %d versus %d items\n", w.Run2.InFlight, w.Result1.GetInt(items2), w.Result2.GetInt(items2))
	// Output:
	// SAFE BEHIND A BARRIER: an event in flight at the switch does not commute with it; drain, then switch
	// [add] in flight: 5 versus 6 items
}
