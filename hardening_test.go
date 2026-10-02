package gsm

import (
	"strings"
	"testing"
)

// Hardening: lookups, plans, ports and digests that accepted inputs naming
// something that is not there, or that two different policies shared.

// hardPair: src (Bool a) -> dst (Int n 0..2, shared; Bool f, free), a convergent
// federation of combinator registries (so it can be certified).
func hardPair() (*Federation, *Registry, *Registry) {
	src := NewRegistry("src")
	a := src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	dst.Bool("f")
	src.DeclEvent("raise", Do(Set(a, Lit(1))))
	f := NewFederation("pair").Morphism(src, dst).Shared(n).
		Map(func(s, d State) State {
			if s.GetBool(a) {
				return d.SetInt(n, 2)
			}
			return d.SetInt(n, 0)
		}).Add()
	return f, src, dst
}

// 1: FedMachine.Of and Apply given a registry that is not a component used to act on
// component 0 (the map lookup's zero value).
func TestFedMachine_UnknownRegistryRejected(t *testing.T) {
	f, _, _ := hardPair()
	fm, _, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	stranger := NewRegistry("stranger")
	stranger.Bool("a")
	stranger.Event("raise").Apply(func(s State) State { return s }).Add()
	fs := fm.NewState()
	for name, call := range map[string]func(){
		"Of":    func() { fm.Of(fs, stranger) },
		"Apply": func() { fm.Apply(fs, stranger, "raise") },
	} {
		msg := catchPanic(call)
		if !strings.Contains(msg, `registry "stranger" is not part of federation "pair"`) {
			t.Errorf("%s with a registry outside the federation: want a panic naming it, got %q", name, msg)
		}
	}
}

// 2: BuildCoordinated ignored a coordination point that names no morphism and built
// the federation unchanged.
func TestBuildCoordinated_UnknownPointRejected(t *testing.T) {
	f, _, _ := hardPair()
	for _, cp := range []CoordinationPoint{{Src: "dst", Dst: "src"}, {Src: "nowhere", Dst: "dst"}} {
		_, _, err := f.BuildCoordinated([]CoordinationPoint{cp})
		if err == nil || !strings.Contains(err.Error(), "names no morphism") {
			t.Errorf("BuildCoordinated(%v): want a rejection of the unknown point, got %v", cp, err)
		}
	}
	if _, _, err := f.BuildCoordinated([]CoordinationPoint{{Src: "src", Dst: "dst"}}); err != nil {
		t.Errorf("a point naming the morphism was rejected: %v", err)
	}
}

// 3: an enum with a repeated label, like a repeated variable or event name, makes
// name-addressed reads and writes ambiguous (Set resolves to the first index).
func TestEnum_DuplicateLabelRejected(t *testing.T) {
	r := NewRegistry("dup_label")
	r.Enum("s", "a", "b", "a")
	want := `gsm: registry "dup_label": enum "s" has duplicate label "a"`
	if _, _, err := r.Build(); err == nil || err.Error() != want {
		t.Errorf("Build: want %q, got %v", want, err)
	}
	if _, err := r.PolicyDigest(); err == nil || err.Error() != want {
		t.Errorf("PolicyDigest: want %q, got %v", want, err)
	}
}

// 4: Certify's input ports were checked by variable index only, so a Var from
// another registry (or an index past the end) passed as a port of this one.
func TestCertify_PortVariableCheckedBySchema(t *testing.T) {
	f, _, dst := hardPair()
	if _, err := f.Certify(Port{Registry: dst, Var: dst.vars[1]}); err != nil {
		t.Fatalf("dst's own free variable is not accepted as a port: %v", err)
	}
	other := NewRegistry("other")
	other.Int("n", 0, 2)
	of := other.Int("f", 0, 1) // index 1 like dst's f, but an Int, not a Bool
	other.Bool("g")
	og := other.vars[2] // index 2: dst has no variable 2
	for name, v := range map[string]Var{"another schema at the same index": of, "an index dst does not have": og} {
		if _, err := f.Certify(Port{Registry: dst, Var: v}); err == nil || !strings.Contains(err.Error(), "not a variable of") {
			t.Errorf("a port with %s was accepted: %v", name, err)
		}
	}
}

// 5: the policy digest covered the rules but not the names they are addressed by, so
// two registries that differ only in an event name, a variable name, an enum label,
// or the declared Independent pairs digested the same (and so did certificates over
// them), although replay, projections, ports and CC address them.
func TestPolicyDigest_BindsNames(t *testing.T) {
	mk := func(evName, varName, label string, independent bool) *Registry {
		r := NewRegistry("named")
		v := r.Int(varName, 0, 2)
		r.Enum("e", "x", label)
		r.DeclEvent(evName, Do(Set(v, Lit(1))))
		r.DeclEvent("other", Do(Set(v, Lit(2))))
		if independent {
			r.Independent(evName, "other")
		}
		return r
	}
	base, err := mk("go", "n", "y", false).PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]*Registry{
		"event name":        mk("went", "n", "y", false),
		"variable name":     mk("go", "m", "y", false),
		"enum label":        mk("go", "n", "z", false),
		"independent pairs": mk("go", "n", "y", true),
	} {
		d, derr := r.PolicyDigest()
		if derr != nil {
			t.Fatal(derr)
		}
		if d == base {
			t.Errorf("registries differing only in %s have the same policy digest", name)
		}
	}
	again, err := mk("go", "n", "y", false).PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	if again != base {
		t.Error("the same registry digests differently")
	}

}

// TestCertificateDigest_BindsEventNames: renaming an event of a certified component
// changes the certificate digest, so Verify refuses the renamed component.
func TestCertificateDigest_BindsEventNames(t *testing.T) {
	mk := func(evName string) (*Federation, *Registry, *Registry) {
		src := NewRegistry("src")
		a := src.Bool("a")
		src.DeclEvent(evName, Do(Set(a, Lit(1))))
		dst := NewRegistry("dst")
		n := dst.Int("n", 0, 2)
		f := NewFederation("named").Morphism(src, dst).Shared(n).
			Map(func(s, d State) State {
				if s.GetBool(a) {
					return d.SetInt(n, 2)
				}
				return d.SetInt(n, 0)
			}).Add()
		return f, src, dst
	}
	f, _, _ := mk("raise")
	cert, err := f.Certify()
	if err != nil {
		t.Fatal(err)
	}
	_, src2, dst2 := mk("lift")
	err = cert.Verify(map[string]*Registry{"src": src2, "dst": dst2})
	if err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Errorf("Verify accepted a component whose event was renamed: %v", err)
	}
}

// 6: DiagnoseCycle runs Map closures; one that declares on a cycle component made it
// return a diagnostic about a registry other than the one analyzed, with no error.
func TestDiagnoseCycle_ComponentChangedRejected(t *testing.T) {
	a := NewRegistry("A")
	av := a.Int("v", 0, 2)
	b := NewRegistry("B")
	bv := b.Int("v", 0, 2)
	declared := false
	f := NewFederation("diag_mid").
		Morphism(a, b).Shared(bv).Map(func(src, d State) State {
		if !declared {
			declared = true
			b.Bool("late")
		}
		return d.SetInt(bv, src.GetInt(av))
	}).Add().
		Morphism(b, a).Shared(av).Map(func(src, d State) State { return d.SetInt(av, src.GetInt(bv)) }).Add()
	var err error
	var diag *CycleDiagnostic
	if msg := catchPanic(func() { diag, err = f.DiagnoseCycle() }); msg != "" {
		t.Fatalf("DiagnoseCycle panicked: %s", msg)
	}
	if err == nil || !strings.Contains(err.Error(), `registry "B" was changed while it was being verified`) {
		t.Errorf("want the change reported, got diagnostic %v, err %v", diag, err)
	}
}
