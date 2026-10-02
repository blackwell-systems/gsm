package gsm

import (
	"runtime"
	"testing"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// The gate reads the machine's own tables: at gsm's largest machines (2^20
// states, 20 events) it must not build a second copy of them for the oracle
// (8 bytes per entry, about 170 MB), only the renumbering of the in-domain
// states.
func TestGateDoesNotCopyTheTables(t *testing.T) {
	if testing.Short() {
		t.Skip("2^20 states")
	}
	withTableOracle(t, func(oracle.Tables) (bool, error) { return true, nil })
	m, rep, err := wideFlags(20).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	tableOracle = oracle.CheckTables
	err = certifyMachine(m)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("certifyMachine: %v", err)
	}
	got := after.TotalAlloc - before.TotalAlloc
	t.Logf("the gate allocated %d MiB", got>>20)
	const budget = 32 << 20
	if got > budget {
		t.Fatalf("the gate allocated %d MiB; want at most %d MiB", got>>20, budget>>20)
	}
}
