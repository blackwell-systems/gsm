package gsm

import "testing"

// digestOf returns r's policy digest, failing the test on error.
func digestOf(t *testing.T, r *Registry) string {
	t.Helper()
	d, err := r.PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestDeclaredRuleUnaffectedByCallerSlice: a declared rule is a copy of what the
// caller passed. Rewriting an element of the caller's slice afterwards changes
// no count, so the mid-run guard cannot see it; it must not change the rule.
func TestDeclaredRuleUnaffectedByCallerSlice(t *testing.T) {
	newReg := func() (*Registry, Var, Var) {
		r := NewRegistry("alias")
		a := r.Int("a", 0, 3)
		b := r.Int("b", 0, 3)
		return r, a, b
	}
	cases := map[string]func(r *Registry, a, b Var) (mutate func()){
		"DeclEvent from Do(slice...)": func(r *Registry, a, b Var) func() {
			eff := []Assign{Set(a, Lit(1))}
			r.DeclEvent("e", Do(eff...))
			return func() { eff[0] = Set(b, Lit(1)) }
		},
		"DeclEvent from a Transform value": func(r *Registry, a, b Var) func() {
			eff := Do(Set(a, Lit(1)))
			r.DeclEvent("e", eff)
			return func() { eff[0] = Set(b, Lit(1)) }
		},
		"DeclEventGuarded effect": func(r *Registry, a, b Var) func() {
			eff := Do(Set(a, Lit(1)))
			r.DeclEventGuarded("e", Le(V(a), Lit(2)), eff)
			return func() { eff[0] = Set(b, Lit(1)) }
		},
		"On(...).Does": func(r *Registry, a, b Var) func() {
			eff := Do(Set(a, Lit(1)))
			r.On("e").Does(eff).Add()
			return func() { eff[0] = Set(b, Lit(1)) }
		},
		"DeclInvariant repair": func(r *Registry, a, b Var) func() {
			fix := Do(Set(a, Lit(2)))
			r.DeclInvariant("cap", Le(V(a), Lit(2)), fix)
			return func() { fix[0] = Set(b, Lit(2)) }
		},
		"And(slice...) in a guard": func(r *Registry, a, b Var) func() {
			ps := []Pred{Le(V(a), Lit(2)), Le(V(b), Lit(2))}
			r.DeclEventGuarded("e", And(ps...), Do(Set(a, Lit(1))))
			return func() { ps[0] = Eq(V(a), Lit(3)) }
		},
		"Or(slice...) in an invariant": func(r *Registry, a, b Var) func() {
			ps := []Pred{Le(V(a), Lit(2)), Le(V(b), Lit(0))}
			r.DeclInvariant("either", Or(ps...), Do(Set(a, Lit(2))))
			return func() { ps[0] = Eq(V(a), Lit(3)) }
		},
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			r, a, b := newReg()
			mutate := declare(r, a, b)
			before := digestOf(t, r)
			mutate()
			if after := digestOf(t, r); after != before {
				t.Fatalf("rewriting the caller's slice after declaration changed the declared rule "+
					"(policy digest %s -> %s)", before[:12], after[:12])
			}
		})
	}
}

// TestCertifiedLazyMachineUnaffectedByCallerSlice is the consequence: after
// BuildCompositional certified the machine, rewriting the caller's slice made
// the lazy machine's Apply run a different, unverified rule.
func TestCertifiedLazyMachineUnaffectedByCallerSlice(t *testing.T) {
	r := NewRegistry("alias")
	a := r.Int("a", 0, 3)
	b := r.Int("b", 0, 3)
	r.DeclInvariant("cap_a", Le(V(a), Lit(2)), Do(Set(a, Lit(2))))
	r.DeclInvariant("cap_b", Le(V(b), Lit(2)), Do(Set(b, Lit(2))))
	eff := []Assign{Set(a, Lit(1))}
	r.DeclEvent("set_a", Do(eff...))
	r.DeclEvent("inc_b", Do(Set(b, Add(V(b), Lit(1)))))
	m, _, err := r.BuildCompositional()
	if err != nil {
		t.Fatal(err)
	}
	eff[0] = Set(b, Lit(0)) // now writes b, outside the declared write set {a}
	if got := m.Apply(m.NewState().SetInt(b, 2), "set_a"); got.GetInt(b) != 2 || got.GetInt(a) != 1 {
		s0 := m.NewState()
		t.Fatalf("the certified machine's set_a changed after Build: from b=2 it gives %s; "+
			"set_a;inc_b = %s, inc_b;set_a = %s", got,
			m.Apply(m.Apply(s0, "set_a"), "inc_b"), m.Apply(m.Apply(s0, "inc_b"), "set_a"))
	}
}

// TestEnumLabelsUnaffectedByCallerSlice: Enum keeps its own copy of the labels.
func TestEnumLabelsUnaffectedByCallerSlice(t *testing.T) {
	r := NewRegistry("labels")
	values := []string{"red", "green"}
	color := r.Enum("color", values...)
	values[1] = "blue"
	if got := r.vars[0].labels[1]; got != "green" {
		t.Fatalf("the registry's enum label changed with the caller's slice: %q", got)
	}
	if got := color.labels[1]; got != "green" {
		t.Fatalf("the returned Var's enum label changed with the caller's slice: %q", got)
	}
}

// TestCertify_RejectsRegistryChangedDuringCertify: Certify runs morphism closures
// after its components were built (morphism checks, table extraction). A closure
// that declares on a component there would put a declaration nothing checked
// into the certificate's digest.
func TestCertify_RejectsRegistryChangedDuringCertify(t *testing.T) {
	src := NewRegistry("src")
	flag := src.Bool("flag")
	src.DeclEvent("raise", Do(Set(flag, Lit(1))))
	dst := NewRegistry("dst")
	mir := dst.Bool("mirror")
	dst.DeclEvent("noop", Do())
	armed, declared := false, false
	f := NewFederation("fed").Morphism(src, dst).Shared(mir).
		Map(func(s, d State) State {
			if armed && !declared {
				declared = true
				src.DeclEvent("lower", Do(Set(flag, Lit(0)))) // raise and lower do not commute
			}
			return d.setRaw(mir, s.getRaw(flag))
		}).Add()
	armed = true
	cert, err := f.Certify()
	if !declared {
		t.Fatal("premise: the morphism declared an event during Certify")
	}
	if err == nil {
		t.Fatalf("Certify returned a certificate (digest %s) covering src's \"lower\", which no check saw",
			cert.Digest[:12])
	}
	if want := `gsm: registry "src" was changed while it was being verified (a rule declared ` +
		`a variable, invariant, event, or Independent pair)`; err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
}
