package gsm

// Ergonomic surface over the combinator primitives. Everything here LOWERS to the
// same Expr/Pred/Transform data the analyzable core is built from (combinator.go),
// so it adds no new semantics and nothing the verified oracle has to learn: a rule
// written with this sugar serializes (WriteMachineAST) to exactly the AST a rule
// written with the raw combinators would. The friendly layer is for humans; the
// primitive layer is what we test, serialize, and hand to the machine-checked
// checker. Keep them separate on purpose: sugar may grow freely as long as it only
// ever desugars to primitives.

import "fmt"

// ---- predicate sugar (reads as a sentence, lowers to a combinator Pred) ----

// AtMost: v <= n.
func AtMost(v Var, n int) Pred { return Le(V(v), Lit(n)) }

// AtLeast: v >= n.
func AtLeast(v Var, n int) Pred { return Ge(V(v), Lit(n)) }

// Below: v < n.
func Below(v Var, n int) Pred { return Lt(V(v), Lit(n)) }

// Above: v > n.
func Above(v Var, n int) Pred { return Gt(V(v), Lit(n)) }

// Is: v == n (for a Bool, n is 0 or 1).
func Is(v Var, n int) Pred { return Eq(V(v), Lit(n)) }

// IsNot: v != n.
func IsNot(v Var, n int) Pred { return Ne(V(v), Lit(n)) }

// InRange: lo <= v <= hi.
func InRange(v Var, lo, hi int) Pred { return And(AtLeast(v, lo), AtMost(v, hi)) }

// Between is a synonym for InRange (lo <= v <= hi).
func Between(v Var, lo, hi int) Pred { return InRange(v, lo, hi) }

// ---- two-variable relations (lower to a combinator Pred over both vars) ----

// AtMostVar: a <= b.
func AtMostVar(a, b Var) Pred { return Le(V(a), V(b)) }

// AtLeastVar: a >= b.
func AtLeastVar(a, b Var) Pred { return Ge(V(a), V(b)) }

// BelowVar: a < b.
func BelowVar(a, b Var) Pred { return Lt(V(a), V(b)) }

// AboveVar: a > b.
func AboveVar(a, b Var) Pred { return Gt(V(a), V(b)) }

// SameAs: a == b.
func SameAs(a, b Var) Pred { return Eq(V(a), V(b)) }

// DiffersFrom: a != b.
func DiffersFrom(a, b Var) Pred { return Ne(V(a), V(b)) }

// ---- enum-by-label ergonomics (resolve a label to its index, lower to numbers) ----

// mustLabel resolves an enum value's label to its integer index, panicking with a
// clear message on a typo or a non-enum variable. Rule construction is programmer
// code, so a bad label literal is a programming error surfaced immediately, and the
// resulting AST carries only the numeric index the analyzable core understands.
func mustLabel(v Var, label string) int {
	if v.kind != EnumKind {
		panic(fmt.Sprintf("gsm: %s(%q): variable %q is not an enum", "label helper", label, v.name))
	}
	i, err := v.enumIndex(label)
	if err != nil {
		panic("gsm: " + err.Error())
	}
	return i
}

// IsLabel: enum v equals the value named label.
func IsLabel(v Var, label string) Pred { return Eq(V(v), Lit(mustLabel(v, label))) }

// IsNotLabel: enum v is not the value named label.
func IsNotLabel(v Var, label string) Pred { return Ne(V(v), Lit(mustLabel(v, label))) }

// SetLabel assigns enum v to the value named label.
func SetLabel(v Var, label string) Transform { return Do(Set(v, Lit(mustLabel(v, label)))) }

// ---- transform sugar (lowers to a combinator Transform) ----

// SetTo assigns v := n.
func SetTo(v Var, n int) Transform { return Do(Set(v, Lit(n))) }

// Inc: v := v + 1 (clamped into v's domain).
func Inc(v Var) Transform { return Do(Set(v, Add(V(v), Lit(1)))) }

// Dec: v := v - 1 (nat-truncated at 0).
func Dec(v Var) Transform { return Do(Set(v, Sub(V(v), Lit(1)))) }

// IncBy: v := v + n.
func IncBy(v Var, n int) Transform { return Do(Set(v, Add(V(v), Lit(n)))) }

// DecBy: v := v - n.
func DecBy(v Var, n int) Transform { return Do(Set(v, Sub(V(v), Lit(n)))) }

// Raise sets a Bool true (v := 1).
func Raise(v Var) Transform { return Do(Set(v, Lit(1))) }

// Lower sets a Bool false (v := 0).
func Lower(v Var) Transform { return Do(Set(v, Lit(0))) }

// Toggle flips a Bool: v := 1 - v.
func Toggle(v Var) Transform { return Do(Set(v, Sub(Lit(1), V(v)))) }

// Copy assigns dst := src.
func Copy(dst, src Var) Transform { return Do(Set(dst, V(src))) }

// ---- fluent rule/event builders (lower to DeclInvariant/DeclEvent) ----

// RuleBuilder accumulates an invariant declared in fluent style.
type RuleBuilder struct {
	r    *Registry
	name string
	when Pred
	fix  Transform
}

// Rule begins a fluent invariant: r.Rule("cap").Require(AtMost(a,3)).RepairWith(SetTo(a,3)).Add().
func (r *Registry) Rule(name string) *RuleBuilder { return &RuleBuilder{r: r, name: name} }

// Require sets the predicate that must hold.
func (b *RuleBuilder) Require(p Pred) *RuleBuilder { b.when = p; return b }

// RepairWith sets the compensating transform.
func (b *RuleBuilder) RepairWith(t Transform) *RuleBuilder { b.fix = t; return b }

// Add lowers the rule to a combinator invariant on the registry.
func (b *RuleBuilder) Add() { b.r.DeclInvariant(b.name, b.when, b.fix) }

// OnBuilder accumulates an event declared in fluent style. (Named OnBuilder to sit
// alongside the closure-based EventBuilder, which Registry.Event returns.)
type OnBuilder struct {
	r     *Registry
	name  string
	eff   Transform
	guard Pred
}

// On begins a fluent event: r.On("inc_a").Does(Inc(a)).Add().
func (r *Registry) On(name string) *OnBuilder { return &OnBuilder{r: r, name: name} }

// Does sets the event's effect transform.
func (b *OnBuilder) Does(t Transform) *OnBuilder { b.eff = t; return b }

// OnlyIf gates the event on a precondition (a no-op when false).
func (b *OnBuilder) OnlyIf(p Pred) *OnBuilder { b.guard = p; return b }

// Add lowers the event to a combinator event on the registry.
func (b *OnBuilder) Add() {
	if b.guard == nil {
		b.r.DeclEvent(b.name, b.eff)
		return
	}
	b.r.DeclEventGuarded(b.name, b.guard, b.eff)
}
