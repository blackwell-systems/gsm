package gsm

import (
	"fmt"
)

// Collection declares a keyed collection of identically governed items: one item
// registry, the template, governs the item at every key of type K, and an event
// addressed to a key acts on that key's item only. Build verifies the template alone,
// and by symmetry the result holds for a collection of any number of keys
// (normalization-confluence coq/SymmetryCutoff.v; docs/theory.md §11.8).
//
// Independence holds by construction (lift_idgov). The template's rules are written
// over one item's State, which carries neither a key nor any other item, so no rule can
// read or write another key, and every key runs the same rules. In particular an
// aggregate across keys ("total reserved over all products is at most capacity") cannot
// be written through a Collection: it relates items, and a rule that relates items can
// make a collection diverge even though one item converges (aggregate_diverges). Model
// such a rule in one registry whose state holds the items it relates; Build then checks
// it directly.
//
// The collection machine runs the template's Build tables, not its closures: a rule
// runs only while Build verifies the template. A closure that reads data outside its
// State (a captured variable, a global) is evaluated then, on the template's states, and
// what it computed is frozen into the tables the table oracle certifies. It cannot
// observe other keys at run time, because it does not run at run time.
type Collection[K comparable] struct {
	over     string
	template *Registry
}

// NewCollection declares a collection over keys of type K, each key's item governed by
// template. over names the key ("ProductID"); the report reads "Verified by symmetry over
// <over>". Nothing is checked until Build.
func NewCollection[K comparable](over string, template *Registry) *Collection[K] {
	return &Collection[K]{over: over, template: template}
}

// Build verifies the template with Registry.Build, so the template gets every check
// and the oracle gate a single registry gets, and returns a machine that runs the
// template at every key. On success the report is the template's, with Report.Symmetry
// set. If the template does not build, Build returns no machine, the template's report
// (with its counterexample, such as Report.CCFailure) and the template's error, wrapped.
//
// What the template's report says holds at every key, for any number of keys:
// convergence of the events addressed to one key (run_proj, alo_cutoff); NotIdempotent,
// per key (idem_reduces, alo_cutoff_exact); and CausalOrderRequired, for a listed pair on
// the same key only, since events on different keys always commute (cross_commute,
// declared_cutoff). Every condition has cutoff 1: one item is the whole check.
func (c *Collection[K]) Build() (*CollectionMachine[K], *Report, error) {
	if c.over == "" {
		return nil, nil, fmt.Errorf("gsm: collection: empty key name (NewCollection's over names what the items are keyed by)")
	}
	if c.template == nil {
		return nil, nil, fmt.Errorf("gsm: collection over %s: nil template registry", c.over)
	}
	item, rep, err := c.template.Build()
	if err != nil {
		return nil, rep, fmt.Errorf("gsm: collection over %s: template %q did not build: %w", c.over, c.template.name, err)
	}
	rep.Symmetry = &SymmetryReduction{Over: c.over, Cutoff: 1}
	return &CollectionMachine[K]{over: c.over, item: item}, rep, nil
}

// SymmetryReduction records, in Report.Symmetry, that a machine was verified as the
// template of a Collection: on one item, with the result holding for every key of a
// collection of any size.
type SymmetryReduction struct {
	// Over names the key the collection is over (NewCollection's over).
	Over string
	// Cutoff is the number of items the check needs: 1 for every condition Build reports
	// (see Collection.Build).
	Cutoff int
}

func (s SymmetryReduction) String() string {
	return fmt.Sprintf("Verified by symmetry over %s (items independent; cutoff %d)", s.Over, s.Cutoff)
}

// CollectionMachine is a verified collection: the template's Machine, run at every key.
type CollectionMachine[K comparable] struct {
	over string
	item *Machine
}

// Over returns the key name the collection was declared over.
func (c *CollectionMachine[K]) Over() string { return c.over }

// Item returns the template's machine, which every key runs. Its NewState is the state
// of a key no event has reached, and its Events are the events Apply accepts.
func (c *CollectionMachine[K]) Item() *Machine { return c.item }

// NewState returns an empty collection: every key is at the template's NewState until an
// event reaches it.
func (c *CollectionMachine[K]) NewState() *CollectionState[K] {
	return &CollectionState[K]{owner: c, items: make(map[K]State)}
}

// Apply applies event to the item at key and returns the item's new state: the template
// machine's Apply on the key's current item (its NewState if no event has reached the
// key). No other key changes.
//
// Apply updates s in place. A Machine's State is a value, but a collection holds one item
// per key, so it is updated rather than copied on every event; use Clone for a copy. A
// CollectionState is not safe for concurrent use.
//
// It panics if event is not an event of the template, or if s is nil or was made by
// another CollectionMachine (its items would be states of another machine, which a table
// machine does not detect).
func (c *CollectionMachine[K]) Apply(s *CollectionState[K], key K, event string) State {
	if s == nil {
		panic(fmt.Sprintf("gsm: collection over %s: Apply on a nil CollectionState", c.over))
	}
	if s.owner != c {
		panic(fmt.Sprintf("gsm: collection over %s: Apply on a CollectionState of another collection machine", c.over))
	}
	next := c.item.Apply(s.Item(key), event)
	s.items[key] = next
	return next
}

// CollectionState is the state of a collection: the item state of each key an event has
// reached. Make one with CollectionMachine.NewState.
type CollectionState[K comparable] struct {
	owner *CollectionMachine[K]
	items map[K]State
}

// Item returns the state of the item at key: the template's NewState if no event has
// reached key.
func (s *CollectionState[K]) Item(key K) State {
	if it, ok := s.items[key]; ok {
		return it
	}
	return s.owner.item.NewState()
}

// Keys returns the keys an event has reached, in no particular order.
func (s *CollectionState[K]) Keys() []K {
	out := make([]K, 0, len(s.items))
	for k := range s.items {
		out = append(out, k)
	}
	return out
}

// Len returns the number of keys an event has reached.
func (s *CollectionState[K]) Len() int { return len(s.items) }

// Clone returns an independent copy of s, for the same collection machine.
func (s *CollectionState[K]) Clone() *CollectionState[K] {
	items := make(map[K]State, len(s.items))
	for k, it := range s.items {
		items[k] = it
	}
	return &CollectionState[K]{owner: s.owner, items: items}
}
