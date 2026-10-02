package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// dupEventRegistry declares two events named "a". Independent("a", "b") resolves
// "a" to the first declaration (which commutes with b), but a built machine's
// Apply resolves "a" to the last (which does not), so the pair CC checks and the
// pair the runtime applies are different events.
func dupEventRegistry() *gsm.Registry {
	r := gsm.NewRegistry("dup")
	x := r.Int("x", 0, 3)
	y := r.Bool("y")
	r.On("a").Does(gsm.SetTo(y, 1)).Add()                                 // first "a": commutes with b
	r.On("b").Does(gsm.Inc(x)).Add()                                      // b: x += 1
	r.On("a").Does(gsm.Do(gsm.Set(x, gsm.Add(gsm.V(x), gsm.V(x))))).Add() // last "a": x *= 2
	r.Independent("a", "b")
	return r
}

// TestDuplicateEventName_CheckAndRuntimeDisagree is the bug: with two events named
// "a", the CC check covers the first and Apply runs the last, so a machine that
// diverges by event order was certified convergent. Build must reject it.
func TestDuplicateEventName_CheckAndRuntimeDisagree(t *testing.T) {
	m, _, err := dupEventRegistry().Build()
	if err == nil {
		s := m.NewState()
		ab := m.Apply(m.Apply(s, "a"), "b")
		ba := m.Apply(m.Apply(s, "b"), "a")
		t.Fatalf("Build accepted a registry with two events named %q; the declared pair (a, b) "+
			"was checked for the first \"a\" but Apply runs the last, and the order matters: "+
			"a;b = %s, b;a = %s", "a", ab, ba)
	}
	if !strings.Contains(err.Error(), `registry "dup": duplicate event name "a"`) {
		t.Fatalf("Build error does not name the duplicate: %v", err)
	}
}

// wantDupErr fails unless err is exactly the duplicate-name rejection for event
// "a" of registry reg: not wrapped as a convergence or synthesis failure.
func wantDupErr(t *testing.T, err error, reg string) {
	t.Helper()
	want := `gsm: registry "` + reg + `": duplicate event name "a"`
	if err == nil {
		t.Fatalf("accepted a registry with two events named \"a\"; want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err, want)
	}
}

// TestDuplicateEventName_ClosureEvents: events declared with Event(...).Apply
// are named the same way as combinator events.
func TestDuplicateEventName_ClosureEvents(t *testing.T) {
	r := gsm.NewRegistry("closures")
	x := r.Bool("x")
	r.Event("a").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetBool(x, true) }).Add()
	r.Event("a").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetBool(x, false) }).Add()
	_, _, err := r.Build()
	wantDupErr(t, err, "closures")
}

func TestDuplicateEventName_AllPairsMode(t *testing.T) {
	r := gsm.NewRegistry("allpairs")
	x := r.Bool("x")
	r.On("a").Does(gsm.Raise(x)).Add()
	r.On("a").Does(gsm.Lower(x)).Add()
	_, _, err := r.Build()
	wantDupErr(t, err, "allpairs")
	var b strings.Builder
	wantDupErr(t, r.WriteDeclaredPairs(&b), "allpairs")
	if b.Len() != 0 {
		t.Fatalf("WriteDeclaredPairs wrote %q before rejecting", b.String())
	}
}

func TestDuplicateEventName_BuildCompositional(t *testing.T) {
	_, _, err := dupEventRegistry().BuildCompositional()
	wantDupErr(t, err, "dup")
}

func TestDuplicateEventName_Synthesize(t *testing.T) {
	_, err := dupEventRegistry().Synthesize()
	wantDupErr(t, err, "dup")
	_, err = dupEventRegistry().SynthesizeWith(gsm.Optimal())
	wantDupErr(t, err, "dup")
}

// TestDuplicateEventName_BuildOrSynthesize: the rejection is reported as itself,
// not as "build failed and synthesis could not run".
func TestDuplicateEventName_BuildOrSynthesize(t *testing.T) {
	m, s, err := dupEventRegistry().BuildOrSynthesize()
	if m != nil || s != nil {
		t.Fatalf("got machine %v, synthesis %v; want neither", m, s)
	}
	wantDupErr(t, err, "dup")
}

// TestDuplicateEventName_RulesExport: the rules export, its policy digest, and
// the declared-pairs file for the checkers all refuse, rather than emit events by
// position that the runtime cannot address by name.
func TestDuplicateEventName_RulesExport(t *testing.T) {
	r := dupEventRegistry()
	var b strings.Builder
	wantDupErr(t, r.WriteMachineAST(&b), "dup")
	if b.Len() != 0 {
		t.Fatalf("WriteMachineAST wrote %q before rejecting", b.String())
	}
	_, err := r.PolicyBytes()
	wantDupErr(t, err, "dup")
	_, err = r.PolicyDigest()
	wantDupErr(t, err, "dup")
	b.Reset()
	wantDupErr(t, r.WriteDeclaredPairs(&b), "dup")
	if b.Len() != 0 {
		t.Fatalf("WriteDeclaredPairs wrote %q before rejecting", b.String())
	}
}

// twoRegistryFed is a source "src" whose flag a morphism mirrors into "dst".
func twoRegistryFed() (f *gsm.Federation, src, dst *gsm.Registry) {
	src = gsm.NewRegistry("src")
	flag := src.Bool("flag")
	src.On("a").Does(gsm.Raise(flag)).Add()
	dst = gsm.NewRegistry("dst")
	mirror := dst.Bool("mirror")
	dst.On("a").Does(gsm.Raise(mirror)).Add()
	f = gsm.NewFederation("fed").
		Morphism(src, dst).Shared(mirror).
		Map(func(s, d gsm.State) gsm.State { return d.SetBool(mirror, s.GetBool(flag)) }).Add()
	return f, src, dst
}

// addDupA declares a second event named "a" on r (a no-op).
func addDupA(r *gsm.Registry) { r.On("a").Does(gsm.Do()).Add() }

// TestDuplicateEventName_Federation: a component with a duplicate event name is
// rejected by name, not reported as a component that "does not converge".
func TestDuplicateEventName_Federation(t *testing.T) {
	f, _, dst := twoRegistryFed()
	addDupA(dst)
	_, _, err := f.Build()
	wantDupErr(t, err, "dst")
	_, err = f.Certify()
	wantDupErr(t, err, "dst")
	_, _, err = f.BuildCoordinated(nil)
	wantDupErr(t, err, "dst")
}

// TestDuplicateEventName_CertificateAfterTheFact: a certificate issued while the
// names were unique is refused by Verify and by an EmbedCertified build once a
// component gains a duplicate event name.
func TestDuplicateEventName_CertificateAfterTheFact(t *testing.T) {
	sub, src, dst := twoRegistryFed()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.Verify(map[string]*gsm.Registry{"src": src, "dst": dst}); err != nil {
		t.Fatalf("premise: the certificate verifies before the duplicate: %v", err)
	}
	addDupA(src)
	wantDupErr(t, cert.Verify(map[string]*gsm.Registry{"src": src, "dst": dst}), "src")
	outer := gsm.NewFederation("outer").EmbedCertified(sub, cert)
	_, _, err = outer.Build()
	wantDupErr(t, err, "src")
}
