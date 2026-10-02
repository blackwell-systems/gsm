package gsm

import "testing"

// recordMachines collects what machineObserver sees (the example-machine gate's
// hook for machines made without Build) for one test.
func recordMachines(t *testing.T) *[]string {
	t.Helper()
	saved := machineObserver
	var got []string
	machineObserver = func(kind string, r *Registry, _ *Machine) { got = append(got, kind+" "+r.name) }
	t.Cleanup(func() { machineObserver = saved })
	return &got
}

// The gate records the machines a program receives, not synthesis candidates:
// SynthesizeWith builds a candidate to certify it, and that one is not handed out.
func TestGateRecordsOnlyReceivedSynthesizedMachines(t *testing.T) {
	t.Run("certified", func(t *testing.T) {
		got := recordMachines(t)
		m, syn, err := unrepaired().BuildOrSynthesize()
		if err != nil || m == nil || syn == nil {
			t.Fatalf("BuildOrSynthesize: %v, %v, %v", m, syn, err)
		}
		if len(*got) != 1 || (*got)[0] != "synthesized "+m.name {
			t.Fatalf("recorded %q, want one synthesized machine", *got)
		}
	})
	t.Run("not handed out", func(t *testing.T) {
		got := recordMachines(t)
		if _, err := unrepaired().SynthesizeWith(); err != nil {
			t.Fatal(err)
		}
		if len(*got) != 0 {
			t.Fatalf("recorded %q for a synthesis whose machine was never asked for", *got)
		}
	})
	t.Run("refused", func(t *testing.T) {
		withTableOracle(t, rejecting)
		got := recordMachines(t)
		if m, _, err := unrepaired().BuildOrSynthesize(); err == nil || m != nil {
			t.Fatalf("BuildOrSynthesize with a rejecting oracle: %v, %v", m, err)
		}
		if len(*got) != 0 {
			t.Fatalf("recorded %q for a candidate the oracle refused", *got)
		}
	})
}
