package gsm

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// Abstraction: check relationships between values, not the values (roadmap item 1b).
//
// The theory is normalization-confluence coq/AbstractionCutoff.v. A registry whose
// rules only compare and copy integer values, against each other and a finite set C
// of declared constants (the order-invariant fragment, ord_frag), cannot tell apart
// two states with the same order pattern relative to C. So a condition holds for
// every integer value iff it holds over a small representative domain, reps N C: C,
// the N integers above each constant, and the N below the least one (0 .. N-1 when C
// is empty), at most |C|(N+1)+N values. N depends on the condition: n for repair
// termination (term_abs, wfc_abs) and n + 2m for CC1 (cc1_abs), where n is the
// number of variables and m the number of event parameters (the largest number one event
// declares, OnBuilder.Param). Build takes N = n + 2m for every condition it checks, which is
// at least each condition's cutoff: one domain serves them all. A parameterized event is
// checked at every assignment of representative values to its parameters, which are
// compared and copied like variables (normalization-confluence evaluates an event's rules
// over the state followed by its parameters, AbstractionCutoff.sap). The model's parameters
// range over every integer, so the check runs each such event with its range test conjoined
// to its guard and its bounds among the constants: a registry of the model that does
// nothing outside the ranges and is the declared event inside them.
//
// What Build checks, and the theorem per condition (docs/theory.md §11.9):
//   - WFC, from every representative state; repair reaches validity within K steps
//     (K the deepest chain). term_abs: then within K steps from every integer state.
//   - CC1 for the checked pairs at every valid representative state: gsm's own CC1
//     (valid states, docs/theory.md §6.5). cc1_valid_abs (normalization-confluence
//     coq/AbstractionGsm.v): it holds over every integer iff at the valid
//     representatives; with repair within K steps, gsm_abs_exact makes it exactly the
//     condition for every reordering of independent events from a valid state to reach
//     the same state, and gsm_abs_sound (gsm_abs_sound_all) gives that from every
//     integer state for the runtime, which normalizes its input first.
//   - Idempotence at every valid representative state: idem_valid_abs (runtime form
//     idem_runtime_abs) makes NotIdempotent exact for every value.
//
// The repair-first registry stays in the fragment (derived_ordinv) and its CC1 at every
// state is gsm's CC1 at the valid states (cc1_derived_valid), so cc1_abs applies to it
// (cc1_valid_derived_abs).
//
// The fragment check is syntactic, over the combinator trees, so closure rules are
// refused: gsm cannot inspect them. An undeclared literal is refused
// (exact13_diverges) and so is arithmetic (triangle_diverges).

// absDecl is what Registry.Abstract declared.
type absDecl struct {
	constants []int
}

// Abstract declares that Build verifies the registry by abstraction: instead of
// enumerating every value of every variable, Build checks the rules over a small
// representative domain, and the result holds for every value in the declared ranges.
// constants are the declared constants: every literal a rule compares or copies must be
// one of them. Each call adds to the constants already declared.
//
// Events with parameters (OnBuilder.Param) are checked over representative parameter
// values, with the result holding for every value of every parameter: a parameter may be
// compared and copied, against variables, other parameters and the declared constants, but
// not added or subtracted, and a copy of it into a variable must keep its value in range
// (the parameter's range lies inside the variable's). Each parameter's bounds join the
// constants, since the check tests the range against them. The ranges of the parameters may be as
// wide as an Int allows: Build does not enumerate them, and a family too wide for Build
// without Abstract (more than 1024 instances) is accepted here. The cutoff is N = n + 2m
// for m the most parameters one event declares (cc1_valid_abs), and the check visits every
// pair of representative instances. Independent pairs name an event with parameters as a
// whole; an Independent declaration that names one instance is refused.
//
// It applies when the registry is in the comparison fragment, and Build refuses the
// registry otherwise, returning an *AbstractionError that names the rule and the reason:
//   - every variable is an Int (the theorem is about integer variables);
//   - every event and invariant is a combinator rule (DeclEvent, DeclEventGuarded,
//     DeclInvariant, or the On and Rule sugar). A Go closure cannot be inspected, so gsm
//     cannot show it only compares and copies;
//   - every expression is a variable or a declared constant: no Add or Sub, and no
//     literal that is not declared. Comparisons (Le, Lt, Eq, Ge, Gt, Ne) and And, Or and
//     Not are free;
//   - every assignment keeps its value in range: a copied variable's range lies inside
//     the target's, and a written constant inside the target's range. So a write never
//     saturates, and the rules compute exactly what they compute over the integers.
//
// The Int ranges may be as wide as an Int allows (up to 63 bits each), as long as the
// variables fit one 64-bit state together: Build does not enumerate them. It enumerates
// the representative states instead, len(reps)^n of them for n variables, at most 2²⁰.
// The machine Build returns computes at run time, from the rules, rather than by table
// lookup. Report.Abstraction records the reduction.
func (r *Registry) Abstract(constants ...int) *Registry {
	if r.abs == nil {
		r.abs = &absDecl{}
	}
	r.abs.constants = append(r.abs.constants, constants...)
	return r
}

// AbstractionReduction records, in Report.Abstraction, that Build verified the machine by
// abstraction (Registry.Abstract): over the representative values, with the result holding
// for every value of every variable in its declared range.
type AbstractionReduction struct {
	// Over names the variables, in declaration order. Every variable is abstracted.
	Over []string
	// Constants are the constants of the check, ascending and without repeats: the declared
	// constants, and the bounds of every event parameter (the check tests each parameter's
	// range against them).
	Constants []int
	// Cutoff is N, the number of representatives taken above each constant and below the
	// least: n + 2m, for n variables and m the most parameters one event declares.
	Cutoff int
	// Params is m, the most parameters one event declares (0 when no event has any).
	Params int
	// Representatives are the distinct values of reps N C, ascending.
	Representatives []int
	// States is the number of representative states checked: len(Representatives) to the
	// power len(Over).
	States int
}

func (a AbstractionReduction) String() string {
	consts := "no declared constants"
	if len(a.Constants) > 0 {
		cs := make([]string, len(a.Constants))
		for i, c := range a.Constants {
			cs[i] = fmt.Sprint(c)
		}
		consts = "constants {" + strings.Join(cs, ", ") + "}"
	}
	params := ""
	if a.Params > 0 {
		params = fmt.Sprintf("; event parameters over the same representatives, up to %d per event", a.Params)
	}
	return fmt.Sprintf("Verified by abstraction over %s (rules compare values only; %s; %d representatives%s)",
		strings.Join(a.Over, ", "), consts, len(a.Representatives), params)
}

// AbstractWitness is the witness of a CC failure Build found by abstraction, as integer
// values of the variables (in declaration order): the state and the two results. A
// representative can lie outside a variable's declared range (one below the least constant,
// say); then InRange is false and the CCFailure's State, Result1 and Result2 are zero, since
// a State cannot hold the values. A failure outside the ranges is a failure over the
// integers, so the abstraction cannot certify the machine, but the machine as declared may
// still converge (for ranges small enough, ordinary Build without Abstract decides it).
type AbstractWitness struct {
	Vars                    []string
	State, Result1, Result2 []int
	InRange                 bool
}

func (w *AbstractWitness) values(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%s=%d", w.Vars[i], v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// AbstractionError is Build's error when a registry declared with Abstract cannot be
// verified by abstraction: a rule outside the comparison fragment, a variable that is not an
// Int, or a domain that does not fit. Rule names what is refused (`event "pay"`,
// `invariant "cap"`, `variable "flag"`), or is empty for the registry as a whole.
type AbstractionError struct {
	Registry string
	Rule     string
	Reason   string
}

func (e *AbstractionError) Error() string {
	if e.Rule == "" {
		return fmt.Sprintf("gsm: registry %q: abstraction refused: %s", e.Registry, e.Reason)
	}
	return fmt.Sprintf("gsm: registry %q: abstraction refused: %s %s", e.Registry, e.Rule, e.Reason)
}

const (
	whyExact13 = "Over all integers a test against an undeclared literal can diverge where the " +
		"representatives agree (normalization-confluence exact13_diverges)"
	whyTriangle = "An order-pattern check can pass on a rule that adds and the machine still diverge " +
		"(normalization-confluence triangle_diverges: the guard x < y < z < x + y passes over 0, 1, 2 and " +
		"diverges at (2, 3, 4)). Verify it with Build without Abstract over a bounded range; the solver route " +
		"for linear rules is on the roadmap"
	whyClosure = "is a Go closure: gsm cannot inspect it, so it cannot show that the rule only compares and " +
		"copies values, the fragment the abstraction theorem covers (normalization-confluence ord_frag_sound). " +
		"Declare it with combinators (DeclEvent, DeclEventGuarded, DeclInvariant, On, Rule)"
)

// absFragment checks that r is in the comparison fragment over the constants consts, and
// that no write can saturate. It returns nil or the refusal.
func (r *Registry) absFragment(consts map[int]bool) *AbstractionError {
	refuse := func(rule, reason string) *AbstractionError {
		return &AbstractionError{Registry: r.name, Rule: rule, Reason: reason}
	}
	for _, v := range r.vars {
		if v.kind != IntKind {
			kind := "a Bool"
			if v.kind == EnumKind {
				kind = "an Enum"
			}
			return refuse(fmt.Sprintf("variable %q", v.name), "is "+kind+": the abstraction theorem is about integer "+
				"variables only. Declare it as an Int, with its values among the declared constants")
		}
	}
	for _, ev := range r.events {
		if ev.family > 0 {
			continue // an instance: its family is checked below, with Arg in place of the values
		}
		rule := fmt.Sprintf("event %q", ev.name)
		if !ev.syntactic() {
			return refuse(rule, whyClosure)
		}
		if ev.guardAST != nil {
			if why := r.absPred(ev.guardAST, consts, nil); why != "" {
				return refuse(rule, "guard "+why)
			}
		}
		if why := r.absTransform(ev.effectAST, consts, nil); why != "" {
			return refuse(rule, why)
		}
	}
	for i := range r.families {
		f := &r.families[i]
		rule := fmt.Sprintf("event %q", f.name)
		if !f.syntactic() {
			return refuse(rule, whyClosure)
		}
		if f.guardAST != nil {
			if why := r.absPred(f.guardAST, consts, f.params); why != "" {
				return refuse(rule, "guard "+why)
			}
		}
		if why := r.absTransform(f.effectAST, consts, f.params); why != "" {
			return refuse(rule, why)
		}
	}
	for _, p := range r.indepNames {
		for _, name := range p {
			if r.familyByName(name) < 0 && r.plainEvent(name) < 0 {
				return refuse(fmt.Sprintf("Independent(%q, %q)", p[0], p[1]), fmt.Sprintf("names %q, one instance "+
					"of an event with parameters: abstraction checks an event with parameters as a whole, over "+
					"representative values, so it can be declared Independent only as a whole", name))
			}
		}
	}
	for _, inv := range r.invariants {
		rule := fmt.Sprintf("invariant %q", inv.name)
		if !inv.syntactic() {
			return refuse(rule, whyClosure)
		}
		if why := r.absPred(inv.predAST, consts, nil); why != "" {
			return refuse(rule, why)
		}
		if why := r.absTransform(inv.repairAST, consts, nil); why != "" {
			return refuse(rule, "repair "+why)
		}
	}
	return nil
}

// absExprOK returns why e is outside the fragment, or "". params are the parameters the
// rule's event declares (nil for an invariant or an event without parameters).
func (r *Registry) absExprOK(e Expr, consts map[int]bool, params []Param) string {
	switch x := e.(type) {
	case argRef:
		if paramOf(params, x.name) == nil {
			return fmt.Sprintf("reads Arg(%q), which it does not declare", x.name)
		}
		return ""
	case varRef:
		if !r.owns(x.v) {
			return fmt.Sprintf("reads variable %q, which is not a variable of this registry", x.v.name)
		}
		return ""
	case litE:
		if !consts[x.n] {
			return fmt.Sprintf("uses the literal %d, which is not a declared constant. Declare it (Abstract(..., %d)), "+
				"or verify with Build without Abstract. %s", x.n, x.n, whyExact13)
		}
		return ""
	case binE:
		if args := exprArgs(e, nil); len(args) > 0 {
			return fmt.Sprintf("computes %s: arithmetic on the parameter %q is outside the fragment abstraction "+
				"covers (parameters are compared and copied only). Arithmetic on parameters needs the difference "+
				"route, which gsm does not implement with parameters; Build without Abstract checks every instance "+
				"of a parameter over a small range. %s", exprText(e), args[0], whyTriangle)
		}
		return fmt.Sprintf("computes %s: arithmetic is outside the fragment abstraction covers (comparisons and "+
			"copies). %s", exprText(e), whyTriangle)
	}
	return fmt.Sprintf("uses an expression gsm cannot inspect (%T)", e)
}

// absPred returns why p is outside the fragment, or "".
func (r *Registry) absPred(p Pred, consts map[int]bool, params []Param) string {
	switch x := p.(type) {
	case cmpP:
		if why := r.absExprOK(x.a, consts, params); why != "" {
			return why
		}
		return r.absExprOK(x.b, consts, params)
	case boolP:
		for _, q := range x.ps {
			if why := r.absPred(q, consts, params); why != "" {
				return why
			}
		}
		return ""
	case notP:
		return r.absPred(x.p, consts, params)
	}
	return fmt.Sprintf("uses a predicate gsm cannot inspect (%T)", p)
}

// absTransform returns why t is outside the fragment or could saturate, or "".
func (r *Registry) absTransform(t Transform, consts map[int]bool, params []Param) string {
	for _, a := range t {
		if !r.owns(a.v) {
			return fmt.Sprintf("writes variable %q, which is not a variable of this registry", a.v.name)
		}
		if why := r.absExprOK(a.e, consts, params); why != "" {
			return why
		}
		lo, hi := a.v.min, a.v.min+a.v.domain-1
		switch x := a.e.(type) {
		case argRef:
			if p := paramOf(params, x.name); p.Min < lo || p.Max > hi {
				return fmt.Sprintf("copies the parameter %q (%d..%d) into %q (%d..%d): the write could saturate, "+
					"which the abstraction theorem (over all integers) does not model. Give %q a range that contains "+
					"the parameter's", x.name, p.Min, p.Max, a.v.name, lo, hi, a.v.name)
			}
		case varRef:
			if ylo, yhi := x.v.min, x.v.min+x.v.domain-1; ylo < lo || yhi > hi {
				return fmt.Sprintf("copies %q (%d..%d) into %q (%d..%d): the write could saturate, which the "+
					"abstraction theorem (over all integers) does not model. Give %q a range that contains %q's",
					x.v.name, ylo, yhi, a.v.name, lo, hi, a.v.name, x.v.name)
			}
		case litE:
			if x.n < lo || x.n > hi {
				return fmt.Sprintf("writes %d into %q (%d..%d): the write would saturate, which the abstraction "+
					"theorem (over all integers) does not model", x.n, a.v.name, lo, hi)
			}
		}
	}
	return ""
}

// exprText renders an expression for a refusal message.
func exprText(e Expr) string {
	switch x := e.(type) {
	case varRef:
		return x.v.name
	case litE:
		return fmt.Sprint(x.n)
	case binE:
		return exprText(x.a) + " " + x.op + " " + exprText(x.b)
	case argRef:
		return fmt.Sprintf("Arg(%q)", x.name)
	}
	return fmt.Sprintf("%T", e)
}

// absReps returns reps n consts (normalization-confluence AbstractionCutoff.reps), distinct
// and ascending: the constants, the n integers above each, and the n below the least (or
// 0 .. n-1 when there is no constant). consts must be ascending and distinct.
func absReps(n int, consts []int) ([]int, error) {
	set := map[int]bool{}
	if len(consts) == 0 {
		for i := 0; i < n; i++ {
			set[i] = true
		}
	}
	for _, c := range consts {
		if c > math.MaxInt-n {
			return nil, fmt.Errorf("constant %d is within %d of the largest int, so its representatives would "+
				"overflow", c, n)
		}
		for i := 0; i <= n; i++ {
			set[c+i] = true
		}
	}
	if len(consts) > 0 {
		least := consts[0]
		if least < math.MinInt+n {
			return nil, fmt.Errorf("constant %d is within %d of the least int, so its representatives would "+
				"overflow", least, n)
		}
		for i := 1; i <= n; i++ {
			set[least-i] = true
		}
	}
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

// absModel is a registry's rules evaluated over the integers, on states given as the values
// of its variables in declaration order. It is the semantics of the Coq rule language
// (AbstractionCutoff.evalE, evalF; sap, srp, svd): no saturation. On a registry that passes
// absFragment it agrees with the registry's own rules on every state in range, since no
// write there saturates.
type absModel struct{ r *Registry }

func (m absModel) expr(e Expr, s []int) int {
	switch x := e.(type) {
	case varRef:
		return s[x.v.index]
	case litE:
		return x.n
	case binE:
		// Only absCheckUnfragmented (tests) reaches arithmetic.
		a, b := m.expr(x.a, s), m.expr(x.b, s)
		if x.op == "+" {
			return a + b
		}
		if x.op == "-" {
			return a - b
		}
		return 0
	}
	panic(fmt.Sprintf("gsm: abstraction: unexpected expression %T", e))
}

func (m absModel) pred(p Pred, s []int) bool {
	switch x := p.(type) {
	case cmpP:
		a, b := m.expr(x.a, s), m.expr(x.b, s)
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
		}
		return false
	case boolP:
		switch x.op {
		case "and":
			for _, q := range x.ps {
				if !m.pred(q, s) {
					return false
				}
			}
			return true
		case "or":
			for _, q := range x.ps {
				if m.pred(q, s) {
					return true
				}
			}
			return false
		}
		return false
	case notP:
		return !m.pred(x.p, s)
	}
	panic(fmt.Sprintf("gsm: abstraction: unexpected predicate %T", p))
}

// apply runs a transform's assignments left to right, as Transform.apply does.
func (m absModel) apply(t Transform, s []int) []int {
	out := append([]int(nil), s...)
	for _, a := range t {
		out[a.v.index] = m.expr(a.e, out)
	}
	return out
}

func (m absModel) valid(s []int) bool {
	for _, inv := range m.r.invariants {
		if !m.pred(inv.predAST, s) {
			return false
		}
	}
	return true
}

// repair is one compensation step: the first violated invariant's repair (s when valid).
func (m absModel) repair(s []int) []int {
	for _, inv := range m.r.invariants {
		if !m.pred(inv.predAST, s) {
			return m.apply(inv.repairAST, s)
		}
	}
	return s
}

func (m absModel) event(ev eventDef, s []int) []int {
	if ev.guardAST != nil && !m.pred(ev.guardAST, s) {
		return s
	}
	return m.apply(ev.effectAST, s)
}

// absResult is the representative check of a registry.
type absResult struct {
	reps  []int
	n     int
	ev    absEvents // the checked events: step[e] is the step of ev.evs[e]
	cut   int       // the cutoff N = n + 2m
	total int
	nf    []int   // nf[id]: the normal form of representative state id
	step  [][]int // step[e][id]: nf of event e applied to state id
	valid []bool
	depth int // the deepest repair chain (K)
	wfc   bool
}

// decode returns the values of representative state id.
func (a *absResult) decode(id int) []int {
	s := make([]int, a.n)
	for i := 0; i < a.n; i++ {
		s[i] = a.reps[id%len(a.reps)]
		id /= len(a.reps)
	}
	return s
}

// absCheck runs the representative check of r over consts (ascending, distinct): the normal
// form and step of every representative state, and WFC. It does not check the fragment;
// absFragment does, before Build calls it. A rule result outside the representatives is
// an error (closure_ap and closure_rp rule it out in the fragment).
func (r *Registry) absCheck(consts []int) (*absResult, error) {
	n := len(r.vars)
	cut := n + 2*r.absArity()
	reps, err := absReps(cut, consts)
	if err != nil {
		return nil, &AbstractionError{Registry: r.name, Reason: err.Error()}
	}
	total := 1
	for i := 0; i < n; i++ {
		if len(reps) == 0 || total > maxStateSpace/len(reps) {
			return nil, &AbstractionError{Registry: r.name, Reason: fmt.Sprintf("the representative domain has %d "+
				"values over %d variables, more than the limit of %d representative states (%d per variable: "+
				"each declared constant adds %d)", len(reps), n, maxStateSpace, len(reps), n+1)}
		}
		total *= len(reps)
	}
	index := make(map[int]int, len(reps))
	for i, v := range reps {
		index[v] = i
	}
	ae := r.absEventList(reps)
	if len(ae.evs) > 0 && total > maxParamTableEntries/len(ae.evs) {
		return nil, &AbstractionError{Registry: r.name, Reason: fmt.Sprintf("the check runs %d events (events with "+
			"parameters at every representative value) over %d representative states, more step-table entries "+
			"than the limit of %d: fewer variables, parameters or constants shrink it", len(ae.evs), total,
			maxParamTableEntries)}
	}
	if ne := len(ae.evs); ae.m > 0 && ne*(ne-1)/2 > maxParamPairWork/total {
		return nil, &AbstractionError{Registry: r.name, Reason: fmt.Sprintf("the check runs %d events (events with "+
			"parameters at every representative value), %d pairs over %d representative states, more CC work than "+
			"the limit of %d pair-states: fewer variables, parameters or constants shrink it", ne, ne*(ne-1)/2, total,
			maxParamPairWork)}
	}
	a := &absResult{reps: reps, n: n, ev: ae, cut: cut, total: total, valid: make([]bool, total), nf: make([]int, total)}
	encode := func(s []int) (int, error) {
		id, mul := 0, 1
		for i := 0; i < n; i++ {
			k, ok := index[s[i]]
			if !ok {
				return 0, fmt.Errorf("gsm: registry %q: abstraction: a rule produced %d for %q, which is not a "+
					"representative", r.name, s[i], r.vars[i].name)
			}
			id += k * mul
			mul *= len(reps)
		}
		return id, nil
	}
	m := absModel{r}
	for id := 0; id < total; id++ {
		a.valid[id] = m.valid(a.decode(id))
	}
	a.wfc = true
	for id := 0; id < total; id++ {
		if a.valid[id] {
			a.nf[id] = id
			continue
		}
		s, cur, depth := a.decode(id), id, 0
		seen := map[int]bool{id: true}
		for !a.valid[cur] {
			s = m.repair(s)
			if cur, err = encode(s); err != nil {
				return nil, err
			}
			depth++
			if seen[cur] || depth > total {
				a.wfc = false
				return a, nil
			}
			seen[cur] = true
		}
		a.nf[id] = cur
		if depth > a.depth {
			a.depth = depth
		}
	}
	a.step = make([][]int, len(ae.evs))
	for ei, ev := range ae.evs {
		a.step[ei] = make([]int, total)
		for id := 0; id < total; id++ {
			after, err := encode(m.event(ev, a.decode(id)))
			if err != nil {
				return nil, err
			}
			a.step[ei][id] = a.nf[after]
		}
	}
	return a, nil
}

// absConstants returns the constants of the check, ascending and distinct: the declared
// constants and the bounds of every event parameter. The check runs each parameterized event
// with its range test conjoined to its guard (absEventList), a comparison against the bounds,
// so the bounds are constants of the registry it checks.
func (r *Registry) absConstants() []int {
	set := map[int]bool{}
	for _, c := range r.abs.constants {
		set[c] = true
	}
	for i := range r.families {
		for _, p := range r.families[i].params {
			set[p.Min], set[p.Max] = true, true
		}
	}
	out := make([]int, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// inRange reports whether every value of s is within its variable's declared range.
func (r *Registry) inRange(s []int) bool {
	for i, v := range r.vars {
		if s[i] < v.min || s[i] > v.min+v.domain-1 {
			return false
		}
	}
	return true
}

// stateOf returns s as a State of r over vars (s must be in range).
func (r *Registry) stateOf(s []int, vars []Var) State {
	st := State{vars: vars}
	for i, v := range vars {
		st = st.setRaw(v, uint64(s[i]-v.min))
	}
	return st
}

// absFailure returns the CC failure of events i and j at representative state id.
func (r *Registry) absFailure(a *absResult, i, j, id int) CCFailure {
	names := make([]string, len(r.vars))
	for k, v := range r.vars {
		names[k] = v.name
	}
	s, r1, r2 := a.decode(id), a.decode(a.step[j][a.step[i][id]]), a.decode(a.step[i][a.step[j][id]])
	e1, e2 := a.ev.evs[i], a.ev.evs[j]
	w := &AbstractWitness{Vars: names, State: s, Result1: r1, Result2: r2,
		InRange: r.inRange(s) && r.argsInRange(e1) && r.argsInRange(e2)}
	f := CCFailure{Event1: e1.name, Event2: e2.name, Abstract: w}
	if w.InRange {
		f.State, f.Result1, f.Result2 = r.stateOf(s, r.vars), r.stateOf(r1, r.vars), r.stateOf(r2, r.vars)
	}
	return f
}

// absWitness returns a valid representative state on which events i and j do not commute,
// preferring one within the declared ranges, or -1.
func (r *Registry) absWitness(a *absResult, i, j int) int {
	found := -1
	for id := 0; id < a.total; id++ {
		if a.valid[id] && a.step[j][a.step[i][id]] != a.step[i][a.step[j][id]] {
			if r.inRange(a.decode(id)) {
				return id
			}
			if found < 0 {
				found = id
			}
		}
	}
	return found
}

// absTables returns the representative tables for the table oracle: states renumbered so
// that state 0 (which the oracle treats as the zero state and checks whatever its validity)
// is a valid state, so the oracle checks CC1 at exactly the valid representative states,
// the condition Build checks. It needs a valid state, which WFC guarantees.
func (r *Registry) absTables(a *absResult, pairs [][2]int) (oracle.Tables, error) {
	first := -1
	for id := 0; id < a.total; id++ {
		if a.valid[id] {
			first = id
			break
		}
	}
	if first < 0 {
		return oracle.Tables{}, fmt.Errorf("gsm: registry %q: no representative state is valid", r.name)
	}
	ren := func(id int) int {
		switch id {
		case 0:
			return first
		case first:
			return 0
		}
		return id
	}
	tb := oracle.Tables{NF: make([]int, a.total), Step: make([][]int, len(a.step)), AllPairs: r.allIndependent}
	for id := 0; id < a.total; id++ {
		tb.NF[ren(id)] = ren(a.nf[id])
	}
	for e := range a.step {
		tb.Step[e] = make([]int, a.total)
		for id := 0; id < a.total; id++ {
			tb.Step[e][ren(id)] = ren(a.step[e][id])
		}
	}
	if !r.allIndependent {
		tb.Pairs = pairs
		if tb.Pairs == nil {
			tb.Pairs = [][2]int{}
		}
	}
	return tb, nil
}

// representativeTables runs the fragment and representative checks of a registry declared
// with Abstract and returns its representative tables (see absTables), for the
// differential test and the example-machine gate. It fails when the checks do not reach
// the CC phase.
func (r *Registry) representativeTables() (oracle.Tables, error) {
	consts := r.absConstants()
	set := map[int]bool{}
	for _, c := range consts {
		set[c] = true
	}
	if ae := r.absFragment(set); ae != nil {
		return oracle.Tables{}, ae
	}
	a, err := r.absCheck(consts)
	if err != nil {
		return oracle.Tables{}, err
	}
	if !a.wfc {
		return oracle.Tables{}, fmt.Errorf("gsm: registry %q: WFC fails on the representatives", r.name)
	}
	return r.absTables(a, a.ev.pairs(r))
}

// absCCWitness returns, for the pairs in order, the first pair that does not commute at
// some valid representative state and that state, preferring a pair and state within the
// declared ranges (state and parameter values), or -1.
func (r *Registry) absCCWitness(a *absResult, pairs [][2]int) (int, int) {
	firstP, firstID := -1, -1
	for pi, p := range pairs {
		id := r.absWitness(a, p[0], p[1])
		if id < 0 {
			continue
		}
		if r.inRange(a.decode(id)) && r.argsInRange(a.ev.evs[p[0]]) && r.argsInRange(a.ev.evs[p[1]]) {
			return pi, id
		}
		if firstP < 0 {
			firstP, firstID = pi, id
		}
	}
	return firstP, firstID
}

// buildAbstract is Build for a registry declared with Abstract.
func (r *Registry) buildAbstract() (*Machine, *Report, error) {
	if err := r.checkNamesFor(true); err != nil {
		return nil, nil, err
	}
	report := &Report{Name: r.name, VarCount: len(r.vars), EventCount: len(r.events)}
	if len(r.families) > 0 {
		// Until the representative instances are known: each event without parameters, and
		// each parameterized event once.
		report.EventCount = len(r.families)
		for _, ev := range r.events {
			if ev.family == 0 {
				report.EventCount++
			}
		}
	}
	consts := r.absConstants()
	set := map[int]bool{}
	for _, c := range consts {
		set[c] = true
	}
	refused := func(ae *AbstractionError) (*Machine, *Report, error) {
		report.AbstractionRefused = ae.Error()
		return nil, report, ae
	}
	if ae := r.absFragment(set); ae != nil {
		return refused(ae)
	}
	if r.totalBits > 64 {
		return refused(&AbstractionError{Registry: r.name, Reason: fmt.Sprintf("the variables need %d bits, and a "+
			"state holds 64: narrow the ranges", r.totalBits)})
	}
	a, err := r.absCheck(consts)
	if err != nil {
		if ae, ok := err.(*AbstractionError); ok {
			return refused(ae)
		}
		return nil, report, err
	}
	names := make([]string, len(r.vars))
	for i, v := range r.vars {
		names[i] = v.name
	}
	report.Abstraction = &AbstractionReduction{Over: names, Constants: consts, Cutoff: a.cut, Params: a.ev.m,
		Representatives: a.reps, States: a.total}
	report.StateCount = a.total
	report.EventCount = len(a.ev.evs)
	if len(r.families) > 0 {
		report.Families = make([]EventFamily, len(r.families))
		for i := range r.families {
			f := &r.families[i]
			inst := 0
			for _, ev := range a.ev.evs {
				if ev.family == i+1 {
					inst++
				}
			}
			report.Families[i] = EventFamily{Name: f.name, Params: append([]Param(nil), f.params...),
				Instances: inst, Representative: true}
		}
	}

	// WFC over the representatives: TermK over every integer state (term_abs).
	if !a.wfc {
		report.WFC = false
		return nil, report, fmt.Errorf("gsm: WFC check failed: compensation does not terminate from some "+
			"representative state of %q (over the integers, by term_abs)", r.name)
	}
	report.WFC, report.MaxRepairLen = true, a.depth

	// CC1 at every valid representative state, for the checked pairs (every pair of
	// representative instances of the checked kinds); the undeclared pairs in declared-only
	// mode too, as Build's verifyCC does.
	declared := a.ev.pairs(r)
	report.PairsTotal = len(declared)
	if pi, id := r.absCCWitness(a, declared); pi >= 0 {
		p := declared[pi]
		f := r.absFailure(a, p[0], p[1], id)
		report.CC, report.PairsBrute, report.CCFailure = false, report.PairsTotal, &f
		msg := "gsm: Compensation Commutativity (CC) check failed"
		if !f.Abstract.InRange {
			msg += " at a representative state or parameter value outside the declared ranges, so abstraction " +
				"cannot certify the machine; the machine as declared may still converge"
		}
		return nil, report, errors.New(msg)
	}
	report.CC, report.PairsBrute = true, report.PairsTotal
	if !r.allIndependent {
		isDeclared := map[[2]int]bool{}
		for _, p := range declared {
			isDeclared[p] = true
		}
		// One witness per pair of kinds: a listed pair of instances of events with parameters
		// stands for the two events as a whole.
		byKinds := map[[2]int][][2]int{}
		var kindOrder [][2]int
		for i := 0; i < len(a.ev.evs); i++ {
			for j := i + 1; j < len(a.ev.evs); j++ {
				if isDeclared[[2]int{i, j}] {
					continue
				}
				report.PairsUndeclared++
				k := [2]int{a.ev.kind[i], a.ev.kind[j]}
				if _, ok := byKinds[k]; !ok {
					kindOrder = append(kindOrder, k)
				}
				byKinds[k] = append(byKinds[k], [2]int{i, j})
			}
		}
		for _, k := range kindOrder {
			ps := byKinds[k]
			if pi, id := r.absCCWitness(a, ps); pi >= 0 {
				report.CausalOrderRequired = append(report.CausalOrderRequired, r.absFailure(a, ps[pi][0], ps[pi][1], id))
			}
		}
	}
	// Idempotence at every valid representative state, per kind (idem_valid_abs): an event
	// with parameters is listed by its signature, with a representative witness in
	// Report.Families, preferring one within the declared ranges.
	notIdem := map[int]int{} // kind -> witness event
	for ei, ev := range a.ev.evs {
		k := a.ev.kind[ei]
		if w, ok := notIdem[k]; ok && r.argsInRange(a.ev.evs[w]) {
			continue
		}
		for id := 0; id < a.total; id++ {
			if a.valid[id] && a.step[ei][a.step[ei][id]] != a.step[ei][id] {
				if _, ok := notIdem[k]; !ok || r.argsInRange(ev) {
					notIdem[k] = ei
				}
				break
			}
		}
	}
	for k, name := range a.ev.kinds {
		w, ok := notIdem[k]
		if !ok {
			continue
		}
		ev := a.ev.evs[w]
		if ev.family == 0 {
			report.NotIdempotent = append(report.NotIdempotent, name)
			continue
		}
		fam := &report.Families[ev.family-1]
		fam.NotIdempotent = []string{ev.name}
		report.NotIdempotent = append(report.NotIdempotent, fam.Signature())
	}

	// The oracle gate: the verified table oracle certifies the representative tables.
	tb, err := r.absTables(a, declared)
	if err == nil {
		err = certifyTables(tb.Lookup(), "the representative tables")
	} else {
		err = &oracleError{fmt.Sprintf("gsm: cannot give the representative tables to the verified table oracle: %v; "+
			"not certified", err)}
	}
	if err != nil {
		report.failClosed(err)
		return nil, report, err
	}
	report.Assurance = AssuranceOracleRepresentatives
	report.RulesOracleSkipped = "verified by abstraction: the rules oracle would enumerate the declared ranges"

	m := &Machine{
		name:        r.name,
		vars:        r.vars,
		events:      make(map[string]int),
		dom:         newDomainCheck(r.vars),
		ccPairs:     r.ccPairs(),
		allPairs:    r.allIndependent,
		lazy:        true,
		abstract:    true,
		invariants:  append([]invariantDef(nil), r.invariants...),
		eventDefs:   append([]eventDef(nil), r.events...),
		repairBound: a.depth,
		families:    familiesOf(r),
	}
	for i, ev := range r.events {
		m.events[ev.name] = i
	}
	return m, report, nil
}
