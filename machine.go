package gsm

import "fmt"

// Machine is an immutable, verified governed state machine.
// Created by Builder.Build() after WFC and CC verification passes.
// All operations are table lookups — no computation at runtime.
type Machine struct {
	name   string
	vars   []Var
	events map[string]int // event name → index
	step   [][]uint64     // step[event][stateID] → normal form stateID
	nf     []uint64       // nf[stateID] → normal form stateID
}

// Name returns the machine's name.
func (m *Machine) Name() string { return m.name }

// NewState returns the zero state (all variables at their minimum/first value).
func (m *Machine) NewState() State {
	return State{packed: 0, vars: m.vars}
}

// Apply processes an event, returning the unique normal form.
// This is a single table lookup — O(1).
// Panics if the event name is unknown.
func (m *Machine) Apply(s State, event string) State {
	ei, ok := m.events[event]
	if !ok {
		panic(fmt.Sprintf("gsm: unknown event %q", event))
	}
	return State{
		packed: m.step[ei][s.packed],
		vars:   m.vars,
	}
}

// Normalize returns the normal form of a state.
// If the state is already valid, returns it unchanged.
func (m *Machine) Normalize(s State) State {
	return State{
		packed: m.nf[s.packed],
		vars:   m.vars,
	}
}

// IsValid returns true if all invariants hold for the state.
func (m *Machine) IsValid(s State) bool {
	return m.nf[s.packed] == s.packed
}

// Events returns the names of all declared events.
func (m *Machine) Events() []string {
	names := make([]string, len(m.events))
	for name, idx := range m.events {
		names[idx] = name
	}
	return names
}
