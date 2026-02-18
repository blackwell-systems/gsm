package gsm

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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

// exportFormat is the portable JSON/MessagePack representation of a verified machine.
// Runtime implementations in other languages can load this format and perform
// O(1) event application via table lookups, without reimplementing verification.
type exportFormat struct {
	Name         string      `json:"name"`
	Version      int         `json:"version"`
	Vars         []varExport `json:"vars"`
	Events       []string    `json:"events"`
	NF           []uint64    `json:"nf"`
	Step         [][]uint64  `json:"step"`
	Verification verifyInfo  `json:"verification"`
	ExportedAt   string      `json:"exported_at"`
}

type varExport struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`   // "bool", "enum", "int"
	Labels []string `json:"labels,omitempty"` // enum only
	Min    int      `json:"min,omitempty"`    // int only
	Max    int      `json:"max,omitempty"`    // int only
}

type verifyInfo struct {
	WFC          bool   `json:"wfc"`
	CC           bool   `json:"cc"`
	MaxRepairLen int    `json:"max_repair_depth"`
	StateCount   int    `json:"state_count"`
	EventCount   int    `json:"event_count"`
	VerifiedAt   string `json:"verified_at,omitempty"`
}

// Export writes the verified machine to a portable JSON format.
// The exported file can be loaded by runtime implementations in any language,
// enabling O(1) event application without reimplementing verification.
//
// The format contains:
//   - State variable definitions (types, domains)
//   - Event names (ordered)
//   - Normal form table: nf[stateID] → normalized stateID
//   - Step table: step[eventID][stateID] → normalized result stateID
//   - Verification metadata (WFC/CC results, state count, etc.)
//
// Runtime libraries only need to:
//   1. Load the JSON
//   2. Implement Apply(state, event) as step[events[event]][state]
//
// Example runtime (Python):
//
//	import json
//	class Machine:
//	    def __init__(self, path):
//	        with open(path) as f:
//	            d = json.load(f)
//	        self.events = {n: i for i, n in enumerate(d['events'])}
//	        self.step = d['step']
//	    def apply(self, state, event):
//	        return self.step[self.events[event]][state]
func (m *Machine) Export(path string) error {
	eventNames := m.Events()

	vars := make([]varExport, len(m.vars))
	for i, v := range m.vars {
		vd := varExport{Name: v.name}
		switch v.kind {
		case BoolKind:
			vd.Kind = "bool"
		case EnumKind:
			vd.Kind = "enum"
			vd.Labels = v.labels
		case IntKind:
			vd.Kind = "int"
			vd.Min = v.min
			vd.Max = v.min + v.domain - 1
		}
		vars[i] = vd
	}

	export := exportFormat{
		Name:       m.name,
		Version:    1,
		Vars:       vars,
		Events:     eventNames,
		NF:         m.nf,
		Step:       m.step,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Verification: verifyInfo{
			WFC:        true, // Machine only exists if verification passed
			CC:         true,
			StateCount: len(m.nf),
			EventCount: len(eventNames),
		},
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("gsm: marshal failed: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("gsm: write failed: %w", err)
	}

	return nil
}
