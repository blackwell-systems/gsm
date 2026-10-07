package gsm

import (
	"strings"
	"testing"
)

// Internal tests for CheckMigration's limits (migration.go): an instance too large to
// enumerate fails clearly, on either side, and a search for an AmodM witness that reaches
// the limit is reported as stopped, with the outcome Unknown.

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
// limit just above A's states stops it: Unknown, with the search reported as stopped.
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
	// With room for the whole search, it is exhaustive and finds no witness: still Unknown.
	rep, err = CheckMigration(a, b, mig, events)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != MigrationUnknown || rep.SearchStopped != "" {
		t.Fatalf("outcome %v, search %q, want UNKNOWN with an exhaustive search", rep.Outcome, rep.SearchStopped)
	}
}
