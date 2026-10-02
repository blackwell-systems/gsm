package gsm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
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
// which PolicyBytes (the oracle's input), and so PolicyDigest, leave out because the
// oracle addresses variables and events by position: each variable's name, kind and enum
// labels, each event's name, and the event pairs CC is checked for, as a set (sorted and
// deduplicated, so the order and direction of Independent declarations do not matter).
// PolicyIdentityDigest and certificate digests cover it. Replay logs,
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
//
// With Independent declarations the last line lists the pairs by event index instead,
// for example (pairs (0 1) (1 2)).
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
	// The declared pairs as a set: sorted and deduplicated (each pair already has its
	// lower event index first), so declaring the same pairs in another order, in the
	// other direction, or twice is the same policy.
	if r.allIndependent {
		b.WriteString(" (pairs all)\n")
	} else {
		ps := r.ccPairs()
		sort.Slice(ps, func(i, j int) bool {
			if ps[i][0] != ps[j][0] {
				return ps[i][0] < ps[j][0]
			}
			return ps[i][1] < ps[j][1]
		})
		b.WriteString(" (pairs")
		for k, p := range ps {
			if k > 0 && p == ps[k-1] {
				continue
			}
			fmt.Fprintf(&b, " (%d %d)", p[0], p[1])
		}
		b.WriteString(")\n")
	}
	b.WriteString(")\n")
	return []byte(b.String()), nil
}

// PolicyDigest returns a stable, domain-separated SHA-256 over the format version and
// the canonical policy bytes, as lowercase hex: SHA-256(PolicyFormatVersion "\n"
// PolicyBytes). Two registries produce the same digest exactly when their serialized
// combinator rules are identical, so the digest names a policy independently of who built
// it or in which layer (a rule written with the sugar surface digests the same as the
// equivalent primitive combinators). Callers anchor this digest in an audit trail; a
// verifier recomputes it from PolicyBytes and checks the anchored value, then runs the
// external oracle on those same bytes.
//
// It covers exactly the oracle's input, which addresses variables and events by position,
// so it does not cover the names the rules are addressed by (PolicyNames): two registries
// that differ only in a variable, event or enum-label name, a variable's kind, or the
// declared Independent pairs have the same PolicyDigest. Use PolicyIdentityDigest for a
// fingerprint that also covers those. Moving the names into the serialized format (and so
// into PolicyDigest) is planned before 1.0.
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

// PolicyIdentityVersion is the domain-separation tag for PolicyIdentityDigest.
const PolicyIdentityVersion = "gsm-policy-identity-v1"

// PolicyIdentityDigest returns a stable, domain-separated SHA-256 over the rules and the
// names they are addressed by, as lowercase hex: SHA-256(PolicyIdentityVersion "\n"
// PolicyBytes PolicyNames). Unlike PolicyDigest it changes when a variable, event or
// enum-label name, a variable's kind, or the declared Independent pairs change, so it is
// the fingerprint to use when those matter (replay logs, projections and ports address
// by name, and the pairs decide which orderings CC covers). Certificate digests cover the
// same names.
func (r *Registry) PolicyIdentityDigest() (string, error) {
	b, err := r.PolicyBytes()
	if err != nil {
		return "", err
	}
	names, err := r.PolicyNames()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(PolicyIdentityVersion))
	h.Write([]byte{'\n'})
	h.Write(b)
	h.Write(names)
	return hex.EncodeToString(h.Sum(nil)), nil
}
