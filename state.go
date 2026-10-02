package gsm

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// State is a compact, immutable representation of all variable values.
// Internally it is a bitpacked uint64, enabling use as a table index
// for precomputed normal forms.
type State struct {
	packed uint64
	vars   []Var // shared reference to machine's variable list
}

// StateDigestVersion is the domain-separation tag for State.Digest. Bump it only if the packing
// that Digest hashes over changes, so a digest always names one unambiguous encoding.
const StateDigestVersion = "gsm-state-v1"

// Digest returns a stable, domain-separated SHA-256 over the state's packed value, as lowercase
// hex. It is meaningful alongside a policy digest (which pins the variable layout the packing
// depends on): the pair (policy digest, state digest) unambiguously names a state, and a reference
// build of the same policy replaying the same events reproduces the same digest. An audit leaf that
// records both binds an action to the exact resulting state, so a verifier replaying the policy can
// confirm the runtime's state matched the reference at each transition.
func (s State) Digest() string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], s.packed)
	h := sha256.New()
	h.Write([]byte(StateDigestVersion))
	h.Write([]byte{'\n'})
	h.Write(buf[:])
	return hex.EncodeToString(h.Sum(nil))
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

// sameVar reports whether a and b declare the same variable: name, kind, position and
// width in the packed encoding, domain, minimum and enum labels.
func sameVar(a, b Var) bool {
	if a.name != b.name || a.kind != b.kind || a.index != b.index || a.offset != b.offset ||
		a.bits != b.bits || a.domain != b.domain || a.min != b.min || len(a.labels) != len(b.labels) {
		return false
	}
	for i := range a.labels {
		if a.labels[i] != b.labels[i] {
			return false
		}
	}
	return true
}

// notStateOf returns why s is not a state of the machine whose variables are vars, or nil
// when it is. Membership is by value, so it does not matter which Registry or Machine
// produced s:
//
//   - s has the machine's variable schema: as many variables, each the same as the
//     machine's at that position (sameVar). A State from another machine instance with an
//     identical declaration list qualifies; one with a different list does not, because its
//     packed value means something else under this machine's layout.
//   - every variable's field holds a value inside its domain (an enum index below the label
//     count, an Int within min..max, a Bool 0 or 1);
//   - no bit is set outside the variables' fields.
//
// These are exactly the encodings Build enumerates, so a state passes iff Build's tables
// have an entry for it.
func notStateOf(vars []Var, s State) error {
	if len(s.vars) != len(vars) {
		return fmt.Errorf("it has %d variables, the machine has %d", len(s.vars), len(vars))
	}
	// Fast path: a state that shares the machine's variable slice has its schema.
	if len(vars) > 0 && &s.vars[0] != &vars[0] {
		for i := range vars {
			if !sameVar(s.vars[i], vars[i]) {
				return fmt.Errorf("its variable %d is %s, the machine's is %s", i, s.vars[i].describe(), vars[i].describe())
			}
		}
	}
	var used uint64
	for _, v := range vars {
		mask := uint64((1 << v.bits) - 1)
		used |= mask << v.offset
		if raw := (s.packed >> v.offset) & mask; raw >= uint64(v.domain) {
			return fmt.Errorf("variable %q holds %s, outside %s", v.name, v.rawLabel(raw), v.describeDomain())
		}
	}
	if extra := s.packed &^ used; extra != 0 {
		return fmt.Errorf("it sets bits outside the machine's encoding (%#x)", extra)
	}
	return nil
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
