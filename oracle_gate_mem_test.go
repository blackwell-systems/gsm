package gsm

import (
	"runtime"
	"testing"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// The gate reads the machine's own tables: it must not build a second copy of
// them for the oracle (8 bytes per entry), only the renumbering of the
// in-domain states (12 bytes per state). On 2^16 states and 16 events (sized
// so the test stays fast under -race, as CI runs it) a copy is over 8 MiB; at
// gsm's largest machines (2^20 states, 20 events) the gate allocated 279 MiB
// before it read the tables in place.
func TestGateDoesNotCopyTheTables(t *testing.T) {
	if testing.Short() {
		t.Skip("2^16 states")
	}
	withTableOracle(t, func(oracle.Lookup) (bool, error) { return true, nil })
	m, rep, err := wideFlags(16).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	tableOracle = oracle.CheckLookup
	err = certifyMachine(m)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("certifyMachine: %v", err)
	}
	got := after.TotalAlloc - before.TotalAlloc
	t.Logf("the gate allocated %d MiB", got>>20)
	const budget = 2 << 20
	if got > budget {
		t.Fatalf("the gate allocated %d MiB; want at most %d MiB", got>>20, budget>>20)
	}
}
