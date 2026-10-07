package gsm

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Events with parameters.
//
// An event may declare integer parameters, each over a range: "withdraw amount",
// "reserve qty", "book n rooms". A parameterized event is the family of its instances,
// one ordinary event per assignment of values to its parameters, named
// Instance(name, values...): withdraw(25), move(1,2). Declaration expands the family into
// its instances, so every path that checks, certifies, exports or runs events (Build, the
// per-component check, BuildCompositional, Synthesize, collections, federations,
// CheckMigration, certificates, the oracle tables, Export) sees ordinary events and needs
// no new theory: the instances are a finite family of events, and the existing theorems
// cover every finite family.
//
// The cost is the instance count: every instance is an event of the machine, so the step
// tables and the event pairs CC checks grow with it. A family has at most
// maxFamilyInstances instances; a wider one is checked only by abstraction
// (Registry.Abstract), which checks the rules over representative parameter values
// (normalization-confluence coq/AbstractionGsm.v, cutoff n + 2m) and refuses arithmetic on
// a parameter.

// maxFamilyInstances is the most instances one parameterized event may expand into (the
// product of its parameters' range sizes). A wider family is refused, except by abstraction.
const maxFamilyInstances = 1024

// maxParamTableEntries and maxParamPairWork bound Build's global check of a registry with
// parameterized events: the step-table entries (instances times encodings) and the CC work
// (checked pairs times states). Without parameters a registry's events are written one by
// one; with parameters a few declarations can ask for millions of pairs, so the global
// check refuses such a registry up front rather than run for hours.
const (
	maxParamTableEntries = 1 << 26
	maxParamPairWork     = 1 << 32
)

// Param is a declared event parameter: an integer in Min..Max.
type Param struct {
	Name     string
	Min, Max int
}

func (p Param) String() string { return fmt.Sprintf("%s %d..%d", p.Name, p.Min, p.Max) }

// size is the number of values in the range (Max >= Min is checked at declaration).
func (p Param) size() int { return p.Max - p.Min + 1 }

// Args are the values of an event's parameters, by name, as a closure rule of a
// parameterized event receives them (EventBuilder.GuardArgs, EventBuilder.ApplyArgs).
type Args map[string]int

// Instance returns the name of an instance of a parameterized event: the event's name
// followed by the values in declaration order, comma-separated in parentheses, as in
// withdraw(25) or move(1,2). Every API that takes an event name (Machine.Apply,
// CollectionMachine.Apply, FedMachine.ApplyNamed, Registry.Independent, CheckMigration's
// event map, an exported machine's event list) takes an instance name.
func Instance(event string, args ...int) string {
	var b strings.Builder
	b.WriteString(event)
	b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(a))
	}
	b.WriteByte(')')
	return b.String()
}

// argRef reads an event parameter. It is only meaningful inside a parameterized event:
// declaration substitutes each instance's value for it, so no checked or running rule ever
// evaluates one.
type argRef struct{ name string }

func (x argRef) eval(State) int {
	panic(fmt.Sprintf("gsm: Arg(%q) evaluated outside a parameterized event", x.name))
}
func (argRef) vars() []int { return nil }

// Arg reads the parameter name of the event being declared, in a combinator guard or
// effect: Ge(V(balance), Arg("amount")), Set(balance, Sub(V(balance), Arg("amount"))).
// The event must declare the parameter (OnBuilder.Param). An invariant, or an event
// without that parameter, cannot read it: declaring one panics.
func Arg(name string) Expr { return argRef{name} }

// IncByArg: v := v + the parameter name.
func IncByArg(v Var, name string) Transform { return Do(Set(v, Add(V(v), Arg(name)))) }

// DecByArg: v := v - the parameter name.
func DecByArg(v Var, name string) Transform { return Do(Set(v, Sub(V(v), Arg(name)))) }

// SetToArg: v := the parameter name.
func SetToArg(v Var, name string) Transform { return Do(Set(v, Arg(name))) }

// ---- substitution and inspection of parameter reads ----

func substExpr(e Expr, vals map[string]int) Expr {
	switch x := e.(type) {
	case argRef:
		return litE{vals[x.name]}
	case binE:
		return binE{x.op, substExpr(x.a, vals), substExpr(x.b, vals)}
	}
	return e
}

func substPred(p Pred, vals map[string]int) Pred {
	switch x := p.(type) {
	case cmpP:
		return cmpP{x.op, substExpr(x.a, vals), substExpr(x.b, vals)}
	case boolP:
		ps := make([]Pred, len(x.ps))
		for i, q := range x.ps {
			ps[i] = substPred(q, vals)
		}
		return boolP{x.op, ps}
	case notP:
		return notP{substPred(x.p, vals)}
	}
	return p
}

func substTransform(t Transform, vals map[string]int) Transform {
	if t == nil {
		return nil
	}
	out := make(Transform, len(t))
	for i, a := range t {
		out[i] = Assign{a.v, substExpr(a.e, vals)}
	}
	return out
}

// exprArgs appends the parameter names e reads.
func exprArgs(e Expr, out []string) []string {
	switch x := e.(type) {
	case argRef:
		return append(out, x.name)
	case binE:
		return exprArgs(x.b, exprArgs(x.a, out))
	}
	return out
}

func predArgs(p Pred, out []string) []string {
	switch x := p.(type) {
	case cmpP:
		return exprArgs(x.b, exprArgs(x.a, out))
	case boolP:
		for _, q := range x.ps {
			out = predArgs(q, out)
		}
	case notP:
		return predArgs(x.p, out)
	}
	return out
}

func transformArgs(t Transform, out []string) []string {
	for _, a := range t {
		out = exprArgs(a.e, out)
	}
	return out
}

// noArgs panics when a rule that declares no parameters reads one (rule names it).
func noArgs(rule string, p Pred, t Transform) {
	var names []string
	if p != nil {
		names = predArgs(p, names)
	}
	names = transformArgs(t, names)
	if len(names) > 0 {
		panic(fmt.Sprintf("gsm: %s reads Arg(%q), but only an event that declares the parameter (Param) can read it",
			rule, names[0]))
	}
}

// ---- families ----

// familyDef is a declared parameterized event.
type familyDef struct {
	name   string
	params []Param

	// Combinator form (OnBuilder): the guard and effect, reading parameters through Arg.
	guardAST  Pred
	effectAST Transform

	// Closure form (EventBuilder): nil for the combinator form.
	writes   []int
	guardFn  func(State, Args) bool
	effectFn func(State, Args) State
	foreign  string

	pos      int  // index in Registry.events where the instances start (or would start)
	count    int  // number of instances (math.MaxInt when the product overflows)
	expanded bool // the instances are in Registry.events
}

// syntactic reports whether the family is declared with combinators.
func (f *familyDef) syntactic() bool { return f.effectAST != nil }

// signature renders the family as name(p1, p2).
func (f *familyDef) signature() string {
	names := make([]string, len(f.params))
	for i, p := range f.params {
		names[i] = p.Name
	}
	return f.name + "(" + strings.Join(names, ", ") + ")"
}

// ranges renders the parameters with their ranges.
func (f *familyDef) ranges() string {
	parts := make([]string, len(f.params))
	for i, p := range f.params {
		parts[i] = p.String()
	}
	return strings.Join(parts, ", ")
}

// inRange reports whether args are values of the family's parameters.
func (f *familyDef) inRange(args []int) bool {
	if len(args) != len(f.params) {
		return false
	}
	for i, p := range f.params {
		if args[i] < p.Min || args[i] > p.Max {
			return false
		}
	}
	return true
}

// argsError says why args are not a valid argument list for the family, or "".
func (f *familyDef) argsError(args []int) string {
	if len(args) != len(f.params) {
		return fmt.Sprintf("event %q takes %d argument(s) (%s), got %d", f.name, len(f.params), f.ranges(), len(args))
	}
	for i, p := range f.params {
		if args[i] < p.Min || args[i] > p.Max {
			return fmt.Sprintf("event %q: argument %s = %d is outside %d..%d", f.name, p.Name, args[i], p.Min, p.Max)
		}
	}
	return ""
}

// instance returns the event of the family at args (which need not be in range: the
// abstraction check evaluates representative values outside the declared ranges).
func (f *familyDef) instance(fi int, args []int) eventDef {
	vals := make(map[string]int, len(f.params))
	for i, p := range f.params {
		vals[p.Name] = args[i]
	}
	ev := eventDef{name: Instance(f.name, args...), family: fi + 1, args: append([]int(nil), args...)}
	if f.syntactic() {
		effect := substTransform(f.effectAST, vals)
		ev.writes = effect.writeVars()
		ev.effect = func(s State) State { return effect.apply(s) }
		ev.effectAST = effect
		if f.guardAST != nil {
			guard := substPred(f.guardAST, vals)
			ev.guard = func(s State) bool { return guard.holds(s) }
			ev.guardAST = guard
		}
		return ev
	}
	ev.writes = append([]int(nil), f.writes...)
	ev.foreign = f.foreign
	args2 := Args(vals)
	if fn := f.effectFn; fn != nil {
		ev.effect = func(s State) State { return fn(s, args2) }
	}
	if fn := f.guardFn; fn != nil {
		ev.guard = func(s State) bool { return fn(s, args2) }
	}
	return ev
}

// tuples calls fn with every assignment of values from dom[i] to parameter i, in
// lexicographic order (the first parameter varies slowest). fn must not keep the slice.
func tuples(dom [][]int, fn func([]int)) {
	cur := make([]int, len(dom))
	idx := make([]int, len(dom))
	for _, d := range dom {
		if len(d) == 0 {
			return
		}
	}
	for i := range dom {
		cur[i] = dom[i][0]
	}
	for {
		fn(cur)
		k := len(dom) - 1
		for ; k >= 0; k-- {
			idx[k]++
			if idx[k] < len(dom[k]) {
				cur[k] = dom[k][idx[k]]
				break
			}
			idx[k] = 0
			cur[k] = dom[k][0]
		}
		if k < 0 {
			return
		}
	}
}

// declareFamily registers a parameterized event and, when it has at most
// maxFamilyInstances instances, appends them to the registry's events.
func (r *Registry) declareFamily(f familyDef) {
	count := 1
	for _, p := range f.params {
		if count > math.MaxInt/p.size() {
			count = math.MaxInt
			break
		}
		count *= p.size()
	}
	f.count, f.pos = count, len(r.events)
	fi := len(r.families)
	if count <= maxFamilyInstances {
		f.expanded = true
		dom := make([][]int, len(f.params))
		for i, p := range f.params {
			for v := p.Min; v <= p.Max; v++ {
				dom[i] = append(dom[i], v)
			}
		}
		tuples(dom, func(args []int) { r.events = append(r.events, f.instance(fi, args)) })
	}
	r.families = append(r.families, f)
}

// checkParam panics on a parameter that cannot be declared.
func checkParam(event string, params []Param, name string, min, max int) {
	if name == "" {
		panic(fmt.Sprintf("gsm: event %q: a parameter needs a name", event))
	}
	if max < min {
		panic(fmt.Sprintf("gsm: event %q: parameter %q has max < min", event, name))
	}
	if span := max - min; span < 0 || span == math.MaxInt {
		panic(fmt.Sprintf("gsm: event %q: parameter %q range %d..%d has more than math.MaxInt values", event, name, min, max))
	}
	for _, p := range params {
		if p.Name == name {
			panic(fmt.Sprintf("gsm: event %q declares parameter %q twice", event, name))
		}
	}
}

// checkArgs panics when a family's rules read a parameter it does not declare.
func checkArgs(event string, params []Param, p Pred, t Transform) {
	var names []string
	if p != nil {
		names = predArgs(p, names)
	}
	names = transformArgs(t, names)
	for _, n := range names {
		found := false
		for _, q := range params {
			if q.Name == n {
				found = true
				break
			}
		}
		if !found {
			panic(fmt.Sprintf("gsm: event %q reads Arg(%q), which it does not declare (Param)", event, n))
		}
	}
}

// familyByName returns the index of the parameterized event called name, or -1.
func (r *Registry) familyByName(name string) int {
	for i := range r.families {
		if r.families[i].name == name {
			return i
		}
	}
	return -1
}

// checkFamilies rejects a family whose name is also an event's (or another family's), and,
// unless wide is allowed (Build by abstraction), a family too wide to expand.
func (r *Registry) checkFamilies(allowWide bool) error {
	plain := map[string]bool{}
	for _, ev := range r.events {
		if ev.family == 0 {
			plain[ev.name] = true
		}
	}
	seen := map[string]bool{}
	for i := range r.families {
		f := &r.families[i]
		if seen[f.name] {
			return fmt.Errorf("gsm: registry %q: duplicate event name %q (two parameterized events)", r.name, f.name)
		}
		seen[f.name] = true
		if plain[f.name] {
			return fmt.Errorf("gsm: registry %q: duplicate event name %q (an event and a parameterized event)", r.name, f.name)
		}
		if f.foreign != "" {
			return fmt.Errorf("gsm: registry %q: event %q Writes variable %q, which is not a variable of this "+
				"registry (was it declared on another registry?)", r.name, f.name, f.foreign)
		}
		if !f.expanded && !allowWide {
			return fmt.Errorf("gsm: registry %q: event %s has %s instances, above the limit of %d instances "+
				"per parameterized event: Build checks every instance as its own event. Narrow the parameter "+
				"ranges, or declare Abstract to check it over representative values (rules that compare and "+
				"copy parameters only, no arithmetic on them)", r.name, f.signature()+" over "+f.ranges(),
				countText(f.count), maxFamilyInstances)
		}
	}
	return nil
}

func countText(n int) string {
	if n == math.MaxInt {
		return "more than math.MaxInt"
	}
	return groupDigits(strconv.Itoa(n))
}

// paramBudget refuses, for the global check of a registry with parameterized events, a
// step table or CC check too large to run (maxParamTableEntries, maxParamPairWork).
func (r *Registry) paramBudget(packedCount, stateCount int) error {
	if len(r.families) == 0 {
		return nil
	}
	// Every pair is checked: the declared ones, and in declared-only mode the undeclared
	// ones too (Report.CausalOrderRequired).
	n := len(r.events)
	pairs := n * (n - 1) / 2
	if packedCount > 0 && n > maxParamTableEntries/packedCount {
		return &stateSpaceError{fmt.Sprintf("gsm: registry %q: its parameterized events expand to %d events over %d "+
			"encodings, more step-table entries than the limit of %d: narrow the parameter ranges, or declare "+
			"Abstract to check the parameters over representative values", r.name, n, packedCount, maxParamTableEntries)}
	}
	if stateCount > 0 && pairs > maxParamPairWork/stateCount {
		return &stateSpaceError{fmt.Sprintf("gsm: registry %q: its parameterized events expand to %d events, %d pairs "+
			"over %d states, more CC work than the limit of %d pair-states: narrow the parameter ranges, or declare "+
			"Abstract to check the parameters over representative values", r.name, n, pairs, stateCount, maxParamPairWork)}
	}
	return nil
}

// ---- declaration surface ----

// Param declares an integer parameter of the event, with values min..max. Each call adds
// one parameter, in order: Instance and ApplyWith take the values in that order. Guards and
// effects read it with Arg(name). The event becomes the family of its instances, one per
// assignment of values, each checked as its own event, so the ranges multiply the event
// count; a family of more than 1024 instances needs Abstract (see Registry.Abstract).
//
// It panics if max < min, or if the event already declares a parameter of that name.
func (b *OnBuilder) Param(name string, min, max int) *OnBuilder {
	checkParam(b.name, b.params, name, min, max)
	b.params = append(b.params, Param{Name: name, Min: min, Max: max})
	return b
}

// Param declares an integer parameter of a closure event, as OnBuilder.Param does. The
// guard and effect read the values through GuardArgs and ApplyArgs.
func (eb *EventBuilder) Param(name string, min, max int) *EventBuilder {
	checkParam(eb.def.name, eb.params, name, min, max)
	eb.params = append(eb.params, Param{Name: name, Min: min, Max: max})
	return eb
}

// GuardArgs sets the precondition of a parameterized closure event; it receives the
// instance's parameter values. The event is a no-op when it returns false.
func (eb *EventBuilder) GuardArgs(fn func(State, Args) bool) *EventBuilder {
	eb.guardArgs = fn
	return eb
}

// ApplyArgs sets the effect of a parameterized closure event; it receives the instance's
// parameter values, and must return a state of this machine (see EffectFunc).
func (eb *EventBuilder) ApplyArgs(fn func(State, Args) State) *EventBuilder {
	eb.applyArgs = fn
	return eb
}

// ---- reporting ----

// EventFamily describes a parameterized event in a Report.
type EventFamily struct {
	// Name is the event's name; its instances are Instance(Name, values...).
	Name string
	// Params are the declared parameters, in order.
	Params []Param
	// Instances is the number of instances checked: every assignment of values, or under
	// abstraction the representative assignments within the ranges, plus one for every
	// assignment outside them, which is one event that does nothing (Representative).
	Instances int
	// Representative is true when Build checked the family by abstraction, over
	// representative parameter values, with the result holding for every value.
	Representative bool
	// NotIdempotent lists the instances that are not idempotent: every such instance when
	// each was checked, or under abstraction one representative witness (the family is
	// then listed in Report.NotIdempotent by its signature, and every value needs
	// deduplication).
	NotIdempotent []string
}

// Signature renders the family as name(p1, p2).
func (f EventFamily) Signature() string {
	names := make([]string, len(f.Params))
	for i, p := range f.Params {
		names[i] = p.Name
	}
	return f.Name + "(" + strings.Join(names, ", ") + ")"
}

// reportFamilies summarizes the registry's families for a Report checked instance by
// instance, with the instances listed in notIdem.
func (r *Registry) reportFamilies(notIdem []string) []EventFamily {
	if len(r.families) == 0 {
		return nil
	}
	out := make([]EventFamily, len(r.families))
	for i := range r.families {
		f := &r.families[i]
		out[i] = EventFamily{Name: f.name, Params: append([]Param(nil), f.params...), Instances: f.count}
	}
	for _, name := range notIdem {
		for _, ev := range r.events {
			if ev.name == name && ev.family > 0 {
				out[ev.family-1].NotIdempotent = append(out[ev.family-1].NotIdempotent, name)
				break
			}
		}
	}
	return out
}

// groupEvents renders event names for a report line, grouping the instances of each
// parameterized event: "withdraw(amount) for amount in {1, 2, 3}", or, when every instance
// is listed, "withdraw(amount) (every value)". Names that are no instance stay as they are.
func groupEvents(names []string, fams []EventFamily) string {
	if len(fams) == 0 {
		return strings.Join(names, ", ")
	}
	byFam := map[int][]string{}
	var order []string
	placed := map[int]bool{}
	for _, n := range names {
		fi := -1
		for i, f := range fams {
			if f.Representative && n == f.Signature() {
				break // a family checked by abstraction, listed whole
			}
			if strings.HasPrefix(n, f.Name+"(") && strings.HasSuffix(n, ")") {
				if _, ok := parseArgs(n[len(f.Name)+1:len(n)-1], len(f.Params)); ok {
					fi = i
					break
				}
			}
		}
		if fi < 0 {
			order = append(order, n)
			continue
		}
		byFam[fi] = append(byFam[fi], n)
		if !placed[fi] {
			placed[fi] = true
			order = append(order, "\x00"+strconv.Itoa(fi))
		}
	}
	parts := make([]string, 0, len(order))
	for _, o := range order {
		if !strings.HasPrefix(o, "\x00") {
			for _, f := range fams {
				if f.Representative && o == f.Signature() && len(f.NotIdempotent) > 0 {
					o += fmt.Sprintf(" (every value; witness %s)", f.NotIdempotent[0])
					break
				}
			}
			parts = append(parts, o)
			continue
		}
		fi, _ := strconv.Atoi(o[1:])
		f, inst := fams[fi], byFam[fi]
		if !f.Representative && len(inst) == f.Instances {
			parts = append(parts, f.Signature()+" (every value)")
			continue
		}
		vals := make([]string, 0, len(inst))
		for i, n := range inst {
			if i == 8 {
				vals = append(vals, fmt.Sprintf("and %d more", len(inst)-8))
				break
			}
			vals = append(vals, n[len(f.Name):])
		}
		parts = append(parts, fmt.Sprintf("%s at %s", f.Signature(), strings.Join(vals, ", ")))
	}
	return strings.Join(parts, ", ")
}

// parseArgs parses "v1,v2" (k values).
func parseArgs(s string, k int) ([]int, bool) {
	if k == 0 {
		return nil, s == ""
	}
	fields := strings.Split(s, ",")
	if len(fields) != k {
		return nil, false
	}
	out := make([]int, k)
	for i, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil || strconv.Itoa(v) != f {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// ---- machines ----

// machineFamily is a parameterized event as a built Machine holds it.
type machineFamily struct {
	def   familyDef
	index int // index in the registry's families
}

// familiesOf copies a registry's families for a machine.
func familiesOf(r *Registry) map[string]*machineFamily {
	if len(r.families) == 0 {
		return nil
	}
	out := make(map[string]*machineFamily, len(r.families))
	for i := range r.families {
		out[r.families[i].name] = &machineFamily{def: r.families[i], index: i}
	}
	return out
}

// parseInstance resolves an instance name to its family and arguments.
func (m *Machine) parseInstance(name string) (*machineFamily, []int, bool) {
	if !strings.HasSuffix(name, ")") {
		return nil, nil, false
	}
	for fname, f := range m.families {
		if strings.HasPrefix(name, fname+"(") {
			if args, ok := parseArgs(name[len(fname)+1:len(name)-1], len(f.def.params)); ok {
				return f, args, true
			}
		}
	}
	return nil, nil, false
}

// ApplyWith applies an instance of the parameterized event called event, with its
// parameters' values in declaration order: m.ApplyWith(s, "withdraw", 25) is
// m.Apply(s, Instance("withdraw", 25)). It panics if event is not a parameterized event of
// the machine, or if the arguments do not match its parameters (their number, or a value
// outside its range), naming the parameter.
func (m *Machine) ApplyWith(s State, event string, args ...int) State {
	f, ok := m.families[event]
	if !ok {
		if _, plain := m.events[event]; plain {
			panic(fmt.Sprintf("gsm: ApplyWith: event %q has no parameters; use Apply", event))
		}
		panic(fmt.Sprintf("gsm: unknown event %q", event))
	}
	if why := f.def.argsError(args); why != "" {
		panic("gsm: ApplyWith: " + why)
	}
	return m.Apply(s, Instance(event, args...))
}

// Params returns the parameters of the event called event, in declaration order, and
// whether it is a parameterized event of the machine.
func (m *Machine) Params(event string) ([]Param, bool) {
	f, ok := m.families[event]
	if !ok {
		return nil, false
	}
	return append([]Param(nil), f.def.params...), true
}

// Families returns the names of the machine's parameterized events, sorted.
func (m *Machine) Families() []string {
	out := make([]string, 0, len(m.families))
	for name := range m.families {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// lazyInstance returns, on a lazy machine, the event an instance name of a family that was
// not expanded (checked by abstraction) denotes. It panics on an argument outside the
// declared ranges.
func (m *Machine) lazyInstance(name string) (eventDef, bool) {
	f, args, ok := m.parseInstance(name)
	if !ok {
		return eventDef{}, false
	}
	if why := f.def.argsError(args); why != "" {
		panic("gsm: " + why)
	}
	return f.def.instance(f.index, args), true
}

// paramOf returns the parameter called name, or nil.
func paramOf(params []Param, name string) *Param {
	for i := range params {
		if params[i].Name == name {
			return &params[i]
		}
	}
	return nil
}

// plainEvent returns the index of the event called name that is no instance, or -1.
func (r *Registry) plainEvent(name string) int {
	for i, ev := range r.events {
		if ev.family == 0 && ev.name == name {
			return i
		}
	}
	return -1
}

// ---- abstraction (Registry.Abstract) ----

// absEvents are the events the abstraction check runs: every event without parameters, and
// each parameterized event at every assignment of representative values to its parameters,
// in declaration order. kind[i] is the declaration (event kind) events[i] belongs to.
type absEvents struct {
	evs   []eventDef
	kind  []int
	kinds []string // the kinds' names
	m     int      // the most parameters one kind declares
}

// absArity returns m, the most parameters one event declares.
func (r *Registry) absArity() int {
	m := 0
	for i := range r.families {
		if k := len(r.families[i].params); k > m {
			m = k
		}
	}
	return m
}

// absEventList returns the events the abstraction check runs over the representatives reps.
func (r *Registry) absEventList(reps []int) absEvents {
	ae := absEvents{m: r.absArity()}
	addFamily := func(fi int) {
		f := &r.families[fi]
		k := len(ae.kinds)
		ae.kinds = append(ae.kinds, f.name)
		dom := make([][]int, len(f.params))
		for i := range dom {
			dom[i] = reps
		}
		// Every assignment outside the ranges is the same event, one that does nothing (its
		// range test fails), so it is checked once, at the first such assignment.
		noop := false
		tuples(dom, func(args []int) {
			if !f.inRange(args) {
				if noop {
					return
				}
				noop = true
			}
			ae.evs = append(ae.evs, f.rangedInstance(fi, args))
			ae.kind = append(ae.kind, k)
		})
	}
	next := 0 // the next family to place, by declaration position
	for i, ev := range r.events {
		for next < len(r.families) && r.families[next].pos <= i {
			addFamily(next)
			next++
		}
		if ev.family > 0 {
			continue
		}
		ae.kind = append(ae.kind, len(ae.kinds))
		ae.kinds = append(ae.kinds, ev.name)
		ae.evs = append(ae.evs, ev)
	}
	for ; next < len(r.families); next++ {
		addFamily(next)
	}
	return ae
}

// pairs returns the event pairs the abstraction check certifies: every pair in the default
// mode; otherwise, for each Independent declaration in order, every pair of events of the two
// kinds (for a kind with itself, each pair of its events once, and an event without
// parameters with itself as declared).
func (ae *absEvents) pairs(r *Registry) [][2]int {
	var out [][2]int
	if r.allIndependent {
		for i := 0; i < len(ae.evs); i++ {
			for j := i + 1; j < len(ae.evs); j++ {
				out = append(out, [2]int{i, j})
			}
		}
		return out
	}
	kindOf := func(name string) int {
		for k, n := range ae.kinds {
			if n == name {
				return k
			}
		}
		return -1
	}
	for _, p := range r.indepNames {
		k1, k2 := kindOf(p[0]), kindOf(p[1])
		var a, b []int
		for i, k := range ae.kind {
			if k == k1 {
				a = append(a, i)
			}
			if k == k2 {
				b = append(b, i)
			}
		}
		for _, i := range a {
			for _, j := range b {
				if k1 == k2 && (i > j || (i == j && len(a) > 1)) {
					continue
				}
				if i > j {
					out = append(out, [2]int{j, i})
				} else {
					out = append(out, [2]int{i, j})
				}
			}
		}
	}
	return out
}

// rangedInstance is the instance at args of the family with its range test conjoined to its
// guard: min <= p <= max for every parameter p, a comparison against the bounds (which
// absConstants adds to the constants). So the abstraction check runs a registry of the
// model, whose events take any integer values, and an assignment outside the ranges is an
// event that does nothing; on the values in range it is the family itself.
func (f *familyDef) rangedInstance(fi int, args []int) eventDef {
	ev := f.instance(fi, args)
	conj := make([]Pred, 0, 2*len(f.params)+1)
	for i, p := range f.params {
		conj = append(conj, Le(Lit(p.Min), Lit(args[i])), Le(Lit(args[i]), Lit(p.Max)))
	}
	if ev.guardAST != nil {
		conj = append(conj, ev.guardAST)
	}
	guard := And(conj...)
	ev.guardAST = guard
	ev.guard = func(s State) bool { return guard.holds(s) }
	return ev
}

// argsInRange reports whether an event's parameter values lie in their declared ranges
// (always, for an event without parameters).
func (r *Registry) argsInRange(ev eventDef) bool {
	return ev.family == 0 || r.families[ev.family-1].inRange(ev.args)
}

// kindName returns, for an instance of a parameterized event checked by abstraction, the
// event's signature (a representative witness stands for every value); otherwise name.
func kindName(name string, fams []EventFamily) string {
	for _, f := range fams {
		if f.Representative && strings.HasPrefix(name, f.Name+"(") && strings.HasSuffix(name, ")") {
			if _, ok := parseArgs(name[len(f.Name)+1:len(name)-1], len(f.Params)); ok {
				return f.Signature()
			}
		}
	}
	return name
}
