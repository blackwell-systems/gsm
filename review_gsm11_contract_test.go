package gsm

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// G0: the published interchange contract (README "Policy as a portable artifact",
// THEORY.md, ARCHITECTURE.md, PolicyDigest's old godoc) is that the digest is
// SHA-256(PolicyFormatVersion "\n" PolicyBytes): "the digested bytes are exactly the
// oracle's input". bide's auditor recomputes exactly that, independently of gsm
// (go-agents cmd/bide-audit policyDigest), from the policy leaf that holds PolicyBytes
// and PolicyDigest (audit.RecordPolicy). After gsm#11 the two disagree for every
// policy, under the same version tag, so every policy leaf fails "lies about its digest".
func TestReview11_PolicyDigestRecomputableFromPolicyBytes(t *testing.T) {
	r := NewRegistry("kyc-decision")
	approved := r.Bool("approved")
	flag := r.Bool("flagged")
	r.Rule("no_approve_when_flagged").
		Require(Or(Is(approved, 0), Is(flag, 0))).
		RepairWith(SetTo(approved, 0)).
		Add()
	r.On("flag").Does(SetTo(flag, 1)).Add()
	r.On("approve").Does(SetTo(approved, 1)).Add()
	b, err := r.PolicyBytes()
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.PolicyDigest()
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	h.Write([]byte(PolicyFormatVersion + "\n"))
	h.Write(b)
	if got := hex.EncodeToString(h.Sum(nil)); got != d {
		t.Errorf("an independent verifier recomputing SHA-256(%q \\n PolicyBytes) gets %s; PolicyDigest is %s",
			PolicyFormatVersion, got, d)
	}
}
