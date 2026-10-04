package gsm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// mustPanic runs f and returns the panic message, failing the test if f does not panic.
func mustPanic(t *testing.T, f func()) string {
	t.Helper()
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg, _ = r.(string)
				if msg == "" {
					msg = "non-string panic"
				}
			}
		}()
		f()
	}()
	if msg == "" {
		t.Fatal("expected a panic")
	}
	return msg
}

func noopResolver(d State, _ map[string]State) State { return d }

// TestResolve_SecondResolverForSameTargetPanics: a second Resolve for one target used to replace
// the first silently; it now panics, naming the target.
func TestResolve_SecondResolverForSameTargetPanics(t *testing.T) {
	tgt := NewRegistry("tgt")
	tgt.Bool("v")
	f := NewFederation("fed").Resolve(tgt, noopResolver)
	msg := mustPanic(t, func() { f.Resolve(tgt, noopResolver) })
	if !strings.Contains(msg, `"tgt"`) || !strings.Contains(msg, "second Resolver") {
		t.Fatalf("panic should name the target and the duplicate resolver, got %q", msg)
	}
}

// TestEmbed_ResolverConflictPanics: an embedded sub's resolver for a target that already has one
// (declared on the parent) panics instead of replacing it.
func TestEmbed_ResolverConflictPanics(t *testing.T) {
	tgt := NewRegistry("tgt")
	tgt.Bool("v")
	sub := NewFederation("sub").Resolve(tgt, noopResolver)
	parent := NewFederation("parent").Resolve(tgt, noopResolver)
	msg := mustPanic(t, func() { parent.Embed(sub) })
	if !strings.Contains(msg, `Embed("sub")`) || !strings.Contains(msg, `"tgt"`) {
		t.Fatalf("panic should name the embed and the target, got %q", msg)
	}

	// Resolve after the embed conflicts the same way.
	parent2 := NewFederation("parent2").Embed(sub)
	mustPanic(t, func() { parent2.Resolve(tgt, noopResolver) })
}

// TestEmbedCertified_ResolverConflictPanics: EmbedCertified checks the same way as Embed.
func TestEmbedCertified_ResolverConflictPanics(t *testing.T) {
	tgt := NewRegistry("tgt")
	tgt.Bool("v")
	sub := NewFederation("sub").Resolve(tgt, noopResolver)
	parent := NewFederation("parent").Resolve(tgt, noopResolver)
	msg := mustPanic(t, func() { parent.EmbedCertified(sub, nil) })
	if !strings.Contains(msg, `EmbedCertified("sub")`) {
		t.Fatalf("panic should name EmbedCertified, got %q", msg)
	}
}

// copyCycle wires the two-registry monotone copy cycle x.v <-> y.v onto parent.
func copyCycle(parent *Federation, x, y *Registry) {
	xv, yv := x.vars[0], y.vars[0]
	parent.Morphism(x, y).Shared(yv).Map(func(s, d State) State { return d.SetBool(yv, s.GetBool(xv)) }).Add()
	parent.Morphism(y, x).Shared(xv).Map(func(s, d State) State { return d.SetBool(xv, s.GetBool(yv)) }).Add()
}

// TestEmbed_MonotoneOptInDoesNotCarryOver: a parent that embeds a sub which called
// AllowMonotoneCycles is not opted in itself; a cycle in the parent is rejected, and the error
// names the sub and tells the parent to opt in. Once the parent opts in, it builds.
func TestEmbed_MonotoneOptInDoesNotCarryOver(t *testing.T) {
	x := NewRegistry("x")
	x.Bool("v")
	y := NewRegistry("y")
	y.Bool("v")
	sub := NewFederation("sub").AllowMonotoneCycles().Add(x)

	parent := NewFederation("parent").Embed(sub)
	if parent.allowCycles {
		t.Fatal("Embed must not opt the parent in to AllowMonotoneCycles")
	}
	copyCycle(parent, x, y)
	_, rep, err := parent.Build()
	if err == nil {
		t.Fatal("a parent that never opted in must reject a cycle")
	}
	if !strings.Contains(err.Error(), `["sub"]`) || !strings.Contains(err.Error(), "does not carry over") {
		t.Fatalf("error should name the opted-in sub and say the opt-in does not carry over, got %v", err)
	}
	if rep.Assurance != "" {
		t.Fatalf("a rejected federation has no assurance, got %q", rep.Assurance)
	}

	parent.AllowMonotoneCycles()
	if _, _, err := parent.Build(); err != nil {
		t.Fatalf("the parent's own opt-in should allow the monotone cycle: %v", err)
	}
}

// TestEmbedCertified_MonotoneOptInDoesNotCarryOver: the same for EmbedCertified.
func TestEmbedCertified_MonotoneOptInDoesNotCarryOver(t *testing.T) {
	x := NewRegistry("x")
	x.Bool("v")
	sub := NewFederation("sub").AllowMonotoneCycles().Add(x)
	parent := NewFederation("parent").EmbedCertified(sub, nil)
	if parent.allowCycles {
		t.Fatal("EmbedCertified must not opt the parent in to AllowMonotoneCycles")
	}
	if len(parent.monotoneSubs) != 1 || parent.monotoneSubs[0] != "sub" {
		t.Fatalf("monotoneSubs = %v, want [sub]", parent.monotoneSubs)
	}
}

// TestFedReport_Assurance: a built federation's report states the federation-level checks that
// ran and that they are Go-checked, not oracle-certified.
func TestFedReport_Assurance(t *testing.T) {
	_, rep, err := NewFederation("ms").
		Morphism(msMfr(t)).Shared(msListing).Map(msMap).Add().
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Assurance, "not oracle-certified") {
		t.Fatalf("Assurance should say the federated composition is not oracle-certified, got %q", rep.Assurance)
	}
	joined := strings.Join(rep.Checks, "\n")
	for _, want := range []string{"M1: 1 single-source", "R1/R2: 0 resolver", "acyclicity"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Checks missing %q:\n%s", want, joined)
		}
	}
	out := rep.String()
	if !strings.Contains(out, "Federation assurance: components oracle-gated") ||
		!strings.Contains(out, "checked (Go, not oracle): M1") {
		t.Fatalf("String() should print the federation assurance and checks:\n%s", out)
	}

	// A failed build reports no assurance.
	empty := &FedReport{Name: "x"}
	if !strings.Contains(empty.String(), "Federation assurance: not certified") {
		t.Fatalf("an unbuilt report should say not certified:\n%s", empty)
	}
}

// TestFedReport_AssuranceMonotone: a cyclic federation's checks name the monotonicity check.
func TestFedReport_AssuranceMonotone(t *testing.T) {
	x := NewRegistry("x")
	x.Bool("v")
	y := NewRegistry("y")
	y.Bool("v")
	f := NewFederation("loop").AllowMonotoneCycles()
	copyCycle(f, x, y)
	_, rep, err := f.Build()
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(rep.Checks, "\n"); !strings.Contains(joined, "monotone cycles") || strings.Contains(joined, "acyclicity") {
		t.Fatalf("Checks should name the monotone check, not acyclicity:\n%s", joined)
	}
}

var (
	msListing Var
	msMState  Var
)

// msMfr returns a one-edge manufacturer/supplier pair for the assurance test.
func msMfr(t *testing.T) (*Registry, *Registry) {
	t.Helper()
	mfr := NewRegistry("mfr")
	msMState = mfr.Enum("mstate", "draft", "active")
	mfr.Event("pub").Writes(msMState).Apply(func(s State) State { return s.Set(msMState, "active") }).Add()
	sup := NewRegistry("sup")
	msListing = sup.Enum("listing", "hidden", "listed")
	return mfr, sup
}

func msMap(src, d State) State {
	if src.Get(msMState) == "active" {
		return d.Set(msListing, "listed")
	}
	return d.Set(msListing, "hidden")
}

// TestBuildOrSynthesize_ReportsSubstitution: when BuildOrSynthesize returns a synthesized machine,
// the Synthesis says so prominently and carries the Build error; Synthesize alone does not.
func TestBuildOrSynthesize_ReportsSubstitution(t *testing.T) {
	mk := func() *Registry {
		r := NewRegistry("approval")
		stage := r.Enum("stage", "pending", "approved")
		bal := r.Int("bal", 0, 1)
		r.Invariant("funded").Watches(stage, bal).
			Holds(func(s State) bool { return s.Get(stage) != "approved" || s.GetInt(bal) >= 1 }).
			Add() // no Repair: Build fails, synthesis substitutes one
		r.Event("approve").Writes(stage).Apply(func(s State) State { return s.Set(stage, "approved") }).Add()
		r.Event("credit").Writes(bal).Apply(func(s State) State { return s.SetInt(bal, 1) }).Add()
		return r
	}
	m, syn, err := mk().BuildOrSynthesize()
	if err != nil || m == nil || syn == nil {
		t.Fatalf("expected a synthesized machine, got m=%v syn=%v err=%v", m, syn, err)
	}
	if !syn.Substituted {
		t.Fatal("Substituted should be set when the synthesized machine is returned")
	}
	if syn.BuildError == nil {
		t.Fatal("BuildError should hold the Build error that triggered the fallback")
	}
	out := syn.String()
	if !strings.HasPrefix(strings.SplitN(out, "\n", 2)[1], "  REPAIR SYNTHESIZED") {
		t.Fatalf("String() should open with the substitution line:\n%s", out)
	}

	plain, err := mk().Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if plain.Substituted || plain.BuildError != nil || strings.Contains(plain.String(), "REPAIR SYNTHESIZED") {
		t.Fatalf("Synthesize alone must not report a substitution:\n%s", plain)
	}
}

// TestExport_IncludesCheckedPairs: Export (format version 2) lists the event pairs CC was
// checked for and whether that is every pair.
func TestExport_IncludesCheckedPairs(t *testing.T) {
	mk := func(declare bool) *Machine {
		r := NewRegistry("pairs")
		a := r.Bool("a")
		b := r.Bool("b")
		c := r.Bool("c")
		r.Event("ea").Writes(a).Apply(func(s State) State { return s.SetBool(a, true) }).Add()
		r.Event("eb").Writes(b).Apply(func(s State) State { return s.SetBool(b, true) }).Add()
		r.Event("ec").Writes(c).Apply(func(s State) State { return s.SetBool(c, true) }).Add()
		if declare {
			r.Independent("ec", "ea")
		}
		m, _, err := r.Build()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	type out struct {
		Version      int `json:"version"`
		Verification struct {
			AllPairs bool        `json:"all_pairs"`
			Pairs    [][2]string `json:"pairs"`
		} `json:"verification"`
	}
	read := func(m *Machine) out {
		path := t.TempDir() + "/m.json"
		if err := m.Export(path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var o out
		if err := json.Unmarshal(data, &o); err != nil {
			t.Fatal(err)
		}
		return o
	}

	all := read(mk(false))
	if all.Version != 2 || !all.Verification.AllPairs || len(all.Verification.Pairs) != 3 {
		t.Fatalf("all pairs: got %+v", all)
	}
	decl := read(mk(true))
	if decl.Verification.AllPairs || len(decl.Verification.Pairs) != 1 ||
		decl.Verification.Pairs[0] != [2]string{"ea", "ec"} {
		t.Fatalf("declared pairs: got %+v", decl)
	}
}
