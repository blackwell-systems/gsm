package gsm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	if _, _, err := f.BuildCoordinated([]CoordinationPoint{{Src: "src", Dst: "dst", Shared: []string{"n"}}}); err != nil {
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

// 5: the policy digest covers the rules but not the names they are addressed by, so
// two registries that differ only in an event name, a variable name, an enum label,
// or the declared Independent pairs digest the same, and so did certificates over
// them, although replay, projections, ports and CC address them. PolicyDigest stays
// exactly over PolicyBytes (the published contract); PolicyIdentityDigest and the
// certificate digest bind the names.
func TestPolicyIdentityDigest_BindsNames(t *testing.T) {
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
	base, err := mk("go", "n", "y", false).PolicyIdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]*Registry{
		"event name":        mk("went", "n", "y", false),
		"variable name":     mk("go", "m", "y", false),
		"enum label":        mk("go", "n", "z", false),
		"independent pairs": mk("go", "n", "y", true),
	} {
		d, derr := r.PolicyIdentityDigest()
		if derr != nil {
			t.Fatal(derr)
		}
		if d == base {
			t.Errorf("registries differing only in %s have the same identity digest", name)
		}
	}
	// A Bool and an Int over 0..1 serialize alike in the oracle's format.
	kind := func(asBool bool) *Registry {
		r := NewRegistry("kind")
		var v Var
		if asBool {
			v = r.Bool("v")
		} else {
			v = r.Int("v", 0, 1)
		}
		r.DeclEvent("set", Do(Set(v, Lit(1))))
		return r
	}
	db, err := kind(true).PolicyIdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	di, err := kind(false).PolicyIdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	if db == di {
		t.Error("registries differing only in a variable's kind have the same identity digest")
	}
	again, err := mk("go", "n", "y", false).PolicyIdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	if again != base {
		t.Error("the same registry digests differently")
	}

	// PolicyDigest does not cover names (documented): the renamed registry has the
	// same PolicyDigest.
	p1, err := mk("go", "n", "y", false).PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := mk("went", "m", "z", true).PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Error("PolicyDigest changed with names only; it is documented to cover PolicyBytes alone")
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

// Review of #11 (G1-G5 and a DiagnoseCycle gap).

// G1: a morphism's Shared() variables were never checked against the target, so a
// Var of another registry at the same index made Federation.Build panic, or a wider
// one with the same name was taken for the target's.
func TestShared_VariableCheckedBySchema(t *testing.T) {
	cases := map[string]func() Var{
		"same name, wider range": func() Var { o := NewRegistry("other"); return o.Int("n", 0, 7) },
		"other name, same index": func() Var { o := NewRegistry("other"); return o.Int("m", 0, 2) },
		"index past the end":     func() Var { o := NewRegistry("other"); o.Bool("x"); o.Bool("y"); return o.Bool("z") },
	}
	for name, foreign := range cases {
		t.Run(name, func(t *testing.T) {
			src := NewRegistry("src")
			a := src.Bool("a")
			dst := NewRegistry("dst")
			n := dst.Int("n", 0, 2)
			dst.Bool("f")
			fv := foreign()
			mk := func() *Federation {
				return NewFederation("shared_foreign").Morphism(src, dst).Shared(fv).
					Map(func(s, d State) State {
						if s.GetBool(a) {
							return d.SetInt(n, 2)
						}
						return d.SetInt(n, 0)
					}).Add()
			}
			want := `morphism src→dst: Shared() variable "` + fv.name + `" is not a variable of registry "dst"`
			var err error
			if msg := catchPanic(func() { _, _, err = mk().Build() }); msg != "" {
				t.Fatalf("Federation.Build panicked on a foreign Shared() variable: %s", msg)
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Build: want %q, got %v", want, err)
			}
			if msg := catchPanic(func() { _, err = mk().DiagnoseCycle() }); msg != "" {
				t.Fatalf("DiagnoseCycle panicked: %s", msg)
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("DiagnoseCycle: want %q, got %v", want, err)
			}
		})
	}
}

// G2: a coordination point's Shared names what is coordinated; one that does not match
// the morphism's shared variables removed the morphism anyway.
func TestBuildCoordinated_PointSharedChecked(t *testing.T) {
	f, _, _ := hardPair()
	for _, shared := range [][]string{{"f"}, nil, {"n", "f"}} {
		_, _, err := f.BuildCoordinated([]CoordinationPoint{{Src: "src", Dst: "dst", Shared: shared}})
		if err == nil || !strings.Contains(err.Error(), "shares [n]") {
			t.Errorf("Shared %v: want a mismatch error naming the morphism's [n], got %v", shared, err)
		}
	}
}

// G3, G4: the certificate digest framed names without quoting, so different port and
// table declarations could serialize alike.
func TestCertificateDigest_FramingUnambiguous(t *testing.T) {
	d := func(tables []MorphismTable, ports []PortRef) string {
		s, err := digestComponentsAndTables(nil, tables, false, ports)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if d(nil, []PortRef{{Registry: "a.b", Var: "c"}}) == d(nil, []PortRef{{Registry: "a", Var: "b.c"}}) {
		t.Error("two different port declarations share a certificate digest")
	}
	row := []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{1}}}
	tab := func(target string, sources, shared []string) []MorphismTable {
		return []MorphismTable{{Target: target, Sources: sources, Shared: shared, Rows: row}}
	}
	if d(tab("t", []string{"a,b"}, []string{"x"}), nil) == d(tab("t", []string{"a", "b"}, []string{"x"}), nil) {
		t.Error("tables with different source lists share a certificate digest")
	}
	if d(tab("t", []string{"a"}, []string{"x,y"}), nil) == d(tab("t", []string{"a"}, []string{"x", "y"}), nil) {
		t.Error("tables with different shared lists share a certificate digest")
	}
	if d(tab("t <- [a]", []string{"b"}, []string{"x"}), nil) == d(tab("t", []string{"a] <- [b"}, []string{"x"}), nil) {
		t.Error("tables with different targets and sources share a certificate digest")
	}
	// The component line: with the name unquoted, one component whose name embeds the
	// framing of a second digested exactly like the two components.
	empty := func(name string) []byte {
		r := NewRegistry(name)
		b, err := r.PolicyBytes()
		if err != nil {
			t.Fatal(err)
		}
		names, err := r.PolicyNames()
		if err != nil {
			t.Fatal(err)
		}
		return append(b, names...)
	}
	e := string(empty("x"))
	one, err := digestComponentsAndTables([]*Registry{NewRegistry("x\n" + e + "\ncomp y")}, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := digestComponentsAndTables([]*Registry{NewRegistry("x"), NewRegistry("y")}, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Error("one component and two components share a certificate digest")
	}
}

// G5: the same set of Independent pairs, declared in another order or twice, is the
// same policy, so it digests alike.
func TestPolicyNames_PairsCanonical(t *testing.T) {
	mk := func(decl func(r *Registry)) string {
		r := NewRegistry("pairs")
		v := r.Int("v", 0, 2)
		for _, e := range []string{"a", "b", "c"} {
			r.DeclEvent(e, Do(Set(v, Lit(1))))
		}
		decl(r)
		d, err := r.PolicyIdentityDigest()
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := mk(func(r *Registry) { r.Independent("a", "b").Independent("b", "c") })
	for name, decl := range map[string]func(r *Registry){
		"other order":     func(r *Registry) { r.Independent("b", "c").Independent("a", "b") },
		"other direction": func(r *Registry) { r.Independent("b", "a").Independent("c", "b") },
		"repeated":        func(r *Registry) { r.Independent("a", "b").Independent("b", "c").Independent("a", "b") },
	} {
		if mk(decl) != base {
			t.Errorf("the same pair set declared with %s digests differently", name)
		}
	}
	if mk(func(r *Registry) { r.Independent("a", "c") }) == base {
		t.Error("a different pair set digests the same")
	}
}

// DiagnoseCycle works on a frozen copy: a closure that adds a component to f while it
// runs does not reach the analysis or the change check (which compares the components
// it analyzed).
func TestDiagnoseCycle_FrozenWiring(t *testing.T) {
	a := NewRegistry("A")
	av := a.Int("v", 0, 2)
	b := NewRegistry("B")
	bv := b.Int("v", 0, 2)
	var f *Federation
	added := false
	f = NewFederation("diag_frozen").
		Morphism(a, b).Shared(bv).Map(func(src, d State) State {
		if !added {
			added = true
			f.Add(NewRegistry("late_component"))
		}
		return d.SetInt(bv, src.GetInt(av))
	}).Add().
		Morphism(b, a).Shared(av).Map(func(src, d State) State { return d.SetInt(av, src.GetInt(bv)) }).Add()
	var diag *CycleDiagnostic
	var err error
	if msg := catchPanic(func() { diag, err = f.DiagnoseCycle() }); msg != "" {
		t.Fatalf("DiagnoseCycle panicked: %s", msg)
	}
	if err != nil || diag == nil || !diag.Converges || len(diag.Cycle) != 2 {
		t.Fatalf("want the converging A-B diagnostic of the federation as called, got %v, %v", diag, err)
	}
}

// Review of #11, round 2.

// H3: identity digest canonicalizes pair order, direction and repeats, and still
// separates different pair sets, "all" from an explicit list, and no pairs.
func TestReview11v2_IdentityPairs(t *testing.T) {
	mk := func(decl func(r *Registry)) string {
		r := NewRegistry("pairs")
		v := r.Int("v", 0, 2)
		for _, e := range []string{"a", "b", "c"} {
			r.DeclEvent(e, Do(Set(v, Lit(1))))
		}
		decl(r)
		d, err := r.PolicyIdentityDigest()
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := mk(func(r *Registry) { r.Independent("a", "b").Independent("b", "c") })
	for name, d := range map[string]string{
		"order":     mk(func(r *Registry) { r.Independent("b", "c").Independent("a", "b") }),
		"direction": mk(func(r *Registry) { r.Independent("b", "a").Independent("c", "b") }),
		"repeat":    mk(func(r *Registry) { r.Independent("a", "b").Independent("b", "c").Independent("a", "b") }),
	} {
		if d != base {
			t.Errorf("%s changes the identity digest", name)
		}
	}
	for name, d := range map[string]string{
		"other set": mk(func(r *Registry) { r.Independent("a", "c").Independent("b", "c") }),
		"subset":    mk(func(r *Registry) { r.Independent("a", "b") }),
		"all":       mk(func(r *Registry) {}),
		"none":      mk(func(r *Registry) { r.OnlyDeclaredPairs() }),
		"self":      mk(func(r *Registry) { r.Independent("a", "b").Independent("b", "c").Independent("a", "a") }),
	} {
		if d == base {
			t.Errorf("%s does not change the identity digest", name)
		}
	}
}

// TestPolicyIdentityDigest_Recomputable: an independent verifier recomputes the
// identity digest as SHA-256(PolicyIdentityVersion "\n" PolicyBytes PolicyNames).
func TestPolicyIdentityDigest_Recomputable(t *testing.T) {
	r := NewRegistry("ident")
	v := r.Int("v", 0, 2)
	r.Enum("e", "x", "y")
	r.DeclEvent("a", Do(Set(v, Lit(1))))
	r.DeclEvent("b", Do(Set(v, Lit(2))))
	r.Independent("a", "b")
	b, err := r.PolicyBytes()
	if err != nil {
		t.Fatal(err)
	}
	names, err := r.PolicyNames()
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	h.Write([]byte("gsm-policy-identity-v1\n"))
	h.Write(b)
	h.Write(names)
	d, err := r.PolicyIdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != d {
		t.Errorf("recomputed %s, PolicyIdentityDigest is %s", got, d)
	}
}

// coordinationFeds are the federations of the coordination tests and example, plus
// parallel morphisms into one target (each its own coordination point) and a
// three-registry ring.
func coordinationFeds() map[string]*Federation {
	feds := map[string]*Federation{}
	{
		a := NewRegistry("A")
		fa := a.Int("fa", 0, 1)
		b := NewRegistry("B")
		fb := b.Int("fb", 0, 1)
		feds["negation loop"] = NewFederation("loop").
			Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
			Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
	}
	{
		a := NewRegistry("A")
		fa := a.Int("fa", 0, 1)
		b := NewRegistry("B")
		fb := b.Int("fb", 0, 1)
		feds["chain"] = NewFederation("chain").
			Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add()
	}
	{
		p := NewRegistry("primary")
		pv := p.Int("v", 0, 1)
		m := NewRegistry("mirror")
		mv := m.Int("v", 0, 1)
		feds["mirrors"] = NewFederation("mirrors").
			Morphism(p, m).Shared(mv).Map(func(s, d State) State { return d.SetInt(mv, s.GetInt(pv)) }).Add().
			Morphism(m, p).Shared(pv).Map(func(s, d State) State { return d.SetInt(pv, 1-s.GetInt(mv)) }).Add()
	}
	{
		a := NewRegistry("A")
		a1 := a.Bool("a1")
		a2 := a.Bool("a2")
		b := NewRegistry("B")
		bx := b.Bool("bx")
		c := NewRegistry("C")
		c.Bool("c")
		keep := func(s, d State) State { return d }
		feds["parallel morphisms"] = NewFederation("par").
			Morphism(c, a).Shared(a1, a2).Map(keep).Add().
			Morphism(a, b).Shared(bx).Map(func(s, d State) State { return d.SetBool(bx, !s.GetBool(a1)) }).Add().
			Morphism(b, a).Shared(a1).Map(keep).Add().
			Morphism(b, a).Shared(a2).Map(keep).Add().
			Resolve(a, func(d State, src map[string]State) State { return d.SetBool(a1, false).SetBool(a2, false) })
	}
	{
		regs := make([]*Registry, 3)
		vs := make([]Var, 3)
		for i := range regs {
			regs[i] = NewRegistry(fmt.Sprintf("R%d", i))
			vs[i] = regs[i].Int("v", 0, 1)
		}
		f := NewFederation("ring")
		for i := range regs {
			src, dst := regs[i], regs[(i+1)%3]
			sv, dv := vs[i], vs[(i+1)%3]
			f.Morphism(src, dst).Shared(dv).Map(func(s, d State) State { return d.SetInt(dv, 1-s.GetInt(sv)) }).Add()
		}
		feds["negation ring"] = f
	}
	return feds
}

// TestBuildCoordinated_AcceptsOwnPlan: BuildCoordinated accepts the plan
// CoordinationPlan returns, for every federation above (a regression: with parallel
// morphisms between two registries, a point was matched against the wrong one).
func TestBuildCoordinated_AcceptsOwnPlan(t *testing.T) {
	for name, f := range coordinationFeds() {
		plan := f.CoordinationPlan()
		if _, _, err := f.BuildCoordinated(plan); err != nil {
			t.Errorf("%s: BuildCoordinated(%v): %v", name, plan, err)
		}
	}
}
