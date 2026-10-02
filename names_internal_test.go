package gsm

import (
	"fmt"
	"strings"
	"testing"
)

// TestDuplicateVariableName_VerifyRechecksTheWrongVariable: a certificate table
// names shared variables by name. With two variables named "x" in the target, the
// re-check resolves "x" to the last one while the federated runtime's projection
// merge (MergeProjection) resolves it to the first, so Verify can accept a table
// that breaks validity preservation for the variable the runtime writes.
func TestDuplicateVariableName_VerifyRechecksTheWrongVariable(t *testing.T) {
	s := NewRegistry("s")
	on := s.Bool("on")
	s.DeclEvent("flip", Do(Set(on, Lit(1))))
	target := func(second string) *Registry {
		r := NewRegistry("t")
		first := r.Bool("x")
		r.Bool(second)
		r.DeclInvariant("first_x_clear", Eq(V(first), Lit(0)), Do(Set(first, Lit(0))))
		return r
	}
	tr := target("x")

	// The table writes 1 into "x" for every source state: invalid for the first x
	// (the one MergeProjection writes), harmless for the second.
	tables := []MorphismTable{{
		Target: "t", Sources: []string{"s"}, Shared: []string{"x"},
		Rows: []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{1}}, {SourceIDs: []uint64{1}, Values: []uint64{1}}},
	}}
	// The rules serialization names variables by index, so a twin whose second
	// variable is named "y" has the same policy bytes and so the same digest.
	dig, err := digestComponentsAndTables([]*Registry{s, target("y")}, tables, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cert := &Certificate{Name: "forged", Digest: dig, Tables: tables}
	err = cert.Verify(map[string]*Registry{"s": s, "t": tr})
	if err == nil {
		t.Fatal(`Verify accepted a table that writes 1 into "x" of t, which makes t invalid for the first ` +
			`"x" (the variable the runtime merges into); the re-check resolved "x" to the second variable`)
	}
	if want := `gsm: registry "t": duplicate variable name "x"`; err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
}

// TestVerify_ComponentKeyMustBeItsName: Verify digests the components by their
// registry names but looks a table's target up by map key, so a map whose keys do
// not match the registries' names re-checks a table against the wrong registry.
func TestVerify_ComponentKeyMustBeItsName(t *testing.T) {
	s := NewRegistry("s")
	on := s.Bool("on")
	s.DeclEvent("flip", Do(Set(on, Lit(1))))
	strict := NewRegistry("t") // the table's target: x must stay 0
	x := strict.Bool("x")
	strict.DeclInvariant("x_clear", Eq(V(x), Lit(0)), Do(Set(x, Lit(0))))
	lax := NewRegistry("u") // same variable, no invariant
	lax.Bool("x")

	// The table writes 1 into t.x: a validity-preservation violation for t.
	tables := []MorphismTable{{
		Target: "t", Sources: []string{"s"}, Shared: []string{"x"},
		Rows: []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{1}}, {SourceIDs: []uint64{1}, Values: []uint64{1}}},
	}}
	dig, err := digestComponentsAndTables([]*Registry{s, strict, lax}, tables, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cert := &Certificate{Name: "forged", Digest: dig, Tables: tables}
	if cert.Verify(map[string]*Registry{"s": s, "t": strict, "u": lax}) == nil {
		t.Fatal("premise: Verify must refuse the table against the real target")
	}
	// Repeated, because map order is random: the error must name the same key every time.
	for i := 0; i < 32; i++ {
		err = cert.Verify(map[string]*Registry{"s": s, "t": lax, "u": strict})
		if err == nil {
			t.Fatal(`Verify accepted the table: with the keys swapped the digest still matches (it uses ` +
				`registry names) and the table for "t" was re-checked against registry "u"`)
		}
		if want := `gsm: certificate "forged": component key "t" names registry "u"`; err.Error() != want {
			t.Fatalf("got %q, want %q", err, want)
		}
	}
}

// synthRegistry is a registry Synthesize finds a convergent repair for.
func synthRegistry() (*Registry, Var) {
	r := NewRegistry("syn")
	x := r.Int("x", 0, 2)
	r.Invariant("cap").Watches(x).Holds(func(s State) bool { return s.GetInt(x) <= 1 }).Add()
	r.Event("a").Writes(x).Apply(func(s State) State { return s.SetInt(x, 1) }).Add()
	r.Event("b").Writes(x).Apply(func(s State) State { return s }).Add()
	return r, x
}

// TestSynthesisMachine_UnaffectedByLaterDuplicate: Synthesis.Machine is the
// machine that was synthesized. An event declared on the registry afterwards
// (here a duplicate "a") does not reach it, and every other path rejects the
// registry.
func TestSynthesisMachine_UnaffectedByLaterDuplicate(t *testing.T) {
	r, x := synthRegistry()
	s, err := r.Synthesize()
	if err != nil || !s.Convergent {
		t.Fatalf("premise: synthesis converges: %v %v", err, s)
	}
	r.Event("a").Writes(x).Apply(func(s State) State { return s.SetInt(x, 0) }).Add()
	r.Bool("late")
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("the synthesized machine panics after a duplicate was declared on its registry: %v", p)
		}
	}()

	m := s.Machine()
	if got := fmt.Sprint(m.Events()); got != "[a b]" {
		t.Fatalf("synthesized machine's events are %s, want [a b]", got)
	}
	if got := m.Apply(m.NewState(), "a"); got.GetInt(x) != 1 {
		t.Fatalf("Apply(\"a\") ran the event declared after synthesis: %s", got)
	}
	if got := m.NewState().String(); strings.Contains(got, "late") {
		t.Fatalf("the synthesized machine's state carries a variable declared afterwards: %s", got)
	}
	if got := s.Repairs()[0][0].String(); strings.Contains(got, "late") {
		t.Fatalf("a synthesized repair carries a variable declared afterwards: %s", got)
	}
	if _, _, err := r.Build(); err == nil {
		t.Fatal("Build accepted the registry with the duplicate")
	}
	if _, err := r.Synthesize(); err == nil {
		t.Fatal("Synthesize accepted the registry with the duplicate")
	}
}

// TestSynthesisMachine_UnaffectedByLaterIndependent: a pair declared Independent
// after Synthesize was never checked, so the machine and its tables must not
// claim it.
func TestSynthesisMachine_UnaffectedByLaterIndependent(t *testing.T) {
	r := NewRegistry("syn2")
	x := r.Bool("x")
	r.Invariant("any").Watches(x).Holds(func(s State) bool { return true }).Add()
	r.Event("up").Writes(x).Apply(func(s State) State { return s.SetBool(x, true) }).Add()
	r.Event("down").Writes(x).Apply(func(s State) State { return s.SetBool(x, false) }).Add()
	r.Event("noop").Writes(x).Apply(func(s State) State { return s }).Add()
	r.Independent("up", "noop")
	s, err := r.Synthesize()
	if err != nil || !s.Convergent {
		t.Fatalf("premise: synthesis converges: %v %v", err, s)
	}
	r.Independent("up", "down") // up;down != down;up

	b, err := s.Machine().convergenceTables()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(string(b), "\n")[3]; got != "pairs 1 0 2" {
		t.Fatalf("tables claim %q, want only the pair synthesis checked: %q", got, "pairs 1 0 2")
	}

	// Synthesized in all-pairs mode; a later Independent switches the registry to
	// declared-only mode, but the machine was checked for every pair.
	r3 := NewRegistry("syn3")
	y := r3.Bool("y")
	r3.Event("up").Writes(y).Apply(func(s State) State { return s.SetBool(y, true) }).Add()
	r3.Event("noop").Writes(y).Apply(func(s State) State { return s }).Add()
	s3, err := r3.Synthesize()
	if err != nil || !s3.Convergent {
		t.Fatalf("premise: synthesis converges: %v %v", err, s3)
	}
	r3.Independent("up", "noop")
	b, err = s3.Machine().convergenceTables()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(string(b), "\n")[3]; got != "pairs all" {
		t.Fatalf("tables claim %q, want %q", got, "pairs all")
	}
}

// midRunRegistry returns a registry whose invariant check declares one more event
// named `extra` the first time it runs, so the declaration lands after the name
// check and before the result is built.
func midRunRegistry(name, extra string, withRepair bool) *Registry {
	r := NewRegistry(name)
	x := r.Int("x", 0, 2)
	declared := false
	ib := r.Invariant("cap").Watches(x).Holds(func(s State) bool {
		if !declared {
			declared = true
			r.Event(extra).Writes(x).Apply(func(s State) State { return s }).Add()
		}
		return s.GetInt(x) <= 1
	})
	if withRepair {
		ib.Repair(func(s State) State { return s.SetInt(x, 1) })
	}
	ib.Add()
	r.Event("a").Writes(x).Apply(func(s State) State { return s.SetInt(x, 1) }).Add()
	r.Event("b").Writes(x).Apply(func(s State) State { return s }).Add()
	return r
}

// wantModified fails unless err is the rejection of a registry changed while it
// was being verified.
func wantModified(t *testing.T, err error, reg string) {
	t.Helper()
	want := `gsm: registry "` + reg + `" was changed while it was being verified (a rule declared ` +
		`a variable, invariant, event, or Independent pair)`
	if err == nil {
		t.Fatalf("accepted a registry that gained an event during verification; want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
}

// TestDeclareDuringVerification: a rule closure that declares an event while the
// registry is being verified (here a duplicate "a", and a new unique name) must
// not yield a result built from a registry other than the one checked.
func TestDeclareDuringVerification(t *testing.T) {
	for _, extra := range []string{"a", "c"} {
		t.Run("Synthesize/"+extra, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panicked: %v", p)
				}
			}()
			s, err := midRunRegistry("mid", extra, false).Synthesize()
			if err == nil && s.Convergent {
				m := s.Machine()
				t.Logf("events %v, Apply(%q) = %s", m.Events(), extra, m.Apply(m.NewState(), extra))
			}
			wantModified(t, err, "mid")
		})
		t.Run("Build/"+extra, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panicked: %v", p)
				}
			}()
			_, _, err := midRunRegistry("mid", extra, true).Build()
			wantModified(t, err, "mid")
		})
		t.Run("BuildCompositional/"+extra, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panicked: %v", p)
				}
			}()
			_, _, err := midRunRegistry("mid", extra, true).BuildCompositional()
			wantModified(t, err, "mid")
		})
	}
}
