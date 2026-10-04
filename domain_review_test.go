package gsm

import (
	"strings"
	"testing"
)

// Findings of the adversarial review of gsm#6 (review-gsm6), kept as regression
// tests. Each asserts the behavior the PR's contract implies.

// R1: Build's Report describes a domain rejection as a WFC failure (bad repair) or
// as WFC PASS with no failure at all (bad effect).
func TestReview_BuildReportNamesDomainRejection(t *testing.T) {
	cases := map[string]*Registry{
		"repair": capped("rev_rep", func(s State, n Var) State { return rawState(s, n, 3) }, effectSetOne),
		"effect": capped("rev_eff", repairToZero, func(s State, n Var) State { return rawState(s, n, 3) }),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			_, rep, err := r.Build()
			if err == nil {
				t.Fatal("Build accepted")
			}
			s := rep.String()
			if strings.Contains(s, "compensation does not terminate") || strings.Contains(s, "WFC: PASS") {
				t.Errorf("report misdescribes a domain rejection:\n%s\nerr: %v", s, err)
			}
		})
	}
}

// R2: BuildCompositional records a domain rejection as a footprint violation.
func TestReview_CompositionalReportNamesDomainRejection(t *testing.T) {
	r := capped("rev_comp", repairToZero, func(s State, n Var) State { return rawState(s, n, 3) })
	_, rep, err := r.BuildCompositional(TrustClosureFootprints())
	if err == nil {
		t.Fatal("BuildCompositional accepted")
	}
	if rep.FootprintViolation != "" {
		t.Errorf("domain rejection reported as a footprint violation:\n%s", rep)
	}
}

// staleSub: src(Bool a) -> dst(n 0..2). The Map is correct on the representative
// target (n=0, the lowest valid state) and returns n=3 on every other target.
// Its certificate is the one a correct Map would have produced: the tables and the
// digest only see the representative target.
func staleSub(t *testing.T, bad bool) (*Federation, *Certificate, *Registry) {
	t.Helper()
	src := NewRegistry("src")
	src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	stale := false
	sub := NewFederation("sub").Morphism(src, dst).Shared(n).Map(func(s, d State) State {
		if stale && d.GetInt(n) != 0 {
			return rawState(d, n, 3)
		}
		return d.SetInt(n, 2)
	}).Add()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatal(err)
	}
	stale = bad // the Map is edited after certification; the certificate is reused
	return sub, cert, dst
}

// R6: EmbedCertified used to trust an internal Map on its certificate, which records the
// Map's images at one representative target only, so a Map edited after certification to
// go wrong only elsewhere built. Build now re-runs the live sub's closures over every valid
// source and target state, so the edited Map is refused at Build, before any image is used.
func TestReview_EmbedCertifiedInternalMapUnchecked(t *testing.T) {
	sub, cert, _ := staleSub(t, true)
	outer := NewFederation("outer").EmbedCertified(sub, cert)
	_, _, err := outer.Build()
	if err == nil || !strings.Contains(err.Error(), "does not match its certificate") ||
		!strings.Contains(err.Error(), "not a state of") || !strings.Contains(err.Error(), "src→dst") {
		t.Fatalf("Build accepted (or misreported) a Map that is wrong away from the representative target: %v", err)
	}
}

// R7: EmbedCertified + an internal Map returning State{} at the representative
// target makes Federation.Build panic (extractTables -> getRaw) instead of erroring.
func TestReview_EmbedCertifiedSchemaLessImagePanics(t *testing.T) {
	src := NewRegistry("src")
	src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	stale := false
	sub := NewFederation("sub").Morphism(src, dst).Shared(n).Map(func(s, d State) State {
		if stale {
			return State{}
		}
		return d.SetInt(n, 2)
	}).Add()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatal(err)
	}
	stale = true
	outer := NewFederation("outer").EmbedCertified(sub, cert)
	if msg := catchPanic(func() { _, _, err = outer.Build() }); msg != "" {
		t.Errorf("Federation.Build panicked instead of returning an error: %s", msg)
	}
}

// R8: Certify on a federation with an embedded sub extracts tables from the
// internal closures without a domain check (only representative target), so it can
// issue a certificate whose recorded value is out of range... here the Map leaks at
// the representative target only after the outer Build.
func TestReview_OuterCertifyUncheckedImages(t *testing.T) {
	src := NewRegistry("src")
	src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	calls := 0
	leakAfter := -1
	sub := NewFederation("sub").Morphism(src, dst).Shared(n).Map(func(s, d State) State {
		calls++
		if leakAfter >= 0 && calls > leakAfter {
			return rawState(d, n, 3)
		}
		return d.SetInt(n, 2)
	}).Add()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatal(err)
	}
	outer := NewFederation("outer").EmbedCertified(sub, cert)
	// Count the closure calls outer.Build makes, then leak on the calls after it.
	calls = 0
	if _, _, err = outer.Build(); err != nil {
		t.Fatal(err)
	}
	built := calls
	calls = 0
	leakAfter = built
	var oc *Certificate
	if msg := catchPanic(func() { oc, err = outer.Certify() }); msg != "" {
		t.Fatalf("Certify panicked: %s", msg)
	}
	if err == nil {
		comps := map[string]*Registry{"src": src, "dst": dst}
		if verr := oc.Verify(comps); verr != nil {
			t.Errorf("Certify issued a certificate that its own Verify rejects: %v", verr)
		}
	}
}

// R9: rejection messages are deterministic across repeated builds.
func TestReview_DeterministicMessages(t *testing.T) {
	mk := func() *Registry {
		r := NewRegistry("rev_det")
		for _, nm := range []string{"a", "b", "c", "d"} {
			v := r.Int(nm, 0, 2)
			r.Event("bad_" + nm).Writes(v).Apply(func(s State) State { return rawState(s, v, 3) }).Add()
		}
		return r
	}
	var first, firstC string
	for i := 0; i < 50; i++ {
		_, _, err := mk().Build()
		_, _, errC := mk().BuildCompositional(TrustClosureFootprints())
		if i == 0 {
			first, firstC = err.Error(), errC.Error()
			continue
		}
		if err.Error() != first || errC.Error() != firstC {
			t.Fatalf("nondeterministic: %q vs %q / %q vs %q", err, first, errC, firstC)
		}
	}
}

// R10: no false rejects for legitimate states: enum past power-of-two, negative Int
// min, zero-width variables, a structurally identical machine's state from a
// separately declared registry, used by repair and effect on Build and lazy paths.
func TestReview_NoFalseRejects(t *testing.T) {
	decl := func(r *Registry) (Var, Var, Var) {
		e := r.Enum("e", "x", "y", "z", "w", "v")
		i := r.Int("i", -3, 3)
		b := r.Bool("b")
		return e, i, b
	}
	twinR := NewRegistry("twin")
	te, ti, tb := decl(twinR)
	twin, _, err := twinR.Build()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry("rev_ok")
	e, i, b := decl(r)
	r.Invariant("i_not_3").Watches(i).Holds(func(s State) bool { return s.GetInt(i) != 3 }).
		Repair(func(s State) State { return twin.NewState().SetInt(ti, -2).Set(te, s.Get(e)).SetBool(tb, s.GetBool(b)) }).Add()
	r.Event("go").Writes(e, i, b).Apply(func(s State) State {
		return twin.NewState().Set(te, "z").SetInt(ti, 3).SetBool(tb, true)
	}).Add()
	if _, _, err = r.Build(); err != nil {
		t.Errorf("Build false reject: %v", err)
	}
	m, _, err := r.BuildCompositional(TrustClosureFootprints())
	if err != nil {
		t.Fatalf("BuildCompositional false reject: %v", err)
	}
	if msg := catchPanic(func() { m.Apply(m.NewState(), "go") }); msg != "" {
		t.Errorf("lazy Apply false reject: %s", msg)
	}
}

// R14: Federation.Build runs Map closures after the components are built; a Map that
// declares on its target registry is not caught by the mid-run guard or the domain check.
func TestReview_FederationMapDeclaresMidRun(t *testing.T) {
	src := NewRegistry("src")
	src.Bool("a")
	dst := NewRegistry("dst")
	n := dst.Int("n", 0, 2)
	declared := false
	f := NewFederation("mid_fed").Morphism(src, dst).Shared(n).Map(func(s, d State) State {
		if !declared {
			declared = true
			dst.Bool("late")
		}
		return d.SetInt(n, 2)
	}).Add()
	var err error
	if msg := catchPanic(func() { _, _, err = f.Build() }); msg != "" {
		t.Fatalf("Federation.Build panicked instead of reporting the change: %s", msg)
	}
	if err == nil {
		t.Errorf("Federation.Build accepted a Map that declared a variable on its target")
	}
}

// R3: lazy Apply with an out-of-domain input and a guard that does not fire returned
// the input unchanged, outside the machine. The input is now checked at entry.
func TestReview_LazyApplyGuardFalseOutOfDomainInput(t *testing.T) {
	r := NewRegistry("rev_guard")
	n := r.Int("n", 0, 2)
	r.Event("noop").Writes(n).Guard(func(s State) bool { return false }).
		Apply(func(s State) State { return s.SetInt(n, 0) }).Add()
	m, _, err := r.BuildCompositional(TrustClosureFootprints())
	if err != nil {
		t.Fatal(err)
	}
	in := rawState(m.NewState(), n, 3)
	for name, call := range map[string]func(){
		"Apply":     func() { m.Apply(in, "noop") },
		"Normalize": func() { m.Normalize(in) },
	} {
		msg := catchPanic(call)
		if !strings.Contains(msg, "input {n=3} is not a state of machine") || !strings.Contains(msg, name) {
			t.Errorf("%s did not reject the out-of-domain input: %q", name, msg)
		}
	}
}

// R4: lazy Apply on a foreign input blamed a correct effect. The input is now
// rejected before any rule runs.
func TestReview_LazyApplyForeignInputBlamesEffect(t *testing.T) {
	r := NewRegistry("rev_blame")
	n := r.Int("n", 0, 2)
	r.Event("set2").Writes(n).Apply(func(s State) State { return s.SetInt(n, 2) }).Add()
	m, _, err := r.BuildCompositional(TrustClosureFootprints())
	if err != nil {
		t.Fatal(err)
	}
	w := foreignMachine(t, "wider", func(r *Registry) { r.Int("n", 0, 3) })
	msg := catchPanic(func() { m.Apply(w.NewState(), "set2") })
	if strings.Contains(msg, `event "set2" effect`) || !strings.Contains(msg, "is not a state of machine") {
		t.Errorf("want the foreign input rejected, not the effect blamed: %q", msg)
	}
}

// R5: MergeProjection wrote values outside the domain and truncated values wider
// than the field without error; it now refuses them, and a state of another layout.
func TestReview_MergeProjectionOutOfDomain(t *testing.T) {
	r := NewRegistry("rev_merge")
	r.Int("n", 0, 2)
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []uint64{3, 4, 1 << 40} {
		out, merr := m.MergeProjection(m.NewState(), Projection{Shared: map[string]uint64{"n": raw}})
		if merr == nil {
			t.Errorf("raw %d: MergeProjection returned %s with no error", raw, out)
		} else if !strings.Contains(merr.Error(), "outside 0..2") {
			t.Errorf("raw %d: the error does not give the domain: %v", raw, merr)
		}
	}
	if _, err = m.MergeProjection(m.NewState(), Projection{Shared: map[string]uint64{"n": 2}}); err != nil {
		t.Errorf("an in-domain value was refused: %v", err)
	}
	// Two bad values: the error names the same one (the first by name) on every call.
	r2 := NewRegistry("rev_merge2")
	r2.Int("a", 0, 2)
	r2.Int("b", 0, 2)
	m2, _, err := r2.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		_, err := m2.MergeProjection(m2.NewState(), Projection{Shared: map[string]uint64{"b": 3, "a": 3}})
		if err == nil || !strings.Contains(err.Error(), `for "a"`) {
			t.Fatalf("want the error for \"a\" on every call, got: %v", err)
		}
	}
	w := foreignMachine(t, "wider", func(r *Registry) { r.Int("n", 0, 3) })
	if _, err := m.MergeProjection(w.NewState(), Projection{Shared: map[string]uint64{"n": 1}}); err == nil ||
		!strings.Contains(err.Error(), "is not a state of machine") {
		t.Errorf("a state of another layout was merged into: %v", err)
	}
}

// R12: BuildOrSynthesize swallowed Build's out-of-domain repair error and returned a
// machine with a synthesized compensation. It falls back only when the compensation
// as written does not converge (or is missing), and returns other errors as they are.
func TestReview_BuildOrSynthesizeSwallowsDomainError(t *testing.T) {
	r := capped("rev_bos", func(s State, n Var) State { return rawState(s, n, 3) }, effectSetOne)
	m, syn, err := r.BuildOrSynthesize()
	if err == nil || m != nil || syn != nil || !strings.Contains(err.Error(), "not a state of machine") {
		t.Errorf("want Build's out-of-domain error returned as is; got machine=%v synthesis=%v err=%v", m != nil, syn != nil, err)
	}
}

// R13: BuildOrSynthesize after a mid-run declaration synthesized on the changed
// registry and returned a machine.
func TestReview_BuildOrSynthesizeSwallowsMidRunChange(t *testing.T) {
	r := NewRegistry("mid")
	x := r.Int("x", 0, 2)
	declared := false
	r.Event("a").Writes(x).Apply(func(s State) State {
		if !declared {
			declared = true
			r.Bool("late")
		}
		return s
	}).Add()
	m, _, err := r.BuildOrSynthesize()
	if m != nil {
		t.Fatalf("BuildOrSynthesize returned a machine (%d vars) after a rule changed the registry mid-Build", len(m.vars))
	}
	wantModified(t, err)
}
