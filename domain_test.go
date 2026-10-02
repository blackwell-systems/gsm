package gsm

import (
	"fmt"
	"strings"
	"testing"
)

// Regression tests for out-of-domain closure results (CHANGELOG, Security).
//
// Event effects, invariant repairs, morphism maps and resolvers are Go closures
// that return a State. Build used to record whatever they returned in its tables
// without checking that it was a state of the machine being built: a state carrying
// another machine's variable layout, a value outside a variable's range, or bits
// outside the encoding. Build then certified the machine, and Apply could return a
// state outside the valid states the certificate covers.

// catchPanic runs f and returns the panic message, or "" if f returned normally.
func catchPanic(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

// buildNoPanic calls Build and turns a panic into a test failure, so a check that
// used to crash Build reports which case crashed.
func buildNoPanic(t *testing.T, r *Registry) (m *Machine, rep *Report, err error) {
	t.Helper()
	if msg := catchPanic(func() { m, rep, err = r.Build() }); msg != "" {
		t.Fatalf("Build panicked instead of returning an error: %s", msg)
	}
	return m, rep, err
}

// rawState returns s with v's raw field set to raw, bypassing the domain checks of
// Set/SetInt (the same write MergeProjection performs for a raw projection value).
func rawState(s State, v Var, raw uint64) State { return s.setRaw(v, raw) }

// capped: n ranges over 0..2 and may not be 1. The repair and the set_one effect
// are supplied by the test.
func capped(name string, repair func(s State, n Var) State, effect func(s State, n Var) State) *Registry {
	r := NewRegistry(name)
	n := r.Int("n", 0, 2)
	r.Invariant("not_one").Watches(n).
		Holds(func(s State) bool { return s.GetInt(n) != 1 }).
		Repair(func(s State) State { return repair(s, n) }).Add()
	r.Event("set_one").Writes(n).Apply(func(s State) State { return effect(s, n) }).Add()
	return r
}

func repairToZero(s State, n Var) State { return s.SetInt(n, 0) }
func effectSetOne(s State, n Var) State { return s.SetInt(n, 1) }

// foreignMachine builds a rule-free machine from the given variable declarations.
func foreignMachine(t *testing.T, name string, decl func(r *Registry)) *Machine {
	t.Helper()
	r := NewRegistry(name)
	decl(r)
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type oodCase struct {
	name   string
	rule   string // the event or invariant the error must name
	bad    string // a fragment of the bad result or the reason the error must show
	mkReg  func(t *testing.T) *Registry
	reason string // a fragment of the reason the error must give
}

func oodCases() []oodCase {
	// wider: a machine whose "n" has the same name and position but range 0..3.
	wider := func(t *testing.T) *Machine {
		return foreignMachine(t, "wider", func(r *Registry) { r.Int("n", 0, 3) })
	}
	// longer: the same "n", plus a variable this machine does not have.
	longer := func(t *testing.T) *Machine {
		return foreignMachine(t, "longer", func(r *Registry) { r.Int("n", 0, 2); r.Bool("extra") })
	}
	return []oodCase{
		{
			name: "repair/value-out-of-range", rule: `invariant "not_one"`, bad: "n=3", reason: `"n" holds 3, outside 0..2`,
			mkReg: func(t *testing.T) *Registry {
				return capped("ood_repair_range", func(s State, n Var) State { return rawState(s, n, 3) }, effectSetOne)
			},
		},
		{
			name: "repair/merge-projection", rule: `invariant "not_one"`, bad: "n=3", reason: `"n" holds 3, outside 0..2`,
			mkReg: func(t *testing.T) *Registry {
				twin := foreignMachine(t, "twin", func(r *Registry) { r.Int("n", 0, 2) })
				return capped("ood_repair_merge", func(s State, n Var) State {
					out, err := twin.MergeProjection(s, Projection{Shared: map[string]uint64{"n": 3}})
					if err != nil {
						panic(err)
					}
					return out
				}, effectSetOne)
			},
		},
		{
			name: "repair/other-schema", rule: `invariant "not_one"`, bad: "n=3", reason: "variable 0",
			mkReg: func(t *testing.T) *Registry {
				w := wider(t)
				return capped("ood_repair_schema", func(s State, n Var) State {
					return w.NewState().SetInt(w.vars[0], 3)
				}, effectSetOne)
			},
		},
		{
			name: "repair/extra-variable", rule: `invariant "not_one"`, bad: "extra=true", reason: "2 variables",
			mkReg: func(t *testing.T) *Registry {
				l := longer(t)
				return capped("ood_repair_extra", func(s State, n Var) State {
					return l.NewState().SetBool(l.vars[1], true)
				}, effectSetOne)
			},
		},
		{
			name: "repair/zero-value-state", rule: `invariant "not_one"`, bad: "State(0)", reason: "0 variables",
			mkReg: func(t *testing.T) *Registry {
				return capped("ood_repair_zero", func(s State, n Var) State { return State{} }, effectSetOne)
			},
		},
		{
			name: "effect/value-out-of-range", rule: `event "set_one"`, bad: "n=3", reason: `"n" holds 3, outside 0..2`,
			mkReg: func(t *testing.T) *Registry {
				return capped("ood_effect_range", repairToZero, func(s State, n Var) State { return rawState(s, n, 3) })
			},
		},
		{
			name: "effect/extra-variable", rule: `event "set_one"`, bad: "extra=true", reason: "2 variables",
			mkReg: func(t *testing.T) *Registry {
				l := longer(t)
				return capped("ood_effect_extra", repairToZero, func(s State, n Var) State {
					return l.NewState().SetBool(l.vars[1], true)
				})
			},
		},
		{
			name: "effect/high-bits", rule: `event "set_one"`, bad: "n=0", reason: "bits outside",
			mkReg: func(t *testing.T) *Registry {
				return capped("ood_effect_bits", repairToZero, func(s State, n Var) State {
					return State{packed: s.packed | 1<<40, vars: s.vars}
				})
			},
		},
	}
}

// TestBuild_RejectsOutOfDomainResults: Build refuses a machine whose effect or
// repair returns something that is not a state of the machine, and names the rule,
// the input state and the bad result.
func TestBuild_RejectsOutOfDomainResults(t *testing.T) {
	for _, c := range oodCases() {
		t.Run(c.name, func(t *testing.T) {
			m, rep, err := buildNoPanic(t, c.mkReg(t))
			if err == nil {
				s := m.Apply(m.NewState(), "set_one")
				t.Fatalf("Build certified the machine; Apply(set_one) from the zero state returns %s (encoding %d)\n%s",
					s, s.ID(), rep)
			}
			msg := err.Error()
			for _, want := range []string{c.rule, "not a state of machine", c.bad, c.reason} {
				if !strings.Contains(msg, want) {
					t.Errorf("error does not mention %q: %v", want, err)
				}
			}
			// The input state is named: the effect runs on the zero state ({n=0}),
			// the repair on the only state violating not_one ({n=1}).
			in := "{n=0}"
			if strings.HasPrefix(c.name, "repair/") {
				in = "{n=1}"
			}
			if !strings.Contains(msg, "on state "+in) {
				t.Errorf("error does not name the input state %s: %v", in, err)
			}
		})
	}
}

// TestBuildCompositional_RejectsOutOfDomainResults: the compositional path checks
// every closure result it computes, as Build does.
func TestBuildCompositional_RejectsOutOfDomainResults(t *testing.T) {
	for _, c := range oodCases() {
		t.Run(c.name, func(t *testing.T) {
			r := c.mkReg(t)
			var err error
			var m *Machine
			if msg := catchPanic(func() { m, _, err = r.BuildCompositional() }); msg != "" {
				t.Fatalf("BuildCompositional panicked instead of returning an error: %s", msg)
			}
			if err == nil {
				s := m.Apply(m.NewState(), "set_one")
				t.Fatalf("BuildCompositional certified the machine; Apply(set_one) returns %s (encoding %d)", s, s.ID())
			}
			if !strings.Contains(err.Error(), c.rule) || !strings.Contains(err.Error(), "not a state of machine") {
				t.Fatalf("want an out-of-domain error naming %s, got: %v", c.rule, err)
			}
		})
	}
}

// TestSynthesize_RejectsOutOfDomainEffect: synthesis tabulates every effect, so it
// refuses an effect that leaves the domain instead of searching over it.
func TestSynthesize_RejectsOutOfDomainEffect(t *testing.T) {
	for _, c := range oodCases() {
		if !strings.HasPrefix(c.name, "effect/") {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			r := c.mkReg(t)
			var err error
			if msg := catchPanic(func() { _, err = r.Synthesize() }); msg != "" {
				t.Fatalf("Synthesize panicked instead of returning an error: %s", msg)
			}
			if err == nil || !strings.Contains(err.Error(), "not a state of machine") {
				t.Fatalf("Synthesize: want an out-of-domain error, got: %v", err)
			}
			if msg := catchPanic(func() { _, _, err = r.BuildOrSynthesize() }); msg != "" {
				t.Fatalf("BuildOrSynthesize panicked instead of returning an error: %s", msg)
			}
			if err == nil || !strings.Contains(err.Error(), "not a state of machine") {
				t.Fatalf("BuildOrSynthesize: want an out-of-domain error, got: %v", err)
			}
		})
	}
}

// TestBuild_AcceptsStructurallyIdenticalState: a state built from another machine
// instance with the same variable schema is a state of this machine. Membership is
// by value, not by which Registry or Machine created the state.
func TestBuild_AcceptsStructurallyIdenticalState(t *testing.T) {
	twin := foreignMachine(t, "twin", func(r *Registry) { r.Int("n", 0, 2) })
	tn := twin.vars[0]
	fromTwin := func(s State, n Var) State { return twin.NewState().SetInt(tn, 2) }
	r := capped("identical_schema", fromTwin, fromTwin)
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("Build rejected a state with an identical schema: %v\n%s", err, rep)
	}
	if got := m.Apply(m.NewState(), "set_one"); got.GetInt(r.vars[0]) != 2 {
		t.Fatalf("Apply(set_one) = %s, want n=2", got)
	}

	// The same through the lazy runtime, which checks at Apply time.
	lm, _, err := capped("identical_schema_lazy", fromTwin, fromTwin).BuildCompositional()
	if err != nil {
		t.Fatalf("BuildCompositional rejected a state with an identical schema: %v", err)
	}
	if msg := catchPanic(func() { lm.Apply(lm.NewState(), "set_one") }); msg != "" {
		t.Fatalf("lazy Apply rejected a state with an identical schema: %s", msg)
	}
}

// TestLazyApply_PanicsOnOutOfDomainResult: a lazy (BuildCompositional) machine
// runs its closures at Apply time, where Build cannot have seen the result. A
// closure that leaves the domain only after the build (here, behind a switch) must
// make Apply panic, as Apply does for an unknown event, instead of returning a
// state outside the certified space.
func TestLazyApply_PanicsOnOutOfDomainResult(t *testing.T) {
	l := foreignMachine(t, "longer", func(r *Registry) { r.Int("n", 0, 2); r.Bool("extra") })
	cases := []struct {
		name, rule string
		mk         func(leak *bool) *Registry
	}{
		{"effect", `event "set_one"`, func(leak *bool) *Registry {
			return capped("lazy_effect", repairToZero, func(s State, n Var) State {
				if *leak {
					return l.NewState().SetBool(l.vars[1], true)
				}
				return s.SetInt(n, 1)
			})
		}},
		{"repair", `invariant "not_one"`, func(leak *bool) *Registry {
			return capped("lazy_repair", func(s State, n Var) State {
				if *leak {
					return rawState(s, n, 3)
				}
				return s.SetInt(n, 0)
			}, effectSetOne)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			leak := false
			m, _, err := c.mk(&leak).BuildCompositional()
			if err != nil {
				t.Fatal(err)
			}
			leak = true
			var got State
			msg := catchPanic(func() { got = m.Apply(m.NewState(), "set_one") })
			if msg == "" {
				t.Fatalf("lazy Apply returned %s (encoding %d), outside the machine's domain", got, got.ID())
			}
			if !strings.Contains(msg, c.rule) || !strings.Contains(msg, "not a state of machine") {
				t.Fatalf("panic does not name %s as out of domain: %s", c.rule, msg)
			}
		})
	}
}

// fedPair: source "src" (a Bool) and target "dst" (n over 0..2, shared) joined by a
// morphism whose Map is supplied by the test.
func fedPair(mapFn func(src, d State, n Var) State) (*Federation, *Registry) {
	src := NewRegistry("src")
	src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	f := NewFederation("ood_fed").Morphism(src, dst).Shared(n).
		Map(func(s, d State) State { return mapFn(s, d, n) }).Add()
	return f, dst
}

// TestFederationBuild_RejectsOutOfDomainMap: a morphism Map (and a Resolver) is a
// closure producing the target's state; Build checks every image it computes.
func TestFederationBuild_RejectsOutOfDomainMap(t *testing.T) {
	f, _ := fedPair(func(s, d State, n Var) State { return rawState(d, n, 3) })
	var err error
	if msg := catchPanic(func() { _, _, err = f.Build() }); msg != "" {
		t.Fatalf("Federation.Build panicked instead of returning an error: %s", msg)
	}
	if err == nil || !strings.Contains(err.Error(), "not a state of") || !strings.Contains(err.Error(), "src→dst") {
		t.Fatalf("want an out-of-domain error naming the morphism src→dst, got: %v", err)
	}

	// Resolver: two sources into one target.
	a := NewRegistry("ra")
	a.Bool("x")
	b := NewRegistry("rb")
	b.Bool("y")
	dst := NewRegistry("rdst")
	n := dst.Int("n", 0, 2)
	keep := func(s, d State) State { return d }
	rf := NewFederation("ood_resolver").
		Morphism(a, dst).Shared(n).Map(keep).Add().
		Morphism(b, dst).Shared(n).Map(keep).Add().
		Resolve(dst, func(d State, _ map[string]State) State { return rawState(d, n, 3) })
	if msg := catchPanic(func() { _, _, err = rf.Build() }); msg != "" {
		t.Fatalf("Federation.Build panicked instead of returning an error: %s", msg)
	}
	if err == nil || !strings.Contains(err.Error(), "not a state of") || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("want an out-of-domain error naming the resolver, got: %v", err)
	}
}

// TestFedMachine_PanicsOnOutOfDomainMap: FedMachine.Normalize runs morphism maps at
// Apply time, so it checks their results as lazy Apply does.
func TestFedMachine_PanicsOnOutOfDomainMap(t *testing.T) {
	leak := false
	f, dst := fedPair(func(s, d State, n Var) State {
		if leak {
			return rawState(d, n, 3)
		}
		return d.SetInt(n, 2)
	})
	fm, _, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	leak = true
	var got FedState
	msg := catchPanic(func() { got = fm.Normalize(fm.NewState()) })
	if msg == "" {
		s := fm.Of(got, dst)
		t.Fatalf("FedMachine.Normalize returned target %s (encoding %d), outside its domain", s, s.ID())
	}
	if !strings.Contains(msg, "src→dst") || !strings.Contains(msg, "not a state of") {
		t.Fatalf("panic does not name the morphism as out of domain: %s", msg)
	}
}

// TestCertificateVerify_RejectsOutOfDomainTableValue: a certificate's tables are
// data, bound to the subsystem only by a digest anyone can recompute. Verify must
// refuse a recorded shared value outside the variable's domain rather than write it
// into the target and test validity on the result.
func TestCertificateVerify_RejectsOutOfDomainTableValue(t *testing.T) {
	f, dst := fedPair(func(s, d State, n Var) State { return d.SetInt(n, 2) })
	cert, err := f.Certify()
	if err != nil {
		t.Fatal(err)
	}
	src := f.comps[0]
	comps := map[string]*Registry{"src": src, "dst": dst}
	if err := cert.Verify(comps); err != nil {
		t.Fatalf("the untampered certificate does not verify: %v", err)
	}
	cert.Tables[0].Rows[0].Values[0] = 3 // n's raw field holds 0..2
	if cert.Digest, err = digestComponentsAndTables(f.comps, cert.Tables, cert.Monotone, cert.InputPorts); err != nil {
		t.Fatal(err)
	}
	err = cert.Verify(comps)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Verify accepted a table value outside the variable's domain: %v", err)
	}

	cert.Tables[0].Rows[0].Values = nil // fewer values than shared variables
	if cert.Digest, err = digestComponentsAndTables(f.comps, cert.Tables, cert.Monotone, cert.InputPorts); err != nil {
		t.Fatal(err)
	}
	if msg := catchPanic(func() { err = cert.Verify(comps) }); msg != "" {
		t.Fatalf("Verify panicked on a short table row: %s", msg)
	}
	if err == nil {
		t.Fatal("Verify accepted a table row with fewer values than shared variables")
	}
}
