package gsm_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// The table oracle must certify gsm's largest machines: 20 bits, 2^20 states.
// The checker pinned before normalization-confluence's check_fast died with
// Stack_overflow (exit 2) here under OCaml 4.14 native code.
func TestConvergenceTables_LargestMachine(t *testing.T) {
	checker := oracleBinary(t, envTableOracle)
	if testing.Short() {
		t.Skip("builds a 2^20-state machine")
	}
	r := gsm.NewRegistry("twenty_flags")
	flags := make([]gsm.Var, 20)
	for i := range flags {
		flags[i] = r.Bool(fmt.Sprintf("f%d", i))
	}
	for _, i := range []int{0, 19} {
		f := flags[i]
		r.Event(fmt.Sprintf("set_f%d", i)).Writes(f).
			Apply(func(s gsm.State) gsm.State { return s.SetBool(f, true) }).Add()
	}
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	path := filepath.Join(t.TempDir(), "twenty.tables")
	if err = m.WriteConvergenceTables(path); err != nil {
		t.Fatalf("WriteConvergenceTables: %v", err)
	}
	out, err := exec.Command(checker, path).CombinedOutput()
	t.Logf("verified checker: %s", out)
	if err != nil {
		t.Fatalf("verified checker did not certify a 2^20-state machine gsm built: %v", err)
	}
}
