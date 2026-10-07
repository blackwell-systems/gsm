package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// Tests for CheckMigration under a delivery class (MigrationDelivery, Independent pairs),
// mirroring the instances of normalization-confluence coq/ReconfigurationDelivery.v. Where an
// instance's outcome in gsm's model differs from the Coq instance's, the test says why.

// flagRegistry is a Bool f with the given events: "set" raises it, "toggle" flips it,
// "noop" leaves it.
func flagRegistry(name string, events ...string) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	f := r.Bool("f")
	for _, ev := range events {
		switch ev {
		case "set":
			r.Event(ev).Writes(f).Apply(func(s gsm.State) gsm.State { return s.SetBool(f, true) }).Add()
		case "toggle":
			r.Event(ev).Writes(f).Apply(func(s gsm.State) gsm.State { return s.SetBool(f, !s.GetBool(f)) }).Add()
		case "noop":
			r.Event(ev).Writes(f).Apply(func(s gsm.State) gsm.State { return s }).Add()
		}
	}
	return r, f
}

func alo() gsm.MigrationOption { return gsm.MigrationDeliveryClass(gsm.MigrationAtLeastOnce) }

func wantOutcome(t *testing.T, rep *gsm.MigrationReport, want gsm.MigrationOutcome, failed string) {
	t.Helper()
	if rep.Outcome != want || rep.Failed != failed {
		t.Fatalf("outcome %v failing %q, want %v failing %q\n%s", rep.Outcome, rep.Failed, want, failed, rep)
	}
}

func wantTheorems(t *testing.T, rep *gsm.MigrationReport, names ...string) {
	t.Helper()
	for _, n := range names {
		if !hasTheorem(rep, n) {
			t.Errorf("theorems %v lack %s", rep.Theorems, n)
		}
	}
}

// cross_declared_online / cross_declared_barrier: A sets a flag, B has set (the image) and
// toggle, the migration the identity, DS1 holding. With nothing declared (B declares no pair,
// so every pair, in-flight ones included, is delivered in one fixed order) the change is
// online. Declaring only the cross pair (in-flight set, toggle) lets a replica apply the
// in-flight set after a toggle another applies before it: the live switch diverges, while
// B's own pairs, none declared, keep the barrier safe. gsm derives the cross pairs from B's
// declarations, so the cross pair alone is declared with MigrationInFlightIndependent;
// declaring (set, toggle) in B instead declares B's own pair too, and then B diverges after
// every switch.
func TestCheckMigrationDelivery_CrossDeclared(t *testing.T) {
	newPair := func() (*gsm.Registry, *gsm.Registry, gsm.Migration) {
		a, fa := flagRegistry("A", "set")
		b, fb := flagRegistry("B", "set", "toggle")
		b.OnlyDeclaredPairs()
		return a, b, func(old, blank gsm.State) gsm.State { return blank.SetBool(fb, old.GetBool(fa)) }
	}

	a, b, mig := newPair()
	rep := mustCheck(t, a, b, mig, nil)
	wantOutcome(t, rep, gsm.MigrationSafeOnline, "")
	if !rep.Declared || rep.Delivery != gsm.MigrationExactlyOnce {
		t.Errorf("declared %v, delivery %v", rep.Declared, rep.Delivery)
	}
	wantTheorems(t, rep, "live_declared_exact", "classify_declared_complete")

	a, b, mig = newPair()
	rep = mustCheck(t, a, b, mig, nil, gsm.MigrationInFlightIndependent("set", "toggle"))
	wantOutcome(t, rep, gsm.MigrationSafeBehindBarrier, gsm.MigrationPermBStart)
	if c := cond(t, rep, gsm.MigrationPermBEvery); !c.Holds {
		t.Errorf("PermB-every: %+v", c)
	}
	w := rep.LiveWitness
	if w == nil || w.Barrier || strings.Join(w.Run1.InFlight, ",") != "set" || len(w.Run1.InFlightAt) != 1 {
		t.Fatalf("live witness %+v", w)
	}
	wantTheorems(t, rep, "live_declared_exact", "barrier_declared_exact")
	if !strings.Contains(rep.String(), "(in-flight set)") {
		t.Errorf("report does not name the in-flight event:\n%s", rep)
	}

	a, b, mig = newPair()
	b.Independent("set", "toggle")
	rep = mustCheck(t, a, b, mig, nil)
	wantOutcome(t, rep, gsm.MigrationUnsafe, gsm.MigrationPermBEvery)
}

// reset_straddle: A and B set a flag, the migration resets it. Exactly once the change is
// safe behind a barrier (DS1 fails: an in-flight set is applied after the reset). At least
// once it is unsafe, by AbsorbS alone (B's events commute and are idempotent, and runs of A
// with one set of events migrate to one state): a set applied before a barrier switch and
// redelivered after it raises B's flag.
func TestCheckMigrationDelivery_ResetStraddle(t *testing.T) {
	a, _ := flagRegistry("A", "set")
	b, _ := flagRegistry("B", "set")
	reset := func(old, blank gsm.State) gsm.State { return blank }

	rep := mustCheck(t, a, b, reset, nil)
	wantOutcome(t, rep, gsm.MigrationSafeBehindBarrier, gsm.MigrationDS1)

	rep = mustCheck(t, a, b, reset, nil, alo())
	wantOutcome(t, rep, gsm.MigrationUnsafe, gsm.MigrationAbsorbS)
	for _, k := range []string{gsm.MigrationPermBEvery, gsm.MigrationIdemEvery} {
		if c := cond(t, rep, k); !c.Holds {
			t.Errorf("%s: %+v", k, c)
		}
	}
	if _, ok := rep.Condition(gsm.MigrationAmodA); ok {
		t.Error("AmodA evaluated after AbsorbS decided the outcome")
	}
	w := rep.BarrierWitness
	if w == nil || !w.Barrier || !w.Redelivery || strings.Join(w.Run2.Before, ",") != "set" ||
		strings.Join(w.Run2.InFlight, ",") != "set" || len(w.Run1.InFlight) != 0 {
		t.Fatalf("barrier witness %+v", w)
	}
	if !strings.Contains(rep.String(), "set redelivered after it") {
		t.Errorf("report does not name the redelivery:\n%s", rep)
	}
	wantTheorems(t, rep, "live_free_alo_exact", "barrier_free_alo_exact")
}

// counterRegistry is an Int n in 0..3 with one event inc: "count" adds 1 (saturating at 3),
// "atLeastOne" raises n to at least 1.
func counterRegistry(name, kind string) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	n := r.Int("n", 0, 3)
	r.Event("inc").Writes(n).Apply(func(s gsm.State) gsm.State {
		if kind == "count" {
			return s.SetInt(n, min(s.GetInt(n)+1, 3))
		}
		return s.SetInt(n, max(s.GetInt(n), 1))
	}).Add()
	return r, n
}

// count_dup_prefix: A counts, B's image saturates at 1, the migration the identity. In the
// Coq instance each event is delivered at most once exactly-once, so the exactly-once live
// switch is online and only the at-least-once one meets the duplicate prefix [inc, inc]. In
// gsm's model an event may be submitted any number of times, so [inc, inc] is already a run
// under exactly-once delivery: DS1 fails at 1 under both classes, as live_free_alo_exact's S1
// at that duplicate prefix does. At least once the change is moreover unsafe: two runs of A
// with the same set of events (inc once, inc twice) migrate apart (AmodA).
func TestCheckMigrationDelivery_CountDupPrefix(t *testing.T) {
	a, na := counterRegistry("A", "count")
	b, nb := counterRegistry("B", "atLeastOne")
	id := func(old, blank gsm.State) gsm.State { return blank.SetInt(nb, old.GetInt(na)) }

	rep := mustCheck(t, a, b, id, nil)
	wantOutcome(t, rep, gsm.MigrationSafeBehindBarrier, gsm.MigrationDS1)
	if c := cond(t, rep, gsm.MigrationDS1); !strings.HasPrefix(c.Detail, "at {n=1}") {
		t.Errorf("DS1 fails at %q, want at {n=1}", c.Detail)
	}

	rep = mustCheck(t, a, b, id, nil, alo())
	wantOutcome(t, rep, gsm.MigrationUnsafe, gsm.MigrationAmodA)
	for _, k := range []string{gsm.MigrationPermBStart, gsm.MigrationIdemStart, gsm.MigrationAbsorbS} {
		if c := cond(t, rep, k); !c.Holds {
			t.Errorf("%s: %+v", k, c)
		}
	}
	if c := cond(t, rep, gsm.MigrationDS1); c.Holds {
		t.Error("DS1 holds at least once")
	}
	w := rep.BarrierWitness
	if w == nil || !w.Redelivery || w.Run1.After != nil || w.Run2.After != nil {
		t.Fatalf("barrier witness %+v", w)
	}
}

// A redelivered event of B that is not idempotent: the same counter on both sides is online
// exactly once and unsafe at least once, by Idem-every (and the live check fails Idem-start).
func TestCheckMigrationDelivery_IdempotenceNeeded(t *testing.T) {
	a, na := counterRegistry("A", "count")
	b, nb := counterRegistry("B", "count")
	id := func(old, blank gsm.State) gsm.State { return blank.SetInt(nb, old.GetInt(na)) }
	wantOutcome(t, mustCheck(t, a, b, id, nil), gsm.MigrationSafeOnline, "")
	rep := mustCheck(t, a, b, id, nil, alo())
	wantOutcome(t, rep, gsm.MigrationUnsafe, gsm.MigrationIdemEvery)
	if c := cond(t, rep, gsm.MigrationIdemStart); c.Holds {
		t.Errorf("Idem-start: %+v", c)
	}
	if w := rep.LiveWitness; w == nil || w.Condition != gsm.MigrationIdemStart || !w.Redelivery {
		t.Fatalf("live witness %+v", w)
	}
}

// maxRegistry is a max-register Int v in 0..hi with events m<k> for k in step, 2*step, ...,
// hi: v := max(v, k).
func maxRegistry(name string, hi, step int) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	v := r.Int("v", 0, hi)
	for k := step; k <= hi; k += step {
		k := k
		r.Event("m" + string(rune('0'+k))).Writes(v).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(v, max(s.GetInt(v), k)) }).Add()
	}
	return r, v
}

// rescaled_max: max-registers, the migration and the event map doubling. Every condition of
// every class holds: safe online exactly once, under declared pairs, and at least once.
func TestCheckMigrationDelivery_RescaledMax(t *testing.T) {
	build := func(declare bool) (*gsm.Registry, *gsm.Registry, gsm.Migration) {
		a, va := maxRegistry("A", 3, 1)
		b, vb := maxRegistry("B", 6, 2)
		if declare {
			a.Independent("m1", "m2")
			b.Independent("m2", "m6")
		}
		return a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(vb, 2*old.GetInt(va)) }
	}
	events := map[string]string{"m1": "m2", "m2": "m4", "m3": "m6"}
	a, b, mig := build(false)
	wantOutcome(t, mustCheck(t, a, b, mig, events), gsm.MigrationSafeOnline, "")
	rep := mustCheck(t, a, b, mig, events, alo())
	wantOutcome(t, rep, gsm.MigrationSafeOnline, "")
	wantTheorems(t, rep, "live_free_alo_exact")
	a, b, mig = build(true)
	rep = mustCheck(t, a, b, mig, events)
	wantOutcome(t, rep, gsm.MigrationSafeOnline, "")
	wantTheorems(t, rep, "live_declared_exact")
	if _, err := gsm.CheckMigration(a, b, mig, events, alo()); err == nil {
		t.Error("declared pairs accepted under at-least-once delivery")
	}
}

// Declared independence relaxes every condition to the declared pairs. A and B are
// last-writer-wins on {0, 1, 2}, the migration the identity. Under free delivery B diverges
// after every switch: unsafe. Declaring nothing (every pair delivered in one fixed order),
// the change is online. Declaring A's pair only, the translated pair must commute from the
// migrated start (it does not: not online), B's undeclared pair is not checked at a barrier,
// and the AmodM closure seeded with A's declared pair finds a witness: unsafe.
func TestCheckMigrationDelivery_DeclaredSeeds(t *testing.T) {
	build := func() (*gsm.Registry, *gsm.Registry, gsm.Migration) {
		a, xa := lwwRegistry("A")
		b, xb := lwwRegistry("B")
		return a, b, func(old, blank gsm.State) gsm.State { return blank.SetInt(xb, old.GetInt(xa)) }
	}
	a, b, mig := build()
	wantOutcome(t, mustCheck(t, a, b, mig, nil), gsm.MigrationUnsafe, gsm.MigrationPermBEvery)

	a, b, mig = build()
	a.OnlyDeclaredPairs()
	b.OnlyDeclaredPairs()
	wantOutcome(t, mustCheck(t, a, b, mig, nil), gsm.MigrationSafeOnline, "")

	a, b, mig = build()
	a.Independent("set1", "set2")
	b.OnlyDeclaredPairs()
	rep := mustCheck(t, a, b, mig, nil)
	wantOutcome(t, rep, gsm.MigrationUnsafe, gsm.MigrationAmodM)
	if c := cond(t, rep, gsm.MigrationPermBEvery); !c.Holds {
		t.Errorf("PermB-every: %+v", c)
	}
	if w := rep.LiveWitness; w == nil || len(w.Run1.InFlight) != 2 {
		t.Fatalf("live witness %+v: want the declared pair of A in flight", w)
	}
	wantTheorems(t, rep, "barrier_declared_exact", "closureI_witness_exact")
}

// The closure seeded with declared pairs certifies a barrier: merged_barrier with A's pair
// declared. A is last-writer-wins, the migration merges both writes, B ignores the
// translated events (an in-flight write is lost: DS1 fails). The declared pair diverges
// under A, the migration absorbs it, and the exhausted search certifies the barrier.
func TestCheckMigrationDelivery_DeclaredClosureCertifies(t *testing.T) {
	a, xa := lwwRegistry("A")
	a.Independent("set1", "set2")
	b, fb := flagRegistry("B", "noop")
	b.OnlyDeclaredPairs()
	merge := func(old, blank gsm.State) gsm.State { return blank.SetBool(fb, old.GetInt(xa) != 0) }
	rep := mustCheck(t, a, b, merge, map[string]string{"set1": "noop", "set2": "noop"})
	wantOutcome(t, rep, gsm.MigrationSafeBehindBarrier, gsm.MigrationDS1)
	if c := cond(t, rep, gsm.MigrationAmodM); !c.Decided || !c.Holds {
		t.Errorf("AmodM: %+v", c)
	}
	wantTheorems(t, rep, "barrier_declared_exact", "closureI_swap_exact", "closureI_witness_exact")
}

// AmodA certifies a barrier at least once: A raises a flag, the migration sends it to 2,
// B's event raises its counter to at least 1. An in-flight set gives 1 where set then the
// switch gives 2 (DS1 fails); B's event is idempotent, absorbs a redelivery at 2, and each
// set of events of A migrates to one state.
func TestCheckMigrationDelivery_AmodACertifies(t *testing.T) {
	a, fa := flagRegistry("A", "set")
	b, nb := counterRegistry("B", "atLeastOne")
	mig := func(old, blank gsm.State) gsm.State {
		if old.GetBool(fa) {
			return blank.SetInt(nb, 2)
		}
		return blank
	}
	events := map[string]string{"set": "inc"}
	wantOutcome(t, mustCheck(t, a, b, mig, events), gsm.MigrationSafeBehindBarrier, gsm.MigrationDS1)
	rep := mustCheck(t, a, b, mig, events, alo())
	wantOutcome(t, rep, gsm.MigrationSafeBehindBarrier, gsm.MigrationDS1)
	for _, k := range []string{gsm.MigrationIdemEvery, gsm.MigrationAbsorbS, gsm.MigrationAmodA} {
		if c := cond(t, rep, k); !c.Decided || !c.Holds {
			t.Errorf("%s: %+v", k, c)
		}
	}
	wantTheorems(t, rep, "barrier_free_alo_exact")
}

func TestCheckMigrationDelivery_Refusals(t *testing.T) {
	a, fa := flagRegistry("A", "set")
	b, fb := flagRegistry("B", "set", "toggle")
	mig := func(old, blank gsm.State) gsm.State { return blank.SetBool(fb, old.GetBool(fa)) }
	if _, err := gsm.CheckMigration(a, b, mig, nil, gsm.MigrationInFlightIndependent("nope", "toggle")); err == nil ||
		!strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("unknown in-flight event: %v", err)
	}
	if _, err := gsm.CheckMigration(a, b, mig, nil, gsm.MigrationInFlightIndependent("set", "nope")); err == nil ||
		!strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("unknown event of to: %v", err)
	}
	if _, err := gsm.CheckMigration(a, b, mig, nil, alo(), gsm.MigrationInFlightIndependent("set", "toggle")); err == nil {
		t.Error("a cross pair accepted under at-least-once delivery")
	}
	if _, err := gsm.CheckMigration(a, b, mig, nil, gsm.MigrationDeliveryClass(7)); err == nil {
		t.Error("an unknown delivery class accepted")
	}
}
