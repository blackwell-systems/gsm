// Package gsm implements governed state machines: finite state machines
// whose states live in a registry that enforces invariants via compensation.
// Events are applied in any order; the registry guarantees convergence to
// the same valid state regardless of ordering. Convergence is verified once,
// at Build time, by exhaustive state-space enumeration (well-founded
// compensation + compensation commutativity); runtime application is an O(1)
// table lookup.
//
// Registries also federate. Connect independently-governed registries with
// directed morphisms that encode cross-registry constraints (a manufacturer's
// status fixing a supplier's listing, a regulator's rules constraining a bank),
// and gsm proves the whole network converges — the same build-time guarantee,
// across organizational boundaries. In a morphism the source is authoritative
// over its target's shared component, so cross-registry conflicts resolve
// without coordination. See Federation and FedMachine; the federated normal
// form is constructive and never materializes the product state space.
//
// Beyond verifying a compensation you wrote, gsm can also SYNTHESIZE one:
// Registry.Synthesize takes the invariants (validity) and events and generates a
// convergent compensation — or proves none exists (with a witness). SynthesizeWith
// lets a preference steer the choice, and Optimal returns the provably minimum-cost
// repair. See Synthesize and Synthesis.
//
// Rules can be declared as Go closures (Holds/Repair/Apply) or from a fixed
// combinator vocabulary (DeclInvariant/DeclEvent with V/Lit/Add/Sub, Le/Lt/Eq/
// And/Or/Not, Set/Do); combinator rules are data, so they are inspectable,
// serializable, and can be re-certified by checkers extracted from the proof.
//
// This implements the single-registry model (Section 3) and the federated
// convergence model (Section 8) of "Normalization Confluence in Federated
// Registry Networks" (Blackwell, 2026).
package gsm

import (
	"fmt"
	"strings"
)

// VarKind distinguishes variable types.
type VarKind int

const (
	BoolKind VarKind = iota
	EnumKind
	IntKind
)

// Var is a handle to a declared state variable. Users receive Vars from
// the Builder and pass them to State accessors.
type Var struct {
	name   string
	kind   VarKind
	index  int      // position in variable list
	offset uint     // bit offset in packed state
	bits   uint     // number of bits needed
	domain int      // number of distinct values
	labels []string // enum: value names; nil otherwise
	min    int      // int: minimum value (bool/enum: 0)
}

// Name returns the variable's declared name.
func (v Var) Name() string { return v.name }

// bitsNeeded returns the minimum bits to represent n distinct values.
func bitsNeeded(n int) uint {
	if n <= 1 {
		return 0
	}
	b := uint(0)
	n--
	for n > 0 {
		b++
		n >>= 1
	}
	return b
}

// enumIndex returns the integer index for a named enum value, or error.
func (v *Var) enumIndex(val string) (int, error) {
	for i, l := range v.labels {
		if l == val {
			return i, nil
		}
	}
	return 0, fmt.Errorf("gsm: enum %q has no value %q", v.name, val)
}

// describeDomain renders the variable's domain: "0..2", "{a, b}" or "bool".
func (v *Var) describeDomain() string {
	switch v.kind {
	case BoolKind:
		return "bool"
	case EnumKind:
		return fmt.Sprintf("{%s}", strings.Join(v.labels, ", "))
	default:
		return fmt.Sprintf("%d..%d", v.min, v.min+v.domain-1)
	}
}

// describe renders the variable's declaration, for schema mismatch messages.
func (v *Var) describe() string {
	return fmt.Sprintf("%q %s (%d bits at offset %d)", v.name, v.describeDomain(), v.bits, v.offset)
}

// rawLabel renders a raw field value in the variable's terms (the Int value, or the
// enum index or Bool field, which is out of range when this is called).
func (v *Var) rawLabel(raw uint64) string {
	if v.kind == IntKind {
		return fmt.Sprintf("%d", int(raw)+v.min)
	}
	return fmt.Sprintf("raw value %d", raw)
}

// enumLabel returns the string label for an integer enum index.
func (v *Var) enumLabel(idx int) string {
	if idx >= 0 && idx < len(v.labels) {
		return v.labels[idx]
	}
	return fmt.Sprintf("?%d", idx)
}
