package gsm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// PolicyFormatVersion identifies the combinator-policy serialization that the digest
// and the verified AST oracle agree on. Bump it only when the on-wire format emitted
// by WriteMachineAST changes (which also requires updating the oracle's parser), so
// that a digest always names one unambiguous format.
const PolicyFormatVersion = "gsm-policy-v1"

// PolicyBytes returns the canonical serialization of this registry's combinator rules
// (the same S-expression form WriteMachineAST emits), or an error if any rule falls
// outside the serializable fragment. This is exactly the artifact the verified AST
// oracle re-checks: keeping the digested bytes and the oracle's input identical is
// what lets a policy be a portable, independently verifiable object rather than an
// opaque blob only this process understands.
func (r *Registry) PolicyBytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PolicyDigest returns a stable, domain-separated SHA-256 over the format version and
// the canonical policy bytes, as lowercase hex. Two registries produce the same digest
// exactly when their serialized combinator rules are identical, so the digest names a
// policy independently of who built it or in which layer (a rule written with the
// sugar surface digests the same as the equivalent primitive combinators). Callers
// anchor this digest in an audit trail; a verifier recomputes it from PolicyBytes and
// checks the anchored value, then runs the external oracle on those same bytes.
func (r *Registry) PolicyDigest() (string, error) {
	b, err := r.PolicyBytes()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(PolicyFormatVersion))
	h.Write([]byte{'\n'})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}
