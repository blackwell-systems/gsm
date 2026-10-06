package gsm

import (
	"fmt"
	"math"
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
	abs            *absDecl // non-nil once Abstract is called: Build verifies by abstraction
}

// CheckFunc is a predicate over State.
type CheckFunc func(State) bool

// EffectFunc transforms a State. Used as an event effect (EventBuilder.Apply) or an
// invariant repair (InvariantBuilder.Repair), it must return a state of the machine it
// belongs to: the same variable schema as its input (a State from another machine with an
// identical variable declaration list qualifies) and every variable within its declared
// range. Deriving the result from the input with Set, SetBool and SetInt always satisfies
// this. Build rejects a rule that returns anything else, and a lazy machine's Apply panics
// (see Machine.Apply).
type EffectFunc func(State) State

type invariantDef struct {
	name      string
	footprint []int // indices into vars
	check     CheckFunc
	repair    EffectFunc
	// When declared via the combinator vocabulary (DeclInvariant), the predicate
	// and repair are retained as inspectable AST (nil for closure-based rules).
	// This is what lets a machine be serialized and re-verified from its rules.
	predAST   Pred
	repairAST Transform
	// foreign names a variable passed to Watches that is not a variable of this
	// registry; checkNames reports it (every build path calls checkNames).
	foreign string
}

type eventDef struct {
	name   string
	writes []int // indices into vars
	guard  CheckFunc
	effect EffectFunc
	// Combinator AST of the effect and (optional) guard, retained by DeclEvent /
	// DeclEventGuarded (nil for closures).
	effectAST Transform
	guardAST  Pred
	// foreign names a variable passed to Writes that is not a variable of this
	// registry; checkNames reports it (every build path calls checkNames).
	foreign string
}

// NewRegistry creates a Registry for a named state machine.
// By default, all event pairs are checked for CC. Use Independent()
// to restrict checking to specific pairs.
func NewRegistry(name string) *Registry {
	return &Registry{name: name, allIndependent: true}
}

// Independent declares that two events may arrive in either order at a replica
// (they are concurrent, not causally related). Compensation Commutativity (CC) is
// certified for every declared pair: Build fails if a declared pair does not commute.
//
// The first call switches the registry to declared-only mode: only declared pairs
// are required to commute, so Build accepts a machine in which some undeclared pair
// does not. That is sound only if every such pair is delivered in causal order: the
// two events reach every replica in the same fixed order (the convergence theorem
// for declared-only mode, normalization-confluence coq/Trace.v run_tequiv and
// coq/CausalReplay.v causal_tequiv, exempts a pair from CC only when it is never
// reordered). gsm cannot see delivery order, so Build still checks every undeclared
// pair on its step tables and lists each one that does not commute in
// Report.CausalOrderRequired, with a witness state; the report's convergence line
// says convergence holds under causal delivery of those pairs. An empty
// CausalOrderRequired means every pair commutes and the declarations cost nothing.
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
// compatibility. Every undeclared pair must then be delivered in causal order; see
// Independent and Report.CausalOrderRequired.
func (r *Registry) OnlyDeclaredPairs() *Registry {
	r.allIndependent = false
	return r
}

// checkNames rejects a registry in which two events, two variables, or two labels of
// one enum share a name. An event is addressed by name everywhere after declaration (Independent
// resolves the first match, a built Machine's Apply the last, and replay logs and
// federations hold (registry, event) strings), so a duplicate would let the CC
// check cover one event while the runtime applies another. A variable is
// addressed by name in certificate tables, input ports, and shared projections,
// where a duplicate would let a re-check or a merge act on the wrong variable. An
// enum label is addressed by name in Set, TrySet and the label sugar, which resolve a
// repeated label to its first index, so an enum may not repeat one either.
// Every path that produces a machine, a certificate, or an export for the
// checkers calls it, except Synthesis.Machine, which builds from the snapshot
// SynthesizeWith takes when it returns. Build, BuildCompositional and
// SynthesizeWith also reject a registry that a rule closure changed after the
// check (checkUnchanged), so the registry they build from has the same declarations
// as the one checked.
func (r *Registry) checkNames() error {
	if err := r.checkDeclaredVars(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(r.events))
	for _, ev := range r.events {
		if seen[ev.name] {
			return fmt.Errorf("gsm: registry %q: duplicate event name %q", r.name, ev.name)
		}
		seen[ev.name] = true
	}
	seenVar := make(map[string]bool, len(r.vars))
	for _, v := range r.vars {
		if seenVar[v.name] {
			return fmt.Errorf("gsm: registry %q: duplicate variable name %q", r.name, v.name)
		}
		seenVar[v.name] = true
		seenLabel := make(map[string]bool, len(v.labels))
		for _, l := range v.labels {
			if seenLabel[l] {
				return fmt.Errorf("gsm: registry %q: enum %q has duplicate label %q", r.name, v.name, l)
			}
			seenLabel[l] = true
		}
	}
	return nil
}

// checkDeclaredVars rejects an event or invariant that names a variable of another
// registry in Writes or Watches (or whose combinator rule mentions one at a position
// this registry does not have). Such a Var carries an index into the other registry's
// variable list, which BuildCompositional would otherwise use as an index into this one.
func (r *Registry) checkDeclaredVars() error {
	for _, ev := range r.events {
		if ev.foreign != "" {
			return fmt.Errorf("gsm: registry %q: event %q Writes variable %q, which is not a variable of this "+
				"registry (was it declared on another registry?)", r.name, ev.name, ev.foreign)
		}
		for _, i := range ev.writes {
			if i < 0 || i >= len(r.vars) {
				return fmt.Errorf("gsm: registry %q: event %q writes a variable that is not a variable of this "+
					"registry (index %d; was it declared on another registry?)", r.name, ev.name, i)
			}
		}
	}
	for _, inv := range r.invariants {
		if inv.foreign != "" {
			return fmt.Errorf("gsm: registry %q: invariant %q Watches variable %q, which is not a variable of "+
				"this registry (was it declared on another registry?)", r.name, inv.name, inv.foreign)
		}
		for _, i := range inv.footprint {
			if i < 0 || i >= len(r.vars) {
				return fmt.Errorf("gsm: registry %q: invariant %q watches a variable that is not a variable of "+
					"this registry (index %d; was it declared on another registry?)", r.name, inv.name, i)
			}
		}
	}
	return nil
}

// owns reports whether v is a variable of this registry: the variable declared at
// v's position has v's name, kind, layout and domain.
func (r *Registry) owns(v Var) bool {
	return v.index >= 0 && v.index < len(r.vars) && sameVar(r.vars[v.index], v)
}

// registryShape is what a rule closure could change by declaring on its own
// registry while that registry is being verified. Declarations only append, so
// the counts identify the registry that was checked.
type registryShape struct {
	vars, invariants, events, independent int
	allIndependent                        bool
}

func (r *Registry) shape() registryShape {
	return registryShape{len(r.vars), len(r.invariants), len(r.events), len(r.independent), r.allIndependent}
}

// checkUnchanged rejects a registry that changed since before was taken. Build,
// BuildCompositional and SynthesizeWith run rule closures between checkNames and
// building their result; a closure that declares on the registry would otherwise
// get a declaration past the name check, or into a result that never checked it.
func (r *Registry) checkUnchanged(before registryShape) error {
	if r.shape() != before {
		return fmt.Errorf("gsm: registry %q was changed while it was being verified (a rule declared "+
			"a variable, invariant, event, or Independent pair)", r.name)
	}
	return nil
}

func (r *Registry) eventIndex(name string) int {
	for i, ev := range r.events {
		if ev.name == name {
			return i
		}
	}
	panic(fmt.Sprintf("gsm: unknown event %q", name))
}

// Bool declares a boolean state variable. Variable names must be unique within
// the registry (see Event); Build rejects a duplicate.
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

// Enum declares an enumerated state variable. Its name must be unique within the
// registry, and its labels within the enum; Build rejects a repeat of either.
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
		labels: append([]string(nil), values...), // the caller's slice stays theirs
		min:    0,
	}
	r.totalBits += bits
	r.vars = append(r.vars, v)
	return v
}

// Int declares a bounded integer state variable. Its name must be unique within
// the registry. It panics if max <= min, or if the range has more values than an int
// can count (for example Int(0, math.MaxInt)).
//
// Writes through SetInt, Inc, Dec, IncBy, DecBy and combinator Set saturate at the
// bounds: a value past max is stored as max, silently. Build reports each rule that
// saturates (Report.Saturations), because an invariant written to catch the overflow
// (bal <= max || overdraft) never sees it. Size the range past any bound an invariant
// tests.
func (r *Registry) Int(name string, min, max int) Var {
	if max < min {
		panic(fmt.Sprintf("gsm: int %q has max < min", name))
	}
	if max == min {
		panic(fmt.Sprintf("gsm: int %q needs max > min; a single-value range (%d..%d) is a degenerate "+
			"variable with no states to range over", name, min, max))
	}
	// The domain size max-min+1 must be an int. Int(0, math.MaxInt) or a range
	// spanning both signs widely overflows it, which used to pass the Go checks and
	// fail later, far from the declaration.
	if span := max - min; span < 0 || span == math.MaxInt {
		panic(fmt.Sprintf("gsm: int %q range %d..%d is too wide: it has more than math.MaxInt values. "+
			"A gsm variable enumerates its domain, so declare the range the machine needs", name, min, max))
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

// Invariant begins declaring a named invariant. The name labels diagnostics only;
// invariants are addressed by declaration order, so it need not be unique.
func (r *Registry) Invariant(name string) *InvariantBuilder {
	return &InvariantBuilder{
		r:   r,
		def: invariantDef{name: name},
	}
}

// Watches declares the invariant's footprint: which variables it constrains
// and which its repair may modify. Each must be a variable of this registry; a
// variable of another registry makes every build path return an error naming it.
func (ib *InvariantBuilder) Watches(vars ...Var) *InvariantBuilder {
	for _, v := range vars {
		if !ib.r.owns(v) {
			if ib.def.foreign == "" {
				ib.def.foreign = v.name
			}
			continue
		}
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
// Must only modify variables declared in Over(), and must return a state of this
// machine (see EffectFunc).
func (ib *InvariantBuilder) Repair(fn EffectFunc) *InvariantBuilder {
	ib.def.repair = fn
	return ib
}

// Add registers the invariant with the registry. A Repair is required for Build (which
// verifies a given compensation), but may be omitted when the registry is passed to
// Synthesize, which generates a convergent compensation from the validity predicates alone.
func (ib *InvariantBuilder) Add() {
	if ib.def.check == nil {
		panic(fmt.Sprintf("gsm: invariant %q has no check function", ib.def.name))
	}
	ib.r.invariants = append(ib.r.invariants, ib.def)
}

// EventBuilder provides a fluent API for declaring an event.
type EventBuilder struct {
	r   *Registry
	def eventDef
}

// Event begins declaring a named event. The name is how the event is addressed
// afterwards (Machine.Apply, Independent, FedMachine.ApplyNamed, replay logs), so
// it must be unique within the registry: Build, BuildCompositional, Synthesize,
// the federation and certificate paths, and the rules exports reject a registry
// that declares two events with the same name. The duplicate is reported by
// those paths as an error, not by Add.
func (r *Registry) Event(name string) *EventBuilder {
	return &EventBuilder{
		r:   r,
		def: eventDef{name: name},
	}
}

// Writes declares which variables this event modifies. Each must be a variable of
// this registry; a variable of another registry makes every build path return an
// error naming it.
func (eb *EventBuilder) Writes(vars ...Var) *EventBuilder {
	for _, v := range vars {
		if !eb.r.owns(v) {
			if eb.def.foreign == "" {
				eb.def.foreign = v.name
			}
			continue
		}
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

// Apply sets the event's effect function. It must return a state of this machine (see
// EffectFunc); Build rejects the event otherwise.
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
