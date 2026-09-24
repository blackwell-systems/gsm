package gsm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// buildCapMachine returns a small convergent combinator machine used across the
// policy-digest tests.
func buildCapMachine(t *testing.T) *gsm.Registry {
	t.Helper()
	r := gsm.NewRegistry("cap")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)
	r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))
	r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
	r.DeclEvent("inc_b", gsm.Do(gsm.Set(b, gsm.Add(gsm.V(b), gsm.Lit(1)))))
	return r
}

// TestPolicyBytes_MatchesWriteMachineAST confirms PolicyBytes is exactly the oracle's
// input, so the digested artifact and the re-checked artifact cannot diverge.
func TestPolicyBytes_MatchesWriteMachineAST(t *testing.T) {
	r := buildCapMachine(t)
	got, err := r.PolicyBytes()
	if err != nil {
		t.Fatalf("PolicyBytes: %v", err)
	}
	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Fatalf("PolicyBytes differs from WriteMachineAST output")
	}
}

// TestPolicyDigest_StableAndDeterministic: the same rules digest to the same value
// across independent builds, and the digest is a lowercase hex SHA-256.
func TestPolicyDigest_StableAndDeterministic(t *testing.T) {
	d1, err := buildCapMachine(t).PolicyDigest()
	if err != nil {
		t.Fatalf("PolicyDigest: %v", err)
	}
	d2, err := buildCapMachine(t).PolicyDigest()
	if err != nil {
		t.Fatalf("PolicyDigest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest not deterministic: %s vs %s", d1, d2)
	}
	if len(d1) != 64 || strings.ToLower(d1) != d1 {
		t.Fatalf("expected 64-char lowercase hex digest, got %q", d1)
	}
}

// TestPolicyDigest_ChangesWithRules: a different policy digests differently.
func TestPolicyDigest_ChangesWithRules(t *testing.T) {
	base, err := buildCapMachine(t).PolicyDigest()
	if err != nil {
		t.Fatalf("PolicyDigest: %v", err)
	}
	// Same machine but the cap is 4 instead of 3: a genuinely different policy.
	r := gsm.NewRegistry("cap")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)
	r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(4)), gsm.Do(gsm.Set(a, gsm.Lit(4))))
	r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
	r.DeclEvent("inc_b", gsm.Do(gsm.Set(b, gsm.Add(gsm.V(b), gsm.Lit(1)))))
	changed, err := r.PolicyDigest()
	if err != nil {
		t.Fatalf("PolicyDigest: %v", err)
	}
	if base == changed {
		t.Fatalf("digest did not change when the policy changed")
	}
}

// TestPolicyDigest_SugarEqualsPrimitive: a rule written with the ergonomic sugar
// digests identically to the equivalent raw combinators, since both lower to the same
// serialized bytes. This is the property that lets humans author at the sugar layer
// while the audit trail commits to the primitive artifact.
func TestPolicyDigest_SugarEqualsPrimitive(t *testing.T) {
	primitive := func() (string, error) {
		r := gsm.NewRegistry("m")
		a := r.Int("a", 0, 5)
		r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))
		r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
		return r.PolicyDigest()
	}
	sugared := func() (string, error) {
		r := gsm.NewRegistry("m")
		a := r.Int("a", 0, 5)
		r.Rule("a_cap").Require(gsm.AtMost(a, 3)).RepairWith(gsm.SetTo(a, 3)).Add()
		r.On("inc_a").Does(gsm.Inc(a)).Add()
		return r.PolicyDigest()
	}
	dp, err := primitive()
	if err != nil {
		t.Fatalf("primitive digest: %v", err)
	}
	ds, err := sugared()
	if err != nil {
		t.Fatalf("sugar digest: %v", err)
	}
	if dp != ds {
		t.Fatalf("sugar and primitive digests differ:\n primitive %s\n sugar     %s", dp, ds)
	}
}
