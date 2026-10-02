package gsm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// The rules oracle must use gsm's arithmetic: Go's signed int, not Coq's
// natural numbers. These machines are ones where the two differ. In each, gsm
// rejects the machine and the oracle must reject it too.

func exportAST(t *testing.T, r *gsm.Registry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	return buf.Bytes()
}

// TestMachineAST_SignedSubGuard is the stage-0 probe: the guard a - b < 0 holds
// at a=0, b=1 in Go, so mark and seta do not commute there. Under truncating
// subtraction the guard is never true and the oracle certified the machine.
func TestMachineAST_SignedSubGuard(t *testing.T) {
	r := gsm.NewRegistry("signed-sub-guard")
	a := r.Int("a", 0, 1)
	b := r.Int("b", 0, 1)
	c := r.Int("c", 0, 1)
	r.DeclEventGuarded("mark", gsm.Lt(gsm.Sub(gsm.V(a), gsm.V(b)), gsm.Lit(0)), gsm.Do(gsm.Set(c, gsm.Lit(1))))
	r.DeclEvent("seta", gsm.Do(gsm.Set(a, gsm.Lit(1))))

	_, rep, err := r.Build()
	if err == nil || rep == nil || rep.CC {
		t.Fatalf("gsm must reject (CC fails at a=0,b=1), got err=%v report=%v", err, rep)
	}
	machine := exportAST(t, r)
	code, out := runOracle(t, oracleBinary(t, envASTOracle), "signed-sub-guard.machine", machine)
	if code != 1 {
		t.Fatalf("rules oracle must reject (exit 1), got exit %d:\n%s\nmachine:\n%s", code, out, machine)
	}
}

// TestMachineAST_BoolWriteOfNegative: a Bool write stores (value != 0), so
// flag := a - b sets flag when a - b = -1. The rules format has no variable
// kinds, and the oracle's write clamps, so the oracle must refuse to certify
// any write that could store a negative value into a two-valued variable with
// minimum 0. Before, it stored 0 and certified this machine.
func TestMachineAST_BoolWriteOfNegative(t *testing.T) {
	r := gsm.NewRegistry("bool-write-negative")
	a := r.Int("a", 0, 1)
	b := r.Int("b", 1, 2)
	flag := r.Bool("flag")
	r.DeclEvent("mark", gsm.Do(gsm.Set(flag, gsm.Sub(gsm.V(a), gsm.V(b)))))
	r.DeclEvent("seta", gsm.Do(gsm.Set(a, gsm.Lit(1))))

	_, rep, err := r.Build()
	if err == nil || rep == nil || rep.CC {
		t.Fatalf("gsm must reject (CC fails at a=0,b=1), got err=%v report=%v", err, rep)
	}
	machine := exportAST(t, r)
	code, out := runOracle(t, oracleBinary(t, envASTOracle), "bool-write-negative.machine", machine)
	if code != 1 {
		t.Fatalf("rules oracle must reject (exit 1), got exit %d:\n%s\nmachine:\n%s", code, out, machine)
	}
}

// TestMachineAST_NegativeMinAndLiteral: negative minimums and literals are part
// of the exported fragment. This machine converges in gsm and must be certified.
func TestMachineAST_NegativeMinAndLiteral(t *testing.T) {
	r := gsm.NewRegistry("negative-values")
	temp := r.Int("temp", -3, 2)
	heat := r.Bool("heat")
	// Invariant: temp <= 1; repair by lowering it to 1. warm only fires while
	// temp > -2, so the zero state (temp = -3) is a fixed point of warm.
	r.DeclInvariant("not_too_hot", gsm.Le(gsm.V(temp), gsm.Lit(1)), gsm.Do(gsm.Set(temp, gsm.Lit(1))))
	r.DeclEventGuarded("warm", gsm.Gt(gsm.V(temp), gsm.Lit(-2)), gsm.Do(gsm.Set(temp, gsm.Add(gsm.V(temp), gsm.Lit(1)))))
	r.DeclEvent("heat_on", gsm.Do(gsm.Set(heat, gsm.Lit(1))))

	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	machine := exportAST(t, r)
	if !strings.Contains(string(machine), "(mins -3 0)") {
		t.Fatalf("expected the negative minimum in the export:\n%s", machine)
	}
	code, out := runOracle(t, oracleBinary(t, envASTOracle), "negative-values.machine", machine)
	if code != 0 {
		t.Fatalf("rules oracle must certify (exit 0), got exit %d:\n%s\nmachine:\n%s", code, out, machine)
	}
}

// TestMachineAST_RefusesPossibleOverflow: gsm's int is 32 bits on 32-bit
// platforms, and the oracle certifies only machines whose expressions provably
// stay within 2^31-1 in magnitude. This machine converges in gsm on 64-bit
// platforms, but the oracle must not certify it.
func TestMachineAST_RefusesPossibleOverflow(t *testing.T) {
	r := gsm.NewRegistry("overflow")
	a := r.Int("a", 0, 3)
	r.DeclEvent("big", gsm.Do(gsm.Set(a, gsm.Add(gsm.Lit(2147483647), gsm.V(a)))))
	machine := exportAST(t, r)
	code, out := runOracle(t, oracleBinary(t, envASTOracle), "overflow.machine", machine)
	if code != 1 || !strings.Contains(out, "outside the certified fragment") {
		t.Fatalf("rules oracle must refuse to certify (exit 1, outside the fragment), got exit %d:\n%s", code, out)
	}
}
