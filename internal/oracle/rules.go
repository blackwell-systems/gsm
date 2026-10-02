package oracle

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The rules oracle: checkBuild from the normalization-confluence proof
// (AstChecker.v), generated as Go like the table oracle. It reads a combinator
// machine's rules, not its tables: it re-derives each event's step by
// evaluating the expression trees (apply, then repair until every invariant
// holds) and checks the property Build checks. Rocq proves a true verdict
// means the machine converges (checkBuild_converges), for the fragment it
// models, which bounded and signSafe decide:
//   - every value an expression can take stays within |2^31-1|;
//   - no write can store a negative value into a two-valued variable with
//     minimum 0 (a gsm Bool stores value != 0; the model clamps).
//
// CheckRules takes the machine in the format Registry.WriteMachineAST writes
// and the pairs in the format Registry.WriteDeclaredPairs writes. The parser is
// a port of the proof repository's front ends (extraction/ast_main.ml,
// coq/goextract/cmd/rulecheck), with the same validation.

// RulesVerdict is the rules oracle's answer.
type RulesVerdict int

const (
	// RulesNoVerdict is the zero value: no verdict (CheckRules returned an
	// error).
	RulesNoVerdict RulesVerdict = iota
	// RulesCertified: the machine converges, from its rules.
	RulesCertified
	// RulesOutsideFragment: the machine is outside the fragment the proof
	// models (see bounded and signSafe), so there is no verdict about it.
	RulesOutsideFragment
	// RulesNotTerminating: repair does not terminate from some state (WFC).
	RulesNotTerminating
	// RulesNotCommuting: a declared pair does not commute on a valid state or
	// the zero state.
	RulesNotCommuting
)

func (v RulesVerdict) String() string {
	switch v {
	case RulesCertified:
		return "certified"
	case RulesOutsideFragment:
		return "outside the rules oracle's fragment"
	case RulesNotTerminating:
		return "repair does not terminate (WFC)"
	case RulesNotCommuting:
		return "a declared pair does not commute"
	}
	return "no verdict"
}

// RulesResult is CheckRules' answer: the verdict, and for RulesOutsideFragment
// which condition failed.
type RulesResult struct {
	Verdict RulesVerdict
	Reason  string
}

// CheckRules runs the rules oracle. An error means no verdict (the input does
// not parse, or the generated code stopped).
func CheckRules(machine, pairs string) (RulesResult, error) {
	var p rulesParser
	var m *I_machine
	var ne int
	if err := catch(func() { m, ne = p.machine(parseAll(tokenize(machine))) }); err != nil {
		return RulesResult{}, fmt.Errorf("oracle: rules: %v", err)
	}
	var ps *I_option[*I_list[*I_prod[int64, int64]]]
	if err := catch(func() { ps = p.pairs(pairs, ne) }); err != nil {
		return RulesResult{}, fmt.Errorf("oracle: pairs: %v", err)
	}
	if p.outOfFragment {
		return RulesResult{RulesOutsideFragment, "a literal, minimum or maximum exceeds |2147483647|"}, nil
	}
	var verdict RulesResult
	if _, err := run(func() bool {
		switch {
		case !F_bounded(m):
			verdict = RulesResult{RulesOutsideFragment, "some expression can exceed |2147483647|"}
		case !F_signSafe(m):
			verdict = RulesResult{RulesOutsideFragment, "a write can store a negative value into a two-valued variable with minimum 0"}
		case !F_wfc(m):
			verdict = RulesResult{Verdict: RulesNotTerminating}
		case !F_checkBuild(m, ps):
			verdict = RulesResult{Verdict: RulesNotCommuting}
		default:
			verdict = RulesResult{Verdict: RulesCertified}
		}
		return true
	}); err != nil {
		return RulesResult{}, err
	}
	return verdict, nil
}

const maxAbs = 2147483647

type sexp struct {
	atom   string
	list   []sexp
	isList bool
}

type parseError string

func failf(f string, a ...any) { panic(parseError(fmt.Sprintf(f, a...))) }

// catch runs f and turns any panic into an error: a parseError is the input's
// fault, anything else a stop in this code, and either way there is no
// verdict (fail closed).
func catch(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseError); ok {
				err = errors.New(string(pe))
			} else {
				err = fmt.Errorf("the reader stopped: %v", r)
			}
		}
	}()
	f()
	return nil
}

func tokenize(s string) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', ')':
			flush()
			out = append(out, string(c))
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			buf.WriteByte(c)
		}
	}
	flush()
	return out
}

func parseAll(toks []string) []sexp {
	pos := 0
	var parse func() sexp
	parse = func() sexp {
		if pos >= len(toks) {
			failf("unexpected end of input")
		}
		t := toks[pos]
		pos++
		switch t {
		case "(":
			l := sexp{isList: true}
			for {
				if pos >= len(toks) {
					failf("unterminated (")
				}
				if toks[pos] == ")" {
					pos++
					return l
				}
				l.list = append(l.list, parse())
			}
		case ")":
			failf("unexpected )")
		}
		return sexp{atom: t}
	}
	var forms []sexp
	for pos < len(toks) {
		forms = append(forms, parse())
	}
	return forms
}

// decimal checks an optional '-' then 1 to maxDigits digits; it returns the
// digit count.
func decimal(s string, maxDigits int) int {
	start := 0
	if len(s) > 0 && s[0] == '-' {
		start = 1
	}
	if len(s) == start || len(s)-start > maxDigits {
		failf("expected decimal integer, got %s", s)
	}
	for _, c := range s[start:] {
		if c < '0' || c > '9' {
			failf("expected decimal integer, got %s", s)
		}
	}
	return len(s) - start
}

func intOf(s string) int64 {
	decimal(s, 10)
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v > maxAbs || v < -maxAbs {
		failf("integer out of range (|n| <= 2147483647): %s", s)
	}
	return v
}

func natOf(s string) int64 {
	v := intOf(s)
	if v < 0 {
		failf("expected non-negative integer, got %s", s)
	}
	return v
}

type rulesParser struct {
	nvars         int64
	outOfFragment bool
}

func (p *rulesParser) valueOf(s string) int64 {
	if decimal(s, 19) > 10 {
		p.outOfFragment = true
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v > maxAbs || v < -maxAbs {
		p.outOfFragment = true
		return 0
	}
	return v
}

func (p *rulesParser) varIndex(s string) int64 {
	i := natOf(s)
	if p.nvars < 0 {
		failf("doms must come before any rule")
	}
	if i >= p.nvars {
		failf("variable index %d out of range (%d variables)", i, p.nvars)
	}
	return i
}

func head(x sexp) string {
	if x.isList && len(x.list) > 0 && !x.list[0].isList {
		return x.list[0].atom
	}
	return ""
}

func (p *rulesParser) expr(x sexp) *I_expr {
	l := x.list
	switch {
	case head(x) == "var" && len(l) == 2 && !l[1].isList:
		return K_EVar(p.varIndex(l[1].atom))
	case head(x) == "lit" && len(l) == 2 && !l[1].isList:
		return K_ELit(p.valueOf(l[1].atom))
	case head(x) == "add" && len(l) == 3:
		return K_EAdd(p.expr(l[1]), p.expr(l[2]))
	case head(x) == "sub" && len(l) == 3:
		return K_ESub(p.expr(l[1]), p.expr(l[2]))
	}
	failf("malformed expr")
	return nil
}

func (p *rulesParser) pred(x sexp) *I_pred {
	l := x.list
	preds := func() *I_list[*I_pred] {
		var ps []*I_pred
		for _, q := range l[1:] {
			ps = append(ps, p.pred(q))
		}
		return list(ps)
	}
	switch {
	case head(x) == "le" && len(l) == 3:
		return K_PLe(p.expr(l[1]), p.expr(l[2]))
	case head(x) == "lt" && len(l) == 3:
		return K_PLt(p.expr(l[1]), p.expr(l[2]))
	case head(x) == "eq" && len(l) == 3:
		return K_PEq(p.expr(l[1]), p.expr(l[2]))
	case head(x) == "and":
		return K_PAnd(preds())
	case head(x) == "or":
		return K_POr(preds())
	case head(x) == "not" && len(l) == 2:
		return K_PNot(p.pred(l[1]))
	}
	failf("malformed pred")
	return nil
}

type assign = *I_prod[int64, *I_expr]
type gevent = *I_prod[*I_pred, *I_list[assign]]

func (p *rulesParser) transform(x sexp) *I_list[assign] {
	if head(x) != "do" {
		failf("malformed transform (expected (do ...))")
	}
	var as []assign
	for _, a := range x.list[1:] {
		if head(a) != "set" || len(a.list) != 3 || a.list[1].isList {
			failf("malformed assign")
		}
		i := p.varIndex(a.list[1].atom)
		as = append(as, K_Pair(i, p.expr(a.list[2])))
	}
	return list(as)
}

func ints(xs []sexp, conv func(string) int64) []int64 {
	var out []int64
	for _, x := range xs {
		if x.isList {
			failf("expected integer")
		}
		out = append(out, conv(x.atom))
	}
	return out
}

// machine builds the machine and returns it with its number of events.
func (p *rulesParser) machine(forms []sexp) (*I_machine, int) {
	p.nvars, p.outOfFragment = -1, false
	var doms, mins []int64
	haveDoms, haveMins := false, false
	var invs, evs []gevent
	for _, f := range forms {
		l := f.list
		switch {
		case head(f) == "doms":
			ds := ints(l[1:], natOf)
			for _, d := range ds {
				if d < 1 {
					failf("every domain must be at least 1")
				}
			}
			if haveDoms {
				failf("doms given twice")
			}
			doms, haveDoms = ds, true
			p.nvars = int64(len(ds))
		case head(f) == "mins":
			ms := ints(l[1:], p.valueOf)
			if haveMins {
				failf("mins given twice")
			}
			mins, haveMins = ms, true
		case head(f) == "inv" && len(l) == 3:
			invs = append(invs, K_Pair(p.pred(l[1]), p.transform(l[2])))
		case head(f) == "ev" && len(l) == 2:
			evs = append(evs, K_Pair(K_PAnd(K_Nil[*I_pred]()), p.transform(l[1])))
		case head(f) == "evwhen" && len(l) == 3:
			evs = append(evs, K_Pair(p.pred(l[1]), p.transform(l[2])))
		default:
			failf("unknown top-level form (expected doms/mins/inv/ev/evwhen)")
		}
	}
	if !haveDoms {
		failf("missing doms")
	}
	if !haveMins {
		mins = make([]int64, len(doms))
	}
	if len(mins) != len(doms) {
		failf("mins must give one minimum per variable")
	}
	for i, d := range doms {
		if v := mins[i] + d - 1; v > maxAbs || v < -maxAbs {
			p.outOfFragment = true
		}
	}
	return K_Build_machine(list(doms), list(mins), list(invs), list(evs)), len(evs)
}

// pairs reads the declared pairs: None for every pair.
func (p *rulesParser) pairs(content string, nevents int) *I_option[*I_list[*I_prod[int64, int64]]] {
	toks := strings.Fields(content)
	if len(toks) == 2 && toks[0] == "pairs" && toks[1] == "all" {
		return K_None[*I_list[*I_prod[int64, int64]]]()
	}
	if len(toks) < 2 || toks[0] != "pairs" {
		failf("expected 'pairs all' or 'pairs k a1 b1 ...'")
	}
	k := natOf(toks[1])
	rest := toks[2:]
	if int64(len(rest)) != 2*k {
		failf("expected %d pair entries, got %d", 2*k, len(rest))
	}
	var ps []*I_prod[int64, int64]
	for i := 0; i+1 < len(rest); i += 2 {
		a, b := natOf(rest[i]), natOf(rest[i+1])
		if a >= int64(nevents) || b >= int64(nevents) {
			failf("declared pair (%d, %d) names an event past the %d events", a, b, nevents)
		}
		ps = append(ps, K_Pair(a, b))
	}
	return K_Some(list(ps))
}
