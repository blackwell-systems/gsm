package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestStateDigest_StableAndDistinguishing: equal states digest equally, distinct states differ,
// and the digest is lowercase hex SHA-256. Replaying the same events from the same start must
// reproduce the same digest, which is what makes a committed state_digest verifiable.
func TestStateDigest_StableAndDistinguishing(t *testing.T) {
	r := gsm.NewRegistry("cap")
	a := r.Int("a", 0, 5)
	r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))
	r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}

	s0 := m.NewState()
	d0 := s0.Digest()
	if len(d0) != 64 || strings.ToLower(d0) != d0 {
		t.Fatalf("expected 64-char lowercase hex digest, got %q", d0)
	}

	// Two independent replays of the same event reach the same digest.
	s1a := m.Apply(s0, "inc_a")
	s1b := m.Apply(m.NewState(), "inc_a")
	if s1a.Digest() != s1b.Digest() {
		t.Fatalf("same transition produced different digests: %s vs %s", s1a.Digest(), s1b.Digest())
	}
	// A different state has a different digest.
	if s1a.Digest() == d0 {
		t.Fatalf("distinct states digested equally")
	}
	// The cap makes inc_a idempotent at 3: from a=3, inc_a stays a=3, same digest.
	s3 := m.Apply(m.Apply(m.Apply(s0, "inc_a"), "inc_a"), "inc_a")
	if got := m.Apply(s3, "inc_a").Digest(); got != s3.Digest() {
		t.Fatalf("expected capped transition to be idempotent in digest: %s vs %s", got, s3.Digest())
	}
}
