package gsm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
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

// PolicyNames returns the canonical serialization of what the rules are addressed by,
// which PolicyBytes (the oracle's input) leaves out because the oracle addresses
// variables and events by position: each variable's name, kind and enum labels, each
// event's name, and the event pairs CC is checked for (WriteDeclaredPairs). Replay logs,
// projections, certificate tables, input ports and Set address these by name, and the
// declared pairs decide which orderings the CC check covers, so two registries that
// differ in any of them are different policies even when their rules serialize alike.
//
//	(names
//	 (var "status" enum "pending" "paid")
//	 (var "paid" bool)
//	 (event "pay")
//	 (pairs all)
//	)
func (r *Registry) PolicyNames() ([]byte, error) {
	if err := r.checkNames(); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("(names\n")
	for _, v := range r.vars {
		kind := map[VarKind]string{BoolKind: "bool", EnumKind: "enum", IntKind: "int"}[v.kind]
		fmt.Fprintf(&b, " (var %s %s", strconv.Quote(v.name), kind)
		for _, l := range v.labels {
			b.WriteString(" " + strconv.Quote(l))
		}
		b.WriteString(")\n")
	}
	for _, ev := range r.events {
		fmt.Fprintf(&b, " (event %s)\n", strconv.Quote(ev.name))
	}
	var pairs strings.Builder
	if err := r.WriteDeclaredPairs(&pairs); err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, " (%s)\n)\n", strings.TrimSuffix(pairs.String(), "\n"))
	return []byte(b.String()), nil
}

// PolicyDigest returns a stable, domain-separated SHA-256 over the format version, the
// canonical policy bytes and the policy names, as lowercase hex. Two registries produce
// the same digest exactly when their serialized combinator rules (PolicyBytes) and the
// names and pairs those rules are addressed by (PolicyNames) are identical, so the digest
// names a policy independently of who built it or in which layer (a rule written with the
// sugar surface digests the same as the equivalent primitive combinators). Callers anchor
// this digest in an audit trail; a verifier recomputes it from PolicyBytes and
// PolicyNames and checks the anchored value, then runs the external oracle on
// PolicyBytes.
func (r *Registry) PolicyDigest() (string, error) {
	b, err := r.PolicyBytes()
	if err != nil {
		return "", err
	}
	names, err := r.PolicyNames()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(PolicyFormatVersion))
	h.Write([]byte{'\n'})
	h.Write(b)
	h.Write(names)
	return hex.EncodeToString(h.Sum(nil)), nil
}
