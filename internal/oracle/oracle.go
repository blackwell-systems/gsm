// Package oracle runs the convergence checkers of the normalization-confluence
// proof in-process.
//
// oracle_gen.go is not written by hand. The proof repository extracts its
// verified checkers from Rocq to MiniML (coq/goextract), and its generator,
// gogen, translates them to Go. PROVENANCE records the proof commit, the prover
// image and the hashes, and CI regenerates the file from them and requires the
// same bytes.
//
// CheckTables runs check_fn, the table oracle over accessor functions, on
// accessors that read the Tables' slices in place, so the tables are never
// copied. Rocq proves that a true verdict means the tables converge, stated on
// the accessors themselves (check_fn_converges), and that check_fn decides the
// same property as check_tables and the list oracle check_fast
// (check_tables_fn, check_fast_fn). Its trusted base is:
//   - Rocq's extraction;
//   - gogen, whose prims are checked against their Rocq definitions and whose
//     output is differentially tested against the OCaml extraction;
//   - the Go toolchain;
//   - this file's accessors. The theorem holds for the functions they compute,
//     so they must be pure and total and return non-negative values: nf(s) is
//     NF[s] and st(e)(s) is Step[e][s] for in-range arguments and 0 otherwise,
//     every entry is checked to lie in 0..2^31-1 first, and the slices must not
//     change while the check runs.
package oracle

import "fmt"

// maxID bounds every state id and event index. The proof repository's front
// ends accept the same range (below 2^31), and with it every number the
// generated code computes stays far inside int64.
const maxID = 1<<31 - 1

// Tables are a machine's convergence tables over states 0..n-1, where n is
// len(NF). State 0 is the zero state.
//   - NF[s] is the normal form of s. A state s is valid when NF[s] = s.
//   - Step[e][s] is the state that applying event e to s leads to,
//     normalized.
//   - Pairs are the event pairs declared independent. Set AllPairs instead
//     (and leave Pairs nil) when every pair is.
type Tables struct {
	NF       []int
	Step     [][]int
	Pairs    [][2]int
	AllPairs bool
}

// CheckTables reports whether the tables have the property gsm's Build checks:
//   - every normal form and every step lands on a valid state;
//   - every declared pair commutes on every valid state and on the zero state.
//
// The verdict is check_fn's. An error means no verdict:
//   - the tables are malformed (no states, a row of the wrong length, an id
//     outside 0..2^31-1, a pair naming a missing event);
//   - or the generated code stopped (it panics rather than compute a wrong
//     number).
//
// A caller that certifies convergence must treat an error as a rejection.
func CheckTables(t Tables) (bool, error) {
	n, ne := len(t.NF), len(t.Step)
	if n < 1 {
		return false, fmt.Errorf("oracle: no states (state 0 is the zero state)")
	}
	if n > maxID || ne > maxID {
		return false, fmt.Errorf("oracle: %d states and %d events exceed the checker's range", n, ne)
	}
	if t.AllPairs && t.Pairs != nil {
		return false, fmt.Errorf("oracle: both AllPairs and declared Pairs given")
	}
	ids := func(what string, xs []int) error {
		for i, x := range xs {
			if x < 0 || x > maxID {
				return fmt.Errorf("oracle: %s entry %d is %d, outside 0..%d", what, i, x, maxID)
			}
		}
		return nil
	}
	if err := ids("nf", t.NF); err != nil {
		return false, err
	}
	for e, row := range t.Step {
		if len(row) != n {
			return false, fmt.Errorf("oracle: step row %d has %d entries, want %d", e, len(row), n)
		}
		if err := ids(fmt.Sprintf("step row %d", e), row); err != nil {
			return false, err
		}
	}
	for _, p := range t.Pairs {
		if p[0] < 0 || p[0] >= ne || p[1] < 0 || p[1] >= ne {
			return false, fmt.Errorf("oracle: declared pair (%d, %d) names an event outside 0..%d", p[0], p[1], ne-1)
		}
	}

	// The accessors check_fn reads (pure, total, non-negative, see above).
	n64, ne64 := int64(n), int64(ne)
	nf := func(s int64) int64 {
		if s >= 0 && s < n64 {
			return int64(t.NF[s])
		}
		return 0
	}
	zero := func(int64) int64 { return 0 }
	rows := make([]func(int64) int64, ne)
	for e := range t.Step {
		row := t.Step[e]
		rows[e] = func(s int64) int64 {
			if s >= 0 && s < n64 {
				return int64(row[s])
			}
			return 0
		}
	}
	st := func(e int64) func(int64) int64 {
		if e >= 0 && e < ne64 {
			return rows[e]
		}
		return zero
	}
	pairs := K_None[*I_list[*I_prod[int64, int64]]]()
	if !t.AllPairs {
		ps := make([]*I_prod[int64, int64], len(t.Pairs))
		for i, p := range t.Pairs {
			ps[i] = K_Pair(int64(p[0]), int64(p[1]))
		}
		pairs = K_Some(list(ps))
	}
	return run(func() bool { return F_check_fn(n64, ne64, nf, st, pairs) })
}

// run calls a generated checker and turns a panic into an error, so a stop
// inside the generated code is never read as a verdict.
func run(check func() bool) (ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok, err = false, fmt.Errorf("oracle: the checker stopped: %v", r)
		}
	}()
	return check(), nil
}

func list[A any](xs []A) *I_list[A] {
	l := K_Nil[A]()
	for i := len(xs) - 1; i >= 0; i-- {
		l = K_Cons(xs[i], l)
	}
	return l
}
