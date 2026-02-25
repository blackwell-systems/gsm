package gsm

import "fmt"

// State is a compact, immutable representation of all variable values.
// Internally it is a bitpacked uint64, enabling use as a table index
// for precomputed normal forms.
type State struct {
	packed uint64
	vars   []Var // shared reference to machine's variable list
}

// Get returns the string value of an enum variable.
func (s State) Get(v Var) string {
	raw := s.getRaw(v)
	return v.enumLabel(int(raw))
}

// GetBool returns the value of a bool variable.
func (s State) GetBool(v Var) bool {
	return s.getRaw(v) != 0
}

// GetInt returns the value of an int variable (adjusted for min offset).
func (s State) GetInt(v Var) int {
	return int(s.getRaw(v)) + v.min
}

// Set returns a new State with an enum variable set to the named value.
// Panics if val is not in the variable's declared enum set. This is appropriate
// for Repair/Apply callbacks with hardcoded values. For user input, use TrySet.
func (s State) Set(v Var, val string) State {
	idx, err := v.enumIndex(val)
	if err != nil {
		panic(fmt.Sprintf("gsm: Set(%q, %q): %v", v.name, val, err))
	}
	return s.setRaw(v, uint64(idx))
}

// TrySet returns a new State with an enum variable set to the named value.
// Returns an error if val is not in the variable's declared enum set.
// Use this when the value comes from user input or external sources.
func (s State) TrySet(v Var, val string) (State, error) {
	idx, err := v.enumIndex(val)
	if err != nil {
		return State{}, err
	}
	return s.setRaw(v, uint64(idx)), nil
}

// SetBool returns a new State with a bool variable set.
func (s State) SetBool(v Var, val bool) State {
	if val {
		return s.setRaw(v, 1)
	}
	return s.setRaw(v, 0)
}

// SetInt returns a new State with an int variable set.
// Value is clamped to the variable's declared range.
func (s State) SetInt(v Var, val int) State {
	max := v.min + v.domain - 1
	if val < v.min {
		val = v.min
	}
	if val > max {
		val = max
	}
	return s.setRaw(v, uint64(val-v.min))
}

// getRaw extracts the raw (offset-adjusted) integer for a variable.
// Example: For a 3-bit variable at offset 2:
//
//	state = ...0001_1010 → (shift right 2) → ...0000_0110 → (mask 0b111) → 6
func (s State) getRaw(v Var) uint64 {
	s.checkVar(v)
	mask := uint64((1 << v.bits) - 1) // Create bitmask for v.bits: (1 << 3) - 1 = 0b111
	return (s.packed >> v.offset) & mask
}

// setRaw returns a new State with a variable's raw integer set.
// This does three steps: (1) clear the variable's bits, (2) mask the new value
// to its bit width, (3) shift and OR the masked value into position.
func (s State) setRaw(v Var, val uint64) State {
	s.checkVar(v)
	mask := uint64((1 << v.bits) - 1)
	cleared := s.packed &^ (mask << v.offset) // Clear old value: AND with inverted mask
	return State{
		packed: cleared | ((val & mask) << v.offset), // Set new value: OR with shifted bits
		vars:   s.vars,
	}
}

// checkVar panics if the variable does not belong to this state's machine.
func (s State) checkVar(v Var) {
	if v.index < 0 || v.index >= len(s.vars) || s.vars[v.index].name != v.name {
		panic(fmt.Sprintf("gsm: variable %q does not belong to this machine", v.name))
	}
}

// ID returns the packed integer, usable as a table index.
func (s State) ID() uint64 { return s.packed }

// String returns a human-readable representation.
func (s State) String() string {
	if s.vars == nil {
		return fmt.Sprintf("State(%d)", s.packed)
	}
	result := "{"
	for i, v := range s.vars {
		if i > 0 {
			result += ", "
		}
		switch v.kind {
		case BoolKind:
			result += fmt.Sprintf("%s=%v", v.name, s.GetBool(v))
		case EnumKind:
			result += fmt.Sprintf("%s=%s", v.name, s.Get(v))
		case IntKind:
			result += fmt.Sprintf("%s=%d", v.name, s.GetInt(v))
		}
	}
	return result + "}"
}
