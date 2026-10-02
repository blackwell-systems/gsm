package gsm

// Serialize a combinator machine to the S-expression format read by the verified
// AST oracle (normalization-confluence/coq/extraction/astchecker). That checker,
// extracted from a machine-checked Coq proof, recomputes convergence straight from
// these rules, so it does not have to trust gsm's Go verification or its tables.
//
// This is faithful for the fragment the Coq model covers: variables over
// min..min+domain-1 (the state stores the raw offset), predicates built from
// <=,<,==,>=,>,!=,and,or,not, transforms built from Set/Add/Sub, and events with an
// optional guard. The oracle's arithmetic is gsm's: signed integers, signed
// comparisons, and a write that clamps into the variable's range. Two things it
// does not model, and instead refuses to certify (exit 1, "outside the certified
// fragment"), so it never misreads them: an expression whose value could exceed
// 2^31-1 in magnitude, where Go's int may wrap (it wraps at 2^31 on 32-bit
// platforms); and a write that could store a negative value into a two-valued
// variable with minimum 0, where a Bool stores (value != 0) but the oracle clamps.
// The format carries no variable kinds, so the oracle applies that second rule to
// every such variable, Bool or not. WriteMachineAST returns an error (rather than
// emit something the oracle would misread) whenever a rule falls outside the
// grammar, so a passing differential test always compares like semantics.

import (
	"fmt"
	"io"
	"strings"
)

// ownVar reports whether v is exactly this registry's variable at v.index. The
// rules format names a variable by index only, so a Var from another registry
// (read and clamped with that registry's layout, minimum and domain) would
// otherwise be exported as this registry's variable: a different machine.
func (r *Registry) ownVar(v Var) error {
	if v.index < 0 || v.index >= len(r.vars) {
		return fmt.Errorf("gsm: variable %q (index %d) is not declared in registry %q", v.name, v.index, r.name)
	}
	if !sameVar(r.vars[v.index], v) {
		return fmt.Errorf("gsm: variable %q (index %d) belongs to another registry, not %q", v.name, v.index, r.name)
	}
	return nil
}

func (r *Registry) exprSexp(e Expr) (string, error) {
	switch x := e.(type) {
	case varRef:
		if err := r.ownVar(x.v); err != nil {
			return "", err
		}
		return fmt.Sprintf("(var %d)", x.v.index), nil
	case litE:
		return fmt.Sprintf("(lit %d)", x.n), nil
	case binE:
		a, err := r.exprSexp(x.a)
		if err != nil {
			return "", err
		}
		b, err := r.exprSexp(x.b)
		if err != nil {
			return "", err
		}
		var op string
		switch x.op {
		case "+":
			op = "add"
		case "-":
			op = "sub"
		default:
			return "", fmt.Errorf("gsm: cannot export operator %q", x.op)
		}
		return fmt.Sprintf("(%s %s %s)", op, a, b), nil
	default:
		return "", fmt.Errorf("gsm: cannot export expression of type %T", e)
	}
}

func (r *Registry) predSexp(p Pred) (string, error) {
	switch x := p.(type) {
	case cmpP:
		a, err := r.exprSexp(x.a)
		if err != nil {
			return "", err
		}
		b, err := r.exprSexp(x.b)
		if err != nil {
			return "", err
		}
		var op string
		switch x.op {
		case "<=":
			op = "le"
		case "<":
			op = "lt"
		case "==":
			op = "eq"
		case ">=": // a >= b  <=>  b <= a
			return fmt.Sprintf("(le %s %s)", b, a), nil
		case ">": // a > b  <=>  b < a
			return fmt.Sprintf("(lt %s %s)", b, a), nil
		case "!=": // a != b  <=>  not (a == b)
			return fmt.Sprintf("(not (eq %s %s))", a, b), nil
		default:
			return "", fmt.Errorf("gsm: cannot export comparison %q", x.op)
		}
		return fmt.Sprintf("(%s %s %s)", op, a, b), nil
	case boolP:
		var head string
		switch x.op {
		case "and":
			head = "and"
		case "or":
			head = "or"
		default:
			return "", fmt.Errorf("gsm: cannot export boolean %q (AST oracle models 'and'/'or'/'not')", x.op)
		}
		parts := make([]string, 0, len(x.ps))
		for _, q := range x.ps {
			s, err := r.predSexp(q)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + head + " " + strings.Join(parts, " ") + ")", nil
	case notP:
		s, err := r.predSexp(x.p)
		if err != nil {
			return "", err
		}
		return "(not " + s + ")", nil
	default:
		return "", fmt.Errorf("gsm: cannot export predicate of type %T", p)
	}
}

func (r *Registry) transformSexp(t Transform) (string, error) {
	parts := make([]string, 0, len(t))
	for _, a := range t {
		if err := r.ownVar(a.v); err != nil {
			return "", err
		}
		e, err := r.exprSexp(a.e)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("(set %d %s)", a.v.index, e))
	}
	return "(do " + strings.Join(parts, " ") + ")", nil
}

// WriteMachineAST serializes this registry's combinator rules to the S-expression
// format the verified AST oracle reads. It fails if any rule was not declared via
// the combinator vocabulary (no retained AST), if a guard is a closure, or if two
// events share a name (the checker would certify the events by position while the
// runtime resolves a name to only one of them).
func (r *Registry) WriteMachineAST(w io.Writer) error {
	if err := r.checkNames(); err != nil {
		return err
	}
	doms := make([]string, len(r.vars))
	mins := make([]string, len(r.vars))
	for i, v := range r.vars {
		if v.index != i {
			return fmt.Errorf("gsm: variable %q index %d out of order", v.name, v.index)
		}
		doms[i] = fmt.Sprintf("%d", v.domain)
		mins[i] = fmt.Sprintf("%d", v.min)
	}
	if _, err := fmt.Fprintf(w, "(doms %s)\n", strings.Join(doms, " ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "(mins %s)\n", strings.Join(mins, " ")); err != nil {
		return err
	}
	for _, inv := range r.invariants {
		if inv.predAST == nil || inv.repairAST == nil {
			return fmt.Errorf("gsm: invariant %q has no combinator AST (declare it with DeclInvariant to export)", inv.name)
		}
		p, err := r.predSexp(inv.predAST)
		if err != nil {
			return fmt.Errorf("invariant %q: %w", inv.name, err)
		}
		t, err := r.transformSexp(inv.repairAST)
		if err != nil {
			return fmt.Errorf("invariant %q repair: %w", inv.name, err)
		}
		if _, err := fmt.Fprintf(w, "(inv %s %s)\n", p, t); err != nil {
			return err
		}
	}
	for _, ev := range r.events {
		if ev.effectAST == nil {
			return fmt.Errorf("gsm: event %q has no combinator AST (declare it with DeclEvent to export)", ev.name)
		}
		if ev.guard != nil && ev.guardAST == nil {
			return fmt.Errorf("gsm: event %q has a closure guard with no AST (declare it with DeclEventGuarded to export)", ev.name)
		}
		t, err := r.transformSexp(ev.effectAST)
		if err != nil {
			return fmt.Errorf("event %q: %w", ev.name, err)
		}
		if ev.guardAST != nil {
			g, err := r.predSexp(ev.guardAST)
			if err != nil {
				return fmt.Errorf("event %q guard: %w", ev.name, err)
			}
			if _, err := fmt.Fprintf(w, "(evwhen %s %s)\n", g, t); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "(ev %s)\n", t); err != nil {
			return err
		}
	}
	return nil
}

// WriteDeclaredPairs writes the event pairs CC is checked for, in the pairs-file
// format the rules oracle takes next to the machine (astchecker <machine>
// <pairs>): "pairs all" when no pair was declared independent, otherwise
// "pairs k a1 b1 ... ak bk" with event indices in the order WriteMachineAST
// emits the events. It is a separate file, not part of WriteMachineAST's output,
// so PolicyBytes and PolicyDigest do not depend on it. Without it the oracle
// checks every pair, the stronger property, so a verdict obtained without the
// pairs file holds for any declaration. It fails if two events share a name, since
// Independent resolved each name to one of them only.
func (r *Registry) WriteDeclaredPairs(w io.Writer) error {
	if err := r.checkNames(); err != nil {
		return err
	}
	if r.allIndependent {
		_, err := io.WriteString(w, "pairs all\n")
		return err
	}
	ps := r.ccPairs()
	var b strings.Builder
	fmt.Fprintf(&b, "pairs %d", len(ps))
	for _, p := range ps {
		fmt.Fprintf(&b, " %d %d", p[0], p[1])
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}
