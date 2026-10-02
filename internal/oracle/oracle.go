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
//   - this file's accessors, and the caller's (Lookup). The theorem holds for
//     the functions they compute, so they must be pure and total and return
//     non-negative values: nf(s) is NF(s) and st(e)(s) is Step(e, s) for
//     in-range arguments and 0 otherwise, every entry is checked to lie in
//     0..2^31-1 first, and what the accessors read must not change while the
//     check runs.
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

// Lookup gives a machine's convergence tables by accessors instead of slices,
// so a caller passes the tables it already holds without copying them. The
// states are 0..N-1 (state 0 is the zero state) and the events 0..NE-1.
//   - NF(s) is the normal form of s, for 0 <= s < N.
//   - Step(e, s) is the state applying event e to s leads to, normalized, for
//     0 <= e < NE and 0 <= s < N.
//   - Pairs and AllPairs are as in Tables.
//
// The proof's guarantee is about the functions the accessors compute, so they
// must be pure: the same answer for the same arguments for as long as the
// check runs (no mutation of what they read meanwhile). CheckLookup calls them
// only on the arguments above, checks once that every answer lies in
// 0..2^31-1, and answers 0 for any other argument the checker asks about.
type Lookup struct {
	N, NE    int
	NF       func(s int) int
	Step     func(e, s int) int
	Pairs    [][2]int
	AllPairs bool
}

// Lookup returns accessors over t's slices. It does not check t; CheckTables
// does.
func (t Tables) Lookup() Lookup {
	return Lookup{
		N: len(t.NF), NE: len(t.Step),
		NF:    func(s int) int { return t.NF[s] },
		Step:  func(e, s int) int { return t.Step[e][s] },
		Pairs: t.Pairs, AllPairs: t.AllPairs,
	}
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
	n := len(t.NF)
	for e, row := range t.Step {
		if len(row) != n {
			return false, fmt.Errorf("oracle: step row %d has %d entries, want %d", e, len(row), n)
		}
	}
	return CheckLookup(t.Lookup())
}

// CheckLookup is CheckTables on tables given by accessors (see Lookup). It
// copies nothing: the checker reads every entry through l's accessors. An
// error means no verdict, as for CheckTables.
func CheckLookup(l Lookup) (bool, error) {
	n, ne := l.N, l.NE
	if n < 1 {
		return false, fmt.Errorf("oracle: no states (state 0 is the zero state)")
	}
	if n > maxID || ne > maxID || ne < 0 {
		return false, fmt.Errorf("oracle: %d states and %d events exceed the checker's range", n, ne)
	}
	if l.AllPairs && l.Pairs != nil {
		return false, fmt.Errorf("oracle: both AllPairs and declared Pairs given")
	}
	for s := 0; s < n; s++ {
		if x := l.NF(s); x < 0 || x > maxID {
			return false, fmt.Errorf("oracle: nf entry %d is %d, outside 0..%d", s, x, maxID)
		}
	}
	for e := 0; e < ne; e++ {
		for s := 0; s < n; s++ {
			if x := l.Step(e, s); x < 0 || x > maxID {
				return false, fmt.Errorf("oracle: step row %d entry %d is %d, outside 0..%d", e, s, x, maxID)
			}
		}
	}
	for _, p := range l.Pairs {
		if p[0] < 0 || p[0] >= ne || p[1] < 0 || p[1] >= ne {
			return false, fmt.Errorf("oracle: declared pair (%d, %d) names an event outside 0..%d", p[0], p[1], ne-1)
		}
	}

	// The accessors check_fn reads (pure, total, non-negative, see above).
	n64, ne64 := int64(n), int64(ne)
	nf := func(s int64) int64 {
		if s >= 0 && s < n64 {
			return int64(l.NF(int(s)))
		}
		return 0
	}
	zero := func(int64) int64 { return 0 }
	rows := make([]func(int64) int64, ne)
	for e := range rows {
		e := e
		rows[e] = func(s int64) int64 {
			if s >= 0 && s < n64 {
				return int64(l.Step(e, int(s)))
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
	if !l.AllPairs {
		ps := make([]*I_prod[int64, int64], len(l.Pairs))
		for i, p := range l.Pairs {
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
