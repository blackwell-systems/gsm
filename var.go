// Package gsm implements governed state machines: finite state machines
// whose states live in a registry that enforces invariants via compensation.
// Events are applied in any order; the registry guarantees convergence to
// the same valid state regardless of ordering.
package gsm

import "fmt"

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

// varDef is the internal definition used during building.
type varDef struct {
	v Var
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

// enumLabel returns the string label for an integer enum index.
func (v *Var) enumLabel(idx int) string {
	if idx >= 0 && idx < len(v.labels) {
		return v.labels[idx]
	}
	return fmt.Sprintf("?%d", idx)
}
