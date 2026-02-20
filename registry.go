package gsm

import (
	"fmt"
)

// Registry holds the rules that govern state machines: variables, invariants,
// compensation functions, and events. After declaring these, call Build() to
// verify convergence properties and produce an immutable Machine.
//
// The registry is the central authority that defines what states are valid
// and how to repair invalid states through compensation.
type Registry struct {
	name           string
	vars           []Var
	invariants     []invariantDef
	events         []eventDef
	totalBits      uint
	independent    [][2]int // pairs of event indices declared independent
	allIndependent bool     // if true, check all pairs
}

// CheckFunc is a predicate over State.
type CheckFunc func(State) bool

// EffectFunc transforms a State.
type EffectFunc func(State) State

type invariantDef struct {
	name      string
	footprint []int // indices into vars
	check     CheckFunc
	repair    EffectFunc
}

type eventDef struct {
	name   string
	writes []int // indices into vars
	guard  CheckFunc
	effect EffectFunc
}

// NewRegistry creates a Registry for a named state machine.
// By default, all event pairs are checked for CC. Use Independent()
// to restrict checking to specific pairs.
func NewRegistry(name string) *Registry {
	return &Registry{name: name, allIndependent: true}
}

// Independent declares that two events may arrive in either order
// (they are not causally related). Compensation Commutativity (CC)
// will be checked for this pair.
//
// Calling Independent() automatically switches to declared-only mode:
// only explicitly declared pairs will be verified. This avoids checking
// all O(n²) event pairs when most are causally ordered.
func (r *Registry) Independent(e1name, e2name string) *Registry {
	// Auto-switch to declared-only mode when Independent is used
	r.allIndependent = false
	r.independent = append(r.independent, [2]int{
		r.eventIndex(e1name),
		r.eventIndex(e2name),
	})
	return r
}

// OnlyDeclaredPairs explicitly switches Compensation Commutativity (CC) checking
// to only the event pairs declared via Independent(). This is now automatic when
// you call Independent(), but this method remains for explicitness and backward
// compatibility.
func (r *Registry) OnlyDeclaredPairs() *Registry {
	r.allIndependent = false
	return r
}

func (r *Registry) eventIndex(name string) int {
	for i, ev := range r.events {
		if ev.name == name {
			return i
		}
	}
	panic(fmt.Sprintf("gsm: unknown event %q", name))
}

// Bool declares a boolean state variable.
func (r *Registry) Bool(name string) Var {
	v := Var{
		name:   name,
		kind:   BoolKind,
		index:  len(r.vars),
		offset: r.totalBits,
		bits:   1,
		domain: 2,
		min:    0,
	}
	r.totalBits += 1
	r.vars = append(r.vars, v)
	return v
}

// Enum declares an enumerated state variable.
func (r *Registry) Enum(name string, values ...string) Var {
	if len(values) < 2 {
		panic(fmt.Sprintf("gsm: enum %q needs at least 2 values", name))
	}
	bits := bitsNeeded(len(values))
	v := Var{
		name:   name,
		kind:   EnumKind,
		index:  len(r.vars),
		offset: r.totalBits,
		bits:   bits,
		domain: len(values),
		labels: values,
		min:    0,
	}
	r.totalBits += bits
	r.vars = append(r.vars, v)
	return v
}

// Int declares a bounded integer state variable.
func (r *Registry) Int(name string, min, max int) Var {
	if max < min {
		panic(fmt.Sprintf("gsm: int %q has max < min", name))
	}
	domain := max - min + 1
	bits := bitsNeeded(domain)
	v := Var{
		name:   name,
		kind:   IntKind,
		index:  len(r.vars),
		offset: r.totalBits,
		bits:   bits,
		domain: domain,
		min:    min,
	}
	r.totalBits += bits
	r.vars = append(r.vars, v)
	return v
}

// InvariantBuilder provides a fluent API for declaring an invariant.
type InvariantBuilder struct {
	r   *Registry
	def invariantDef
}

// Invariant begins declaring a named invariant.
func (r *Registry) Invariant(name string) *InvariantBuilder {
	return &InvariantBuilder{
		r:   r,
		def: invariantDef{name: name},
	}
}

// Watches declares the invariant's footprint — which variables it constrains
// and which its repair may modify.
func (ib *InvariantBuilder) Watches(vars ...Var) *InvariantBuilder {
	for _, v := range vars {
		ib.def.footprint = append(ib.def.footprint, v.index)
	}
	return ib
}

// Holds sets the invariant predicate. Returns true if the invariant holds.
func (ib *InvariantBuilder) Holds(fn CheckFunc) *InvariantBuilder {
	ib.def.check = fn
	return ib
}

// Repair sets the compensation function. Called when Check returns false.
// Must only modify variables declared in Over().
func (ib *InvariantBuilder) Repair(fn EffectFunc) *InvariantBuilder {
	ib.def.repair = fn
	return ib
}

// Add registers the invariant with the registry.
func (ib *InvariantBuilder) Add() {
	if ib.def.check == nil {
		panic(fmt.Sprintf("gsm: invariant %q has no check function", ib.def.name))
	}
	if ib.def.repair == nil {
		panic(fmt.Sprintf("gsm: invariant %q has no repair function", ib.def.name))
	}
	ib.r.invariants = append(ib.r.invariants, ib.def)
}

// EventBuilder provides a fluent API for declaring an event.
type EventBuilder struct {
	r   *Registry
	def eventDef
}

// Event begins declaring a named event.
func (r *Registry) Event(name string) *EventBuilder {
	return &EventBuilder{
		r:   r,
		def: eventDef{name: name},
	}
}

// Writes declares which variables this event modifies.
func (eb *EventBuilder) Writes(vars ...Var) *EventBuilder {
	for _, v := range vars {
		eb.def.writes = append(eb.def.writes, v.index)
	}
	return eb
}

// Guard sets an optional precondition. If the guard returns false,
// the event is a no-op in that state.
func (eb *EventBuilder) Guard(fn CheckFunc) *EventBuilder {
	eb.def.guard = fn
	return eb
}

// Apply sets the event's effect function.
func (eb *EventBuilder) Apply(fn EffectFunc) *EventBuilder {
	eb.def.effect = fn
	return eb
}

// Add registers the event with the registry.
func (eb *EventBuilder) Add() {
	if eb.def.effect == nil {
		panic(fmt.Sprintf("gsm: event %q has no effect function", eb.def.name))
	}
	eb.r.events = append(eb.r.events, eb.def)
}
