package gsm

// Combinator vocabulary (prototype): express invariants and events as DATA built
// from a fixed, gsm-owned set of combinators, instead of arbitrary Go closures.
// It still reads as Go (`Le(V(a), Lit(3))`, `Set(a, Add(V(a), Lit(1)))`), but the
// result is an expression tree gsm can evaluate AND analyze. Two immediate wins:
//
//   - Footprints are DERIVED from the tree (the variables an expression reads and
//     writes), so combinator rules are footprint-conformant by construction: there
//     is no undeclared read or write to check for, because the declaration IS the
//     set of variables the tree mentions.
//   - Rules become inspectable, serializable data, which is the precondition for a
//     verified verifier (extraction) and for portable, auditable policies.
//
// Values are integers in each variable's logical space: an Int reads/writes via
// GetInt/SetInt (offset-adjusted, clamped), a Bool as 0/1, an Enum as its index.

// Expr is an integer-valued expression over the state.
type Expr interface {
	eval(State) int
	vars() []int
}

// Pred is a boolean-valued predicate over the state.
type Pred interface {
	holds(State) bool
	vars() []int
}

func readVal(s State, v Var) int {
	if v.kind == IntKind {
		return s.GetInt(v)
	}
	return int(s.getRaw(v)) // bool: 0/1, enum: index
}

func writeVal(s State, v Var, val int) State {
	switch v.kind {
	case IntKind:
		return s.SetInt(v, val)
	case BoolKind:
		return s.SetBool(v, val != 0)
	default: // enum: set by index (clamped into range)
		if val < 0 {
			val = 0
		}
		if val > v.domain-1 {
			val = v.domain - 1
		}
		return s.setRaw(v, uint64(val))
	}
}

// ---- expression constructors ----

type varRef struct{ v Var }

func (x varRef) eval(s State) int { return readVal(s, x.v) }
func (x varRef) vars() []int      { return []int{x.v.index} }

// V reads a variable.
func V(v Var) Expr { return varRef{v} }

type litE struct{ n int }

func (x litE) eval(State) int { return x.n }
func (litE) vars() []int      { return nil }

// Lit is an integer constant.
func Lit(n int) Expr { return litE{n} }

type binE struct {
	op   string
	a, b Expr
}

func (x binE) eval(s State) int {
	a, b := x.a.eval(s), x.b.eval(s)
	switch x.op {
	case "+":
		return a + b
	case "-":
		return a - b
	default:
		return 0
	}
}
func (x binE) vars() []int { return append(x.a.vars(), x.b.vars()...) }

// Add and Sub are integer arithmetic.
func Add(a, b Expr) Expr { return binE{"+", a, b} }
func Sub(a, b Expr) Expr { return binE{"-", a, b} }

// ---- predicate constructors ----

type cmpP struct {
	op   string
	a, b Expr
}

func (x cmpP) holds(s State) bool {
	a, b := x.a.eval(s), x.b.eval(s)
	switch x.op {
	case "<=":
		return a <= b
	case "<":
		return a < b
	case "==":
		return a == b
	case ">=":
		return a >= b
	case ">":
		return a > b
	case "!=":
		return a != b
	default:
		return false
	}
}
func (x cmpP) vars() []int { return append(x.a.vars(), x.b.vars()...) }

// Comparisons.
func Le(a, b Expr) Pred { return cmpP{"<=", a, b} }
func Lt(a, b Expr) Pred { return cmpP{"<", a, b} }
func Eq(a, b Expr) Pred { return cmpP{"==", a, b} }
func Ge(a, b Expr) Pred { return cmpP{">=", a, b} }
func Gt(a, b Expr) Pred { return cmpP{">", a, b} }
func Ne(a, b Expr) Pred { return cmpP{"!=", a, b} }

type boolP struct {
	op string
	ps []Pred
}

func (x boolP) holds(s State) bool {
	switch x.op {
	case "and":
		for _, p := range x.ps {
			if !p.holds(s) {
				return false
			}
		}
		return true
	case "or":
		for _, p := range x.ps {
			if p.holds(s) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
func (x boolP) vars() []int {
	var out []int
	for _, p := range x.ps {
		out = append(out, p.vars()...)
	}
	return out
}

// And / Or over predicates.
func And(ps ...Pred) Pred { return boolP{"and", ps} }
func Or(ps ...Pred) Pred  { return boolP{"or", ps} }

type notP struct{ p Pred }

func (x notP) holds(s State) bool { return !x.p.holds(s) }
func (x notP) vars() []int        { return x.p.vars() }

// Not negates a predicate.
func Not(p Pred) Pred { return notP{p} }

// ---- transforms (assignments) ----

// Assign sets a variable to an expression's value.
type Assign struct {
	v Var
	e Expr
}

// Set builds one assignment.
func Set(v Var, e Expr) Assign { return Assign{v, e} }

// Transform is a sequence of assignments applied left to right.
type Transform []Assign

// Do groups assignments into a transform.
func Do(as ...Assign) Transform { return as }

func (t Transform) apply(s State) State {
	for _, a := range t {
		s = writeVal(s, a.v, a.e.eval(s))
	}
	return s
}

// writeVars are the variables a transform assigns to.
func (t Transform) writeVars() []int {
	var out []int
	for _, a := range t {
		out = append(out, a.v.index)
	}
	return out
}

// readVars are the variables a transform reads (in the assigned expressions).
func (t Transform) readVars() []int {
	var out []int
	for _, a := range t {
		out = append(out, a.e.vars()...)
	}
	return out
}

func dedup(idx ...[]int) []int {
	seen := map[int]bool{}
	var out []int
	for _, xs := range idx {
		for _, i := range xs {
			if !seen[i] {
				seen[i] = true
				out = append(out, i)
			}
		}
	}
	return out
}

// ---- registry builders (combinator surface) ----

// DeclInvariant declares an invariant from combinators: it holds when `holds` is
// true, and `repair` restores it. The footprint is derived from the variables the
// predicate and transform mention, so it is conformant by construction.
func (r *Registry) DeclInvariant(name string, holds Pred, repair Transform) {
	fp := dedup(holds.vars(), repair.readVars(), repair.writeVars())
	r.invariants = append(r.invariants, invariantDef{
		name:      name,
		footprint: fp,
		check:     func(s State) bool { return holds.holds(s) },
		repair:    func(s State) State { return repair.apply(s) },
		predAST:   holds,
		repairAST: repair,
	})
}

// DeclEvent declares an event from a combinator transform. Its write set is
// derived from the assignments, so Writes need not be declared separately.
func (r *Registry) DeclEvent(name string, effect Transform) {
	r.events = append(r.events, eventDef{
		name:      name,
		writes:    effect.writeVars(),
		effect:    func(s State) State { return effect.apply(s) },
		effectAST: effect,
	})
}

// DeclEventGuarded is DeclEvent with a precondition; the event is a no-op when the
// guard is false.
func (r *Registry) DeclEventGuarded(name string, guard Pred, effect Transform) {
	r.events = append(r.events, eventDef{
		name:      name,
		writes:    effect.writeVars(),
		guard:     func(s State) bool { return guard.holds(s) },
		effect:    func(s State) State { return effect.apply(s) },
		effectAST: effect,
		guardAST:  guard,
	})
}
