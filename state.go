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
func (s State) Set(v Var, val string) State {
	idx, err := v.enumIndex(val)
	if err != nil {
		panic(err)
	}
	return s.setRaw(v, uint64(idx))
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
func (s State) getRaw(v Var) uint64 {
	mask := uint64((1 << v.bits) - 1)
	return (s.packed >> v.offset) & mask
}

// setRaw returns a new State with a variable's raw integer set.
func (s State) setRaw(v Var, val uint64) State {
	mask := uint64((1 << v.bits) - 1)
	cleared := s.packed &^ (mask << v.offset)
	return State{
		packed: cleared | ((val & mask) << v.offset),
		vars:   s.vars,
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
