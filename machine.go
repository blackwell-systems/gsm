package gsm

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
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

	valid []bool // valid[stateID]: encoding is in-domain (every variable within its domain)

	dom *domainCheck // decides whether a State is a state of this machine

	// The event pairs CC was verified for (Registry.ccPairs, each with i < j), and
	// whether that is every pair (no Independent declarations). Recorded so the
	// tables can be re-checked against the same property.
	ccPairs  [][2]int
	allPairs bool

	// Lazy path (BuildCompositional): for machines whose global state space is too
	// large to tabulate, Apply/Normalize compute at runtime from the rules instead
	// of O(1) table lookups. step/nf are nil in this mode.
	lazy       bool
	invariants []invariantDef
	eventDefs  []eventDef
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
//
// The input must be a state of this machine (see EffectFunc): one from NewState, Apply,
// Normalize or MergeProjection, or a state of a structurally identical machine. A table
// machine does not check this, since the check would cost more than the lookup itself; an
// input outside the machine selects an arbitrary table entry or panics with an index out
// of range. A lazy machine checks the input and panics, naming it.
//
// A lazy machine (BuildCompositional) has no tables: Apply runs the event's effect and
// the repairs at call time, and panics if one returns something that is not a state of
// this machine (see EffectFunc), naming the rule, the input state and the result.
// BuildCompositional checked the results only on the states it enumerates (each
// component's states with the other variables at zero, and the footprint check's one- and
// two-variable perturbations of them), so a closure that goes wrong elsewhere, for
// example one that depends on three outside variables jointly or on mutable outside data,
// is caught here. The check costs one mask test plus one comparison per variable whose
// domain does not fill its bit field, per closure call.
func (m *Machine) Apply(s State, event string) State {
	ei, ok := m.events[event]
	if !ok {
		panic(fmt.Sprintf("gsm: unknown event %q", event))
	}
	if m.lazy {
		m.mustBeInput("Apply", s)
		return m.lazyApply(m.eventDefs[ei], s)
	}
	return State{
		packed: m.step[ei][s.packed],
		vars:   m.vars,
	}
}

// Normalize returns the normal form of a state.
// If the state is already valid, returns it unchanged. The input must be a state of this
// machine, as for Apply. On a lazy machine it checks the input, runs the repairs, and
// panics on an input or a result outside the machine, as Apply does.
func (m *Machine) Normalize(s State) State {
	if m.lazy {
		m.mustBeInput("Normalize", s)
		return m.lazyNormalize(s)
	}
	return State{
		packed: m.nf[s.packed],
		vars:   m.vars,
	}
}

// IsValid returns true if all invariants hold for the state. The input must be a state of
// this machine, as for Apply (IsValid does not check it on either kind of machine).
func (m *Machine) IsValid(s State) bool {
	if m.lazy {
		return m.allHold(s)
	}
	return m.nf[s.packed] == s.packed
}

// --- lazy runtime (BuildCompositional machines) ---

func (m *Machine) allHold(s State) bool {
	for _, inv := range m.invariants {
		if !inv.check(s) {
			return false
		}
	}
	return true
}

// mustBeInput panics unless s, the input to op on a lazy machine, is a state of this
// machine. The rules run on the input, so a foreign or out-of-domain input would be blamed
// on the first rule result it produces, or (when no rule fires) returned unchanged.
func (m *Machine) mustBeInput(op string, s State) {
	if err := m.dom.notStateOf(s); err != nil {
		panic(fmt.Sprintf("gsm: %s: input %s is not a state of machine %q: %v", op, s, m.name, err))
	}
}

// mustBeState panics unless out, which a rule returned for input in, is a state of this
// machine (domainCheck). BuildCompositional checked the rules on every state of each
// component, but a lazy machine runs them again at Apply time on states it never saw, so
// the result is checked here, where it is computed. Returns out with this machine's
// variable list.
func (m *Machine) mustBeState(kind, name, part string, in, out State) State {
	if err := m.dom.ruleError(m.name, kind, name, part, in, out); err != nil {
		panic(err.Error())
	}
	return State{packed: out.packed, vars: m.vars}
}

func (m *Machine) lazyNormalize(s State) State {
	for !m.allHold(s) {
		for _, inv := range m.invariants {
			if !inv.check(s) {
				s = m.mustBeState("invariant", inv.name, "repair", s, inv.repair(s))
				break
			}
		}
	}
	return s
}

func (m *Machine) lazyApply(ev eventDef, s State) State {
	after := s
	if ev.guard == nil || ev.guard(s) {
		after = m.mustBeState("event", ev.name, "effect", s, ev.effect(s))
	}
	return m.lazyNormalize(after)
}

// MergeProjection overwrites, on state s, the target variables named in a shared projection
// received from a parent registry, returning the merged state. A distributed target node
// uses it to incorporate its parent's shared component without holding the parent's state or
// the federated machine. (Merging shared variables preserves local validity by the M1
// guarantee, so no re-normalization is required.)
//
// It returns s unchanged and an error if s is not a state of this machine (see EffectFunc),
// if the projection names a variable this machine does not have, or if a value is outside
// its variable's domain (out of range, or wider than the variable's bit field). A projection
// arrives from another node, so its values are checked rather than written or truncated.
func (m *Machine) MergeProjection(s State, p Projection) (State, error) {
	if err := m.dom.notStateOf(s); err != nil {
		return s, fmt.Errorf("gsm: MergeProjection: state %s is not a state of machine %q: %v", s, m.name, err)
	}
	names := make([]string, 0, len(p.Shared))
	for name := range p.Shared {
		names = append(names, name)
	}
	sort.Strings(names) // report the same variable first on every call
	out := s
	for _, name := range names {
		v, ok := m.varByName(name)
		if !ok {
			return s, fmt.Errorf("gsm: projection variable %q is not in machine %q", name, m.name)
		}
		if raw := p.Shared[name]; raw >= uint64(v.domain) {
			return s, fmt.Errorf("gsm: projection value %s for %q is outside %s", v.rawLabel(raw), name, v.describeDomain())
		}
		out = out.setRaw(v, p.Shared[name])
	}
	return out, nil
}

func (m *Machine) varByName(name string) (Var, bool) {
	for _, v := range m.vars {
		if v.name == name {
			return v, true
		}
	}
	return Var{}, false
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
	Kind   string   `json:"kind"`             // "bool", "enum", "int"
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
//  1. Load the JSON
//  2. Implement Apply(state, event) as step[events[event]][state]
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
	if m.lazy {
		return fmt.Errorf("gsm: cannot Export a compositionally-verified machine (no global tables); Export is for Build machines")
	}
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

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("gsm: write failed: %w", err)
	}

	return nil
}

// WriteConvergenceTables writes the machine's tables in the plain-text format the
// external verified table checker consumes (normalization-confluence
// coq/extraction, format version 2):
//
//	gsm-tables 2
//	V nE
//	nf <V state ids>
//	pairs all | pairs k a1 b1 ... ak bk
//	<event 0: V next-state ids>
//	...
//
// It emits only the in-domain encodings (every variable within its domain),
// remapped to a compact 0..V-1 index in encoding order, so the zero state is id 0.
// nf is the normal-form table; a state is valid when nf[s] = s, which is how
// IsValid is defined. pairs names the event pairs CC was verified for ("all"
// when no pairs were declared independent). The checker certifies, independently
// of this Go code, the property Build verifies: every normal form and every step
// lands on a valid state, and every declared pair commutes on every valid state
// and on the zero state. Only available for table-driven machines (compositional
// machines have no global tables).
func (m *Machine) WriteConvergenceTables(path string) error {
	if m.lazy {
		return fmt.Errorf("gsm: WriteConvergenceTables needs global step tables (a Build machine), not a compositional one")
	}
	b, err := m.convergenceTables()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// inDomain reports whether every variable of a packed encoding is within its
// domain (Registry.isValidEncoding, from the machine's own variables).
func (m *Machine) inDomain(packed uint64) bool {
	for _, v := range m.vars {
		mask := uint64((1 << v.bits) - 1)
		if int((packed>>v.offset)&mask) >= v.domain {
			return false
		}
	}
	return true
}

func (m *Machine) convergenceTables() ([]byte, error) {
	newID := make(map[uint64]int)
	var order []uint64
	for s := 0; s < len(m.nf); s++ {
		if m.inDomain(uint64(s)) {
			newID[uint64(s)] = len(order)
			order = append(order, uint64(s))
		}
	}
	id := func(packed uint64) (int, error) {
		i, ok := newID[packed]
		if !ok {
			return 0, fmt.Errorf("gsm: table entry %d is not an in-domain encoding", packed)
		}
		return i, nil
	}
	nE := len(m.step)
	var b strings.Builder
	fmt.Fprintf(&b, "gsm-tables 2\n%d %d\nnf", len(order), nE)
	for _, old := range order {
		i, err := id(m.nf[old])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, " %d", i)
	}
	if m.allPairs {
		b.WriteString("\npairs all\n")
	} else {
		fmt.Fprintf(&b, "\npairs %d", len(m.ccPairs))
		for _, p := range m.ccPairs {
			fmt.Fprintf(&b, " %d %d", p[0], p[1])
		}
		b.WriteByte('\n')
	}
	for e := 0; e < nE; e++ {
		for k, old := range order {
			i, err := id(m.step[e][old])
			if err != nil {
				return nil, err
			}
			if k > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%d", i)
		}
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}
