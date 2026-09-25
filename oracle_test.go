package gsm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestConvergenceTables_WriteAndVerify is the differential test against the
// external machine-checked checker (normalization-confluence coq/extraction).
// gsm builds a machine and writes its step tables; the verified checker then
// independently certifies they converge. If the GSM_CONVERGENCE_CHECKER env var
// points at the built checker binary, this test runs it and asserts agreement;
// otherwise it only checks the tables are written in the expected format (so the
// test is a no-op-friendly default and a real cross-check when the oracle is
// available).
func TestConvergenceTables_WriteAndVerify(t *testing.T) {
	r := gsm.NewRegistry("commuting")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)
	flag := r.Bool("flag")
	r.Invariant("a_cap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 3 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 3) }).Add()
	inc := func(name string, v gsm.Var) {
		r.Event(name).Writes(v).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
	}
	inc("inc_a", a)
	inc("inc_b", b)
	r.Event("raise_flag").Writes(flag).
		Apply(func(s gsm.State) gsm.State { return s.SetBool(flag, true) }).Add()

	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}

	path := filepath.Join(t.TempDir(), "commuting.tables")
	if err = m.WriteConvergenceTables(path); err != nil {
		t.Fatalf("WriteConvergenceTables: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("tables not written: %v", err)
	}

	checker := os.Getenv("GSM_CONVERGENCE_CHECKER")
	if checker == "" {
		t.Skip("set GSM_CONVERGENCE_CHECKER to the verified checker binary to run the differential cross-check")
	}
	out, err := exec.Command(checker, path).CombinedOutput()
	t.Logf("verified checker: %s", out)
	if err != nil {
		t.Fatalf("verified checker rejected gsm's tables (gsm said convergent): %v", err)
	}
}
