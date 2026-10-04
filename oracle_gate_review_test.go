package gsm

import (
	"sync"
	"testing"

	"github.com/blackwell-systems/gsm/internal/oracle"
)

// padded: domains that are not powers of two, so in-domain ids differ from
// packed encodings.
func padded() *Registry {
	r := NewRegistry("padded")
	a := r.Int("a", 0, 4) // 5 values, 3 bits
	b := r.Enum("b", "x", "y", "z")
	r.Invariant("cap").Watches(a, b).
		Holds(func(s State) bool { return s.Get(b) != "z" || s.GetInt(a) <= 2 }).
		Repair(func(s State) State { return s.SetInt(a, 2) }).Add()
	r.Event("inc").Writes(a).Apply(func(s State) State { return s.SetInt(a, min(s.GetInt(a)+1, 4)) }).Add()
	r.Event("toz").Writes(b).Apply(func(s State) State { return s.Set(b, "z") }).Add()
	return r
}

// Every in-domain state's Apply and Normalize, read back through the oracle's
// renumbering, are the oracle's table entries (domains that are not powers of
// two, so ids and encodings differ). From the adversarial review.
func TestOracleTablesRenumberInDomainStates(t *testing.T) {
	var seen []oracle.Tables
	withTableOracle(t, func(l oracle.Lookup) (bool, error) {
		seen = append(seen, materialize(l))
		return oracle.CheckLookup(l)
	})
	m, _, err := padded().Build()
	if err != nil {
		t.Fatal(err)
	}
	tb := seen[0]
	var order []uint64
	for s := uint64(0); s < uint64(len(m.nf)); s++ {
		if m.inDomain(s) {
			order = append(order, s)
		}
	}
	if len(order) == len(m.nf) {
		t.Fatal("no padding: test is vacuous")
	}
	if len(tb.NF) != len(order) {
		t.Fatalf("NF len %d, in-domain %d", len(tb.NF), len(order))
	}
	evs := m.Events()
	for k, packed := range order {
		s := State{packed: packed, vars: m.vars}
		if got := m.Normalize(s).packed; got != order[tb.NF[k]] {
			t.Errorf("Normalize(%d) = %d, oracle NF says %d", packed, got, order[tb.NF[k]])
		}
		for _, name := range evs {
			e := m.events[name]
			if got := m.Apply(s, name).packed; got != order[tb.Step[e][k]] {
				t.Errorf("Apply(%d,%s) = %d, oracle says %d", packed, name, got, order[tb.Step[e][k]])
			}
		}
	}
}

// Builds of every kind run concurrently, each gated (run with -race). From the
// adversarial review.
func TestOracleGateConcurrentBuilds(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var r *Registry
			switch i % 3 {
			case 0:
				r = padded()
			case 1:
				r = twoCounters()
			default:
				r = unrepaired()
			}
			if i%3 == 2 {
				if _, _, err := r.BuildOrSynthesize(); err != nil {
					t.Error(err)
				}
				return
			}
			if _, rep, err := r.Build(); err != nil || rep.Assurance != AssuranceOracleTables {
				t.Error(err)
			}
			if _, rep, err := twoCounters().BuildCompositional(TrustClosureFootprints()); err != nil || rep.Assurance != AssuranceOracleComponentsTested {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

// A registry with no rules still goes through the gate once per component.
// From the adversarial review.
func TestOracleGateCompositionalWithoutRules(t *testing.T) {
	calls := 0
	withTableOracle(t, func(l oracle.Lookup) (bool, error) { calls++; return oracle.CheckLookup(l) })
	r := NewRegistry("empty")
	r.Int("a", 0, 3)
	m, rep, err := r.BuildCompositional()
	if err != nil || m == nil {
		t.Fatalf("BuildCompositional: %v", err)
	}
	if calls != rep.Components || rep.Assurance != AssuranceOracleComponents {
		t.Fatalf("oracle calls %d for %d components, Assurance %v", calls, rep.Components, rep.Assurance)
	}
}
