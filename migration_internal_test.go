package gsm

import (
	"strings"
	"testing"
)

// Internal tests for CheckMigration's limits (migration.go): an instance too large to
// enumerate fails clearly, on either side, and a search for an AmodM witness that reaches
// the limit is reported as stopped, with the outcome Unknown: the only case that is Unknown.

func migCounter(name string, max int) (*Registry, Var) {
	r := NewRegistry(name)
	c := r.Int("c", 0, max)
	r.Event("inc").Writes(c).Apply(func(s State) State { return s.SetInt(c, s.GetInt(c)+1) }).Add()
	return r, c
}

func TestCheckMigration_TooLargeFrom(t *testing.T) {
	a, ca := migCounter("big", 99)
	b, cb := migCounter("big2", 99)
	mig := func(old, blank State) State { return blank.SetInt(cb, old.GetInt(ca)) }
	_, err := CheckMigration(a, b, mig, nil, migrationLimit(10))
	if err == nil || !strings.Contains(err.Error(), `registry "big" reaches more than 10 states`) {
		t.Fatalf("err = %v", err)
	}
	if rep, err := CheckMigration(a, b, mig, nil, migrationLimit(100)); err != nil || rep.Outcome != MigrationSafeOnline {
		t.Fatalf("within the limit: %v, %v", rep, err)
	}
}

func TestCheckMigration_TooLargeTo(t *testing.T) {
	a := NewRegistry("small")
	a.Bool("f")
	a.Event("inc").Apply(func(s State) State { return s }).Add()
	b, _ := migCounter("big", 99)
	_, err := CheckMigration(a, b, func(old, blank State) State { return blank }, nil, migrationLimit(10))
	if err == nil || !strings.Contains(err.Error(), `registry "big" reaches more than 10 states`) {
		t.Fatalf("err = %v", err)
	}
}

// A: z in Z_16 with p (doubling) and q (successor), which do not commute anywhere; the
// migration forgets z, so it is not injective and absorbs every divergence; B's one event
// flips a bit, so DS1 fails. The pairs the AmodM search explores outnumber A's states, so a
// limit just above A's states stops it: Unknown, with the search reported as stopped. With
// room for the whole search it is exhausted with no witness, which certifies the barrier.
func TestCheckMigration_AmodMSearchStops(t *testing.T) {
	a := NewRegistry("ring")
	z := a.Int("z", 0, 15)
	a.Event("p").Writes(z).Apply(func(s State) State { return s.SetInt(z, (2*s.GetInt(z))%16) }).Add()
	a.Event("q").Writes(z).Apply(func(s State) State { return s.SetInt(z, (s.GetInt(z)+1)%16) }).Add()
	b := NewRegistry("bit")
	y := b.Bool("y")
	b.Event("flip").Writes(y).Apply(func(s State) State { return s.SetBool(y, !s.GetBool(y)) }).Add()
	mig := func(old, blank State) State { return blank }
	events := map[string]string{"p": "flip", "q": "flip"}

	rep, err := CheckMigration(a, b, mig, events, migrationLimit(20))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != MigrationUnknown || rep.SearchStopped == "" {
		t.Fatalf("outcome %v, search %q, want UNKNOWN with the search stopped\n%s", rep.Outcome, rep.SearchStopped, rep)
	}
	if c, _ := rep.Condition(MigrationAmodM); c.Decided || !strings.Contains(c.Detail, "stopped") {
		t.Fatalf("AmodM: %+v", c)
	}
	if rep.Failed != MigrationAmodM || !strings.Contains(rep.Reason, "size limit") ||
		!strings.Contains(rep.String(), "[AmodM]: NOT DECIDED") {
		t.Fatalf("failed %q, reason %q\n%s", rep.Failed, rep.Reason, rep)
	}
	// With room for the whole search, it is exhausted with no witness: a certificate
	// (amodm_witness_exact), so the change is safe behind a barrier.
	rep, err = CheckMigration(a, b, mig, events)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != MigrationSafeBehindBarrier || rep.SearchStopped != "" {
		t.Fatalf("outcome %v, search %q, want SAFE BEHIND A BARRIER with an exhaustive search", rep.Outcome, rep.SearchStopped)
	}
	if c, _ := rep.Condition(MigrationAmodM); !c.Decided || !c.Holds {
		t.Fatalf("AmodM: %+v", c)
	}
}

// At-least-once delivery: the AmodA search (pairs of a state and the set of events applied)
// is the only one that stops at the limit. A: z in Z_16 with p (doubling) and q
// (successor), and a flag g raised by r; the migration keeps g, and every event of A
// translates to B's event n, which changes nothing. DS1 fails (r then the switch raises the
// flag, the switch then n does not), B's event is idempotent and absorbs every redelivery,
// and each set of events migrates to one state (g is raised iff r was applied). The pairs of
// a state and a set outnumber A's 32 states, so a limit of 40 stops the search: Unknown.
// With room, the exhausted search certifies the barrier.
func TestCheckMigration_AmodASearchStops(t *testing.T) {
	a := NewRegistry("ring")
	z := a.Int("z", 0, 15)
	g := a.Bool("g")
	a.Event("p").Writes(z).Apply(func(s State) State { return s.SetInt(z, (2*s.GetInt(z))%16) }).Add()
	a.Event("q").Writes(z).Apply(func(s State) State { return s.SetInt(z, (s.GetInt(z)+1)%16) }).Add()
	a.Event("r").Writes(g).Apply(func(s State) State { return s.SetBool(g, true) }).Add()
	b := NewRegistry("mark")
	y := b.Bool("y")
	b.Event("n").Writes(y).Apply(func(s State) State { return s }).Add()
	mig := func(old, blank State) State { return blank.SetBool(y, old.GetBool(g)) }
	events := map[string]string{"p": "n", "q": "n", "r": "n"}
	atLeastOnce := MigrationDeliveryClass(MigrationAtLeastOnce)

	rep, err := CheckMigration(a, b, mig, events, atLeastOnce, migrationLimit(40))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != MigrationUnknown || rep.SearchStopped == "" || rep.Failed != MigrationAmodA {
		t.Fatalf("outcome %v, failed %q, search %q, want UNKNOWN with the AmodA search stopped\n%s",
			rep.Outcome, rep.Failed, rep.SearchStopped, rep)
	}
	if c, _ := rep.Condition(MigrationAmodA); c.Decided || !strings.Contains(c.Detail, "stopped") {
		t.Fatalf("AmodA: %+v", c)
	}
	if !strings.Contains(rep.String(), "[AmodA]: NOT DECIDED") {
		t.Fatalf("report:\n%s", rep)
	}
	rep, err = CheckMigration(a, b, mig, events, atLeastOnce)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != MigrationSafeBehindBarrier || rep.Failed != MigrationDS1 || rep.SearchStopped != "" {
		t.Fatalf("outcome %v, failed %q, want SAFE BEHIND A BARRIER with an exhausted AmodA search\n%s",
			rep.Outcome, rep.Failed, rep)
	}
	if c, _ := rep.Condition(MigrationAmodA); !c.Decided || !c.Holds {
		t.Fatalf("AmodA: %+v", c)
	}
}
