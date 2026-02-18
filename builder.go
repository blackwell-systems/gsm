package gsm

import (
	"fmt"
)

// Builder constructs a governed state machine. After declaring variables,
// invariants, and events, call Build() to verify convergence properties
// and produce an immutable Machine.
type Builder struct {
	name         string
	vars         []Var
	invariants   []invariantDef
	events       []eventDef
	totalBits    uint
	independent  [][2]int // pairs of event indices declared independent
	allIndependent bool  // if true, check all pairs
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

// NewBuilder creates a Builder for a named state machine.
// By default, all event pairs are checked for CC. Use Independent()
// to restrict checking to specific pairs, or AllIndependent() to
// explicitly check all pairs.
func NewBuilder(name string) *Builder {
	return &Builder{name: name, allIndependent: true}
}

// Independent declares that two events may arrive in either order
// (they are not causally related). CC will be checked for this pair.
// Call DeclaredIndependence() first to switch from all-pairs mode.
func (b *Builder) Independent(e1name, e2name string) *Builder {
	b.independent = append(b.independent, [2]int{
		b.eventIndex(e1name),
		b.eventIndex(e2name),
	})
	return b
}

// DeclaredIndependence switches CC checking to only declared pairs.
func (b *Builder) DeclaredIndependence() *Builder {
	b.allIndependent = false
	return b
}

func (b *Builder) eventIndex(name string) int {
	for i, ev := range b.events {
		if ev.name == name {
			return i
		}
	}
	panic(fmt.Sprintf("gsm: unknown event %q", name))
}

// Bool declares a boolean state variable.
func (b *Builder) Bool(name string) Var {
	v := Var{
		name:   name,
		kind:   BoolKind,
		index:  len(b.vars),
		offset: b.totalBits,
		bits:   1,
		domain: 2,
		min:    0,
	}
	b.totalBits += 1
	b.vars = append(b.vars, v)
	return v
}

// Enum declares an enumerated state variable.
func (b *Builder) Enum(name string, values ...string) Var {
	if len(values) < 2 {
		panic(fmt.Sprintf("gsm: enum %q needs at least 2 values", name))
	}
	bits := bitsNeeded(len(values))
	v := Var{
		name:   name,
		kind:   EnumKind,
		index:  len(b.vars),
		offset: b.totalBits,
		bits:   bits,
		domain: len(values),
		labels: values,
		min:    0,
	}
	b.totalBits += bits
	b.vars = append(b.vars, v)
	return v
}

// Int declares a bounded integer state variable.
func (b *Builder) Int(name string, min, max int) Var {
	if max < min {
		panic(fmt.Sprintf("gsm: int %q has max < min", name))
	}
	domain := max - min + 1
	bits := bitsNeeded(domain)
	v := Var{
		name:   name,
		kind:   IntKind,
		index:  len(b.vars),
		offset: b.totalBits,
		bits:   bits,
		domain: domain,
		min:    min,
	}
	b.totalBits += bits
	b.vars = append(b.vars, v)
	return v
}

// InvariantBuilder provides a fluent API for declaring an invariant.
type InvariantBuilder struct {
	b   *Builder
	def invariantDef
}

// Invariant begins declaring a named invariant.
func (b *Builder) Invariant(name string) *InvariantBuilder {
	return &InvariantBuilder{
		b:   b,
		def: invariantDef{name: name},
	}
}

// Over declares the invariant's footprint — which variables it constrains
// and which its repair may modify.
func (ib *InvariantBuilder) Over(vars ...Var) *InvariantBuilder {
	for _, v := range vars {
		ib.def.footprint = append(ib.def.footprint, v.index)
	}
	return ib
}

// Check sets the invariant predicate. Returns true if the invariant holds.
func (ib *InvariantBuilder) Check(fn CheckFunc) *InvariantBuilder {
	ib.def.check = fn
	return ib
}

// Repair sets the compensation function. Called when Check returns false.
// Must only modify variables declared in Over().
func (ib *InvariantBuilder) Repair(fn EffectFunc) *InvariantBuilder {
	ib.def.repair = fn
	return ib
}

// Add registers the invariant with the builder.
func (ib *InvariantBuilder) Add() {
	if ib.def.check == nil {
		panic(fmt.Sprintf("gsm: invariant %q has no check function", ib.def.name))
	}
	if ib.def.repair == nil {
		panic(fmt.Sprintf("gsm: invariant %q has no repair function", ib.def.name))
	}
	ib.b.invariants = append(ib.b.invariants, ib.def)
}

// EventBuilder provides a fluent API for declaring an event.
type EventBuilder struct {
	b   *Builder
	def eventDef
}

// Event begins declaring a named event.
func (b *Builder) Event(name string) *EventBuilder {
	return &EventBuilder{
		b:   b,
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

// Add registers the event with the builder.
func (eb *EventBuilder) Add() {
	if eb.def.effect == nil {
		panic(fmt.Sprintf("gsm: event %q has no effect function", eb.def.name))
	}
	eb.b.events = append(eb.b.events, eb.def)
}
