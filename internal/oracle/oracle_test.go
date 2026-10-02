package oracle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func identity(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// Two counters mod 3 and mod 2, one event bumping each: every pair commutes.
func counters() Tables {
	n := 6 // s = a + 3*b
	inc := func(f func(a, b int) (int, int)) []int {
		out := make([]int, n)
		for s := 0; s < n; s++ {
			a, b := f(s%3, s/3)
			out[s] = a + 3*b
		}
		return out
	}
	return Tables{
		NF: identity(n),
		Step: [][]int{
			inc(func(a, b int) (int, int) { return (a + 1) % 3, b }),
			inc(func(a, b int) (int, int) { return a, (b + 1) % 2 }),
		},
		AllPairs: true,
	}
}

func TestCheckTablesAcceptsCommutingTables(t *testing.T) {
	ok, err := CheckTables(counters())
	if err != nil || !ok {
		t.Fatalf("CheckTables(counters) = %v, %v; want true, nil", ok, err)
	}
}

func TestCheckTablesRejectsANonCommutingPair(t *testing.T) {
	// e0 sets the state to 1, e1 to 2: e0;e1 = 2 but e1;e0 = 1.
	tb := Tables{NF: identity(3), Step: [][]int{{1, 1, 1}, {2, 2, 2}}, AllPairs: true}
	if ok, err := CheckTables(tb); err != nil || ok {
		t.Fatalf("got %v, %v; want false, nil", ok, err)
	}
	// Declared independent only with itself: (e0, e1) is not checked.
	tb.AllPairs, tb.Pairs = false, [][2]int{{0, 0}}
	if ok, err := CheckTables(tb); err != nil || !ok {
		t.Fatalf("with only (0, 0) declared: got %v, %v; want true, nil", ok, err)
	}
	tb.Pairs = [][2]int{{0, 1}}
	if ok, err := CheckTables(tb); err != nil || ok {
		t.Fatalf("with (0, 1) declared: got %v, %v; want false, nil", ok, err)
	}
}

func TestCheckTablesChecksValidityAndTheZeroState(t *testing.T) {
	// NF must land on valid states: NF 0 = 1 but NF 1 = 2.
	if ok, err := CheckTables(Tables{NF: []int{1, 2, 2}, Step: [][]int{{2, 2, 2}}, AllPairs: true}); err != nil || ok {
		t.Errorf("NF not a retraction: got %v, %v; want false", ok, err)
	}
	// Every step must land on a valid state: state 1 is invalid (NF 1 = 0).
	if ok, err := CheckTables(Tables{NF: []int{0, 0}, Step: [][]int{{1, 1}}, AllPairs: true}); err != nil || ok {
		t.Errorf("step to an invalid state: got %v, %v; want false", ok, err)
	}
	// Commutation is checked on the zero state even when it is invalid: the
	// pair commutes on the valid states 1 and 2 but not on 0.
	tb := Tables{NF: []int{1, 1, 2}, Step: [][]int{{1, 1, 2}, {2, 1, 2}}, AllPairs: true}
	if ok, err := CheckTables(tb); err != nil || ok {
		t.Errorf("non-commuting on the invalid zero state: got %v, %v; want false", ok, err)
	}
	// A pair that fails only on a state neither valid nor zero (state 1, NF 1 =
	// 0) is not checked there.
	tb = Tables{NF: []int{0, 0, 2}, Step: [][]int{{0, 0, 2}, {0, 2, 2}}, AllPairs: true}
	if ok, err := CheckTables(tb); err != nil || !ok {
		t.Errorf("non-commuting only off the domain: got %v, %v; want true", ok, err)
	}
}

func TestCheckTablesRefusesMalformedInput(t *testing.T) {
	for name, tb := range map[string]Tables{
		"no states":          {NF: nil, Step: nil, AllPairs: true},
		"short row":          {NF: identity(2), Step: [][]int{{0}}, AllPairs: true},
		"negative id":        {NF: []int{0, -1}, Step: [][]int{{0, 1}}, AllPairs: true},
		"id too large":       {NF: []int{0, 1 << 31}, Step: [][]int{{0, 1}}, AllPairs: true},
		"pair past nE":       {NF: identity(2), Step: [][]int{{0, 1}}, Pairs: [][2]int{{0, 1}}},
		"pairs and AllPairs": {NF: identity(2), Step: [][]int{{0, 1}}, Pairs: [][2]int{{0, 0}}, AllPairs: true},
	} {
		if ok, err := CheckTables(tb); err == nil || ok {
			t.Errorf("%s: got %v, %v; want an input error", name, ok, err)
		}
	}
}

// The generated code fails closed by panicking; CheckTables turns any panic
// into an error and never a verdict.
func TestRunTurnsAPanicIntoAnError(t *testing.T) {
	ok, err := run(func() bool { panic("gogen: int64 overflow in add") })
	if ok || err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("got %v, %v; want false and the panic as an error", ok, err)
	}
}

// oracle_gen.go is exactly the generated file PROVENANCE pins (CI also
// regenerates it from the proof commit; this catches a local edit).
func TestGeneratedFileMatchesProvenance(t *testing.T) {
	prov, err := os.ReadFile("PROVENANCE")
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for _, line := range strings.Split(string(prov), "\n") {
		if v, ok := strings.CutPrefix(line, "GO_SHA256="); ok {
			want = strings.TrimSpace(v)
		}
	}
	src, err := os.ReadFile("oracle_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(src)); got != want {
		t.Fatalf("oracle_gen.go has sha256 %s, PROVENANCE pins %s: it was edited, or regenerated without updating PROVENANCE", got, want)
	}
}
