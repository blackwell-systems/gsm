package gsm

// Serialize a combinator machine to the S-expression format read by the verified
// AST oracle (normalization-confluence/coq/extraction/astchecker). That checker,
// extracted from a machine-checked Coq proof, recomputes convergence straight from
// these rules, so it does not have to trust gsm's Go verification or its tables.
//
// This is faithful only for the fragment the Coq model covers: variables in raw
// 0..domain-1 space (min=0), predicates built from <=,<,==,and,not, transforms
// built from Set/Add/Sub, and unguarded events. WriteMachineAST returns an error
// (rather than emit something the oracle would misread) whenever a rule falls
// outside that fragment, so a passing differential test always compares like semantics.

import (
	"fmt"
	"io"
	"strings"
)

func exprSexp(e Expr) (string, error) {
	switch x := e.(type) {
	case varRef:
		return fmt.Sprintf("(var %d)", x.v.index), nil
	case litE:
		if x.n < 0 {
			return "", fmt.Errorf("gsm: cannot export negative literal %d (AST oracle is over naturals)", x.n)
		}
		return fmt.Sprintf("(lit %d)", x.n), nil
	case binE:
		a, err := exprSexp(x.a)
		if err != nil {
			return "", err
		}
		b, err := exprSexp(x.b)
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

func predSexp(p Pred) (string, error) {
	switch x := p.(type) {
	case cmpP:
		a, err := exprSexp(x.a)
		if err != nil {
			return "", err
		}
		b, err := exprSexp(x.b)
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
		if x.op != "and" {
			return "", fmt.Errorf("gsm: cannot export boolean %q (AST oracle models only 'and'/'not')", x.op)
		}
		parts := make([]string, 0, len(x.ps))
		for _, q := range x.ps {
			s, err := predSexp(q)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(and " + strings.Join(parts, " ") + ")", nil
	case notP:
		s, err := predSexp(x.p)
		if err != nil {
			return "", err
		}
		return "(not " + s + ")", nil
	default:
		return "", fmt.Errorf("gsm: cannot export predicate of type %T", p)
	}
}

func transformSexp(t Transform) (string, error) {
	parts := make([]string, 0, len(t))
	for _, a := range t {
		e, err := exprSexp(a.e)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("(set %d %s)", a.v.index, e))
	}
	return "(do " + strings.Join(parts, " ") + ")", nil
}

// WriteMachineAST serializes this registry's combinator rules to the S-expression
// format the verified AST oracle reads. It fails if any rule was not declared via
// the combinator vocabulary (no retained AST), if any event is guarded, or if any
// variable is outside the oracle's min=0 raw-value fragment.
func (r *Registry) WriteMachineAST(w io.Writer) error {
	doms := make([]string, len(r.vars))
	for i, v := range r.vars {
		if v.index != i {
			return fmt.Errorf("gsm: variable %q index %d out of order", v.name, v.index)
		}
		if v.min != 0 {
			return fmt.Errorf("gsm: cannot export variable %q with min=%d (AST oracle uses raw 0..domain-1 space)", v.name, v.min)
		}
		doms[i] = fmt.Sprintf("%d", v.domain)
	}
	if _, err := fmt.Fprintf(w, "(doms %s)\n", strings.Join(doms, " ")); err != nil {
		return err
	}
	for _, inv := range r.invariants {
		if inv.predAST == nil || inv.repairAST == nil {
			return fmt.Errorf("gsm: invariant %q has no combinator AST (declare it with DeclInvariant to export)", inv.name)
		}
		p, err := predSexp(inv.predAST)
		if err != nil {
			return fmt.Errorf("invariant %q: %w", inv.name, err)
		}
		t, err := transformSexp(inv.repairAST)
		if err != nil {
			return fmt.Errorf("invariant %q repair: %w", inv.name, err)
		}
		if _, err := fmt.Fprintf(w, "(inv %s %s)\n", p, t); err != nil {
			return err
		}
	}
	for _, ev := range r.events {
		if ev.guard != nil {
			return fmt.Errorf("gsm: event %q is guarded (AST oracle models only unguarded events)", ev.name)
		}
		if ev.effectAST == nil {
			return fmt.Errorf("gsm: event %q has no combinator AST (declare it with DeclEvent to export)", ev.name)
		}
		t, err := transformSexp(ev.effectAST)
		if err != nil {
			return fmt.Errorf("event %q: %w", ev.name, err)
		}
		if _, err := fmt.Fprintf(w, "(ev %s)\n", t); err != nil {
			return err
		}
	}
	return nil
}
