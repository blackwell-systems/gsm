//go:build gsmgate

package gsm

// The example-machine gate (internal/gate, .github/oracle/README.md). Only a build
// with -tags gsmgate compiles this file. Run with GSM_GATE_DIR set, such a build
// records every machine the program makes (each Build result, each synthesized
// and each compositional machine) as the extracted checkers' inputs in that
// directory, for internal/cmd/gsmgate to check. A Build result checked per footprint
// component is recorded as one machine per component. It panics if a record cannot be
// written, so a gate run cannot silently miss a machine.

import (
	"bytes"
	"errors"
	"os"
	"strings"

	"github.com/blackwell-systems/gsm/internal/gate"
)

func init() {
	dir := os.Getenv(gate.EnvDir)
	if dir == "" {
		return
	}
	prev := buildObserver
	buildObserver = func(r *Registry, m *Machine, rep *Report, err error) {
		if prev != nil {
			prev(r, m, rep, err)
		}
		gateBuild(dir, r, m, rep, err)
	}
	machineObserver = func(kind string, r *Registry, m *Machine) { gateMachine(dir, kind, r, m) }
}

func gateBuild(dir string, r *Registry, m *Machine, rep *Report, err error) {
	rec := gate.Record{Name: r.name, Kind: gate.KindBuild, BuildOK: err == nil}
	if err != nil {
		rec.BuildErr = err.Error()
		if rep != nil {
			rec.WFCFail = !rep.WFC && strings.Contains(rec.BuildErr, "WFC")
			rec.CCFail = rep.WFC && rep.CCFailure != nil
		}
	}
	if r.abs != nil {
		gateAbstract(dir, rec, r, m, rep)
		return
	}
	if rep != nil && rep.Compositional != nil {
		gateComponents(dir, rec, r, m, rep)
		return
	}
	rules, pairs, rerr := gateRules(r)
	if rerr != nil {
		rec.RulesErr = rerr.Error()
	}
	var tables []byte
	var terr error
	switch {
	case m != nil:
		tables, terr = m.convergenceTables()
	case rec.CCFail:
		// The tables of the same registry without the CC phase, which the table
		// checker has to reject.
		var m2 *Machine
		if m2, _, terr = r.build(false); terr == nil {
			tables, terr = m2.convergenceTables()
		}
	default:
		terr = errors.New("Build failed before the CC phase: no tables")
	}
	if terr != nil {
		rec.TablesErr = terr.Error()
	}
	gateWrite(dir, rec, rules, pairs, tables)
}

// gateAbstract records a Build by abstraction (Registry.Abstract). The rules checker would
// enumerate the declared ranges, so it does not run; the table checker gets the
// representative tables, which Build checked (and, for an accepted machine, the table
// oracle certified).
func gateAbstract(dir string, rec gate.Record, r *Registry, m *Machine, rep *Report) {
	rec.RulesErr = "verified by abstraction: the rules checker would enumerate the declared ranges; the " +
		"representative tables are checked instead"
	var tables []byte
	var terr error
	if m != nil || rec.CCFail {
		tables, terr = r.representativeTablesText()
	} else {
		terr = errors.New("Build failed before the CC phase: no tables")
	}
	if terr != nil {
		rec.TablesErr = terr.Error()
	}
	gateWrite(dir, rec, nil, nil, tables)
}

// gateComponents records a Build that checked the machine per footprint component. The
// rules checker would enumerate the whole machine, so it does not run; the table checker
// gets each component's tables, which the in-process table oracle certified: an accepted
// machine is recorded as one machine per component, named "<registry>/component<k>", and a
// machine rejected for CC as the registry with the failing component's tables, which the
// table checker must reject.
func gateComponents(dir string, rec gate.Record, r *Registry, m *Machine, rep *Report) {
	const why = "checked per footprint component: the rules checker would enumerate the whole machine; each " +
		"component's tables are checked instead"
	rec.RulesErr = why
	tbs, terr := r.componentTablesFor()
	switch {
	case terr != nil:
		rec.TablesErr = terr.Error()
	case m != nil:
		for _, nt := range tbs {
			c := rec
			c.Name = nt.name
			gateWrite(dir, c, nil, nil, formatTables(nt.tables))
		}
		return
	case rec.CCFail && rep != nil:
		if ci := r.componentOf(rep.CCFailure.Event1); ci >= 0 && ci < len(tbs) {
			gateWrite(dir, rec, nil, nil, formatTables(tbs[ci].tables))
			return
		}
		rec.TablesErr = "the failing pair's component was not found"
	default:
		rec.TablesErr = "Build failed before the CC phase: no tables"
	}
	gateWrite(dir, rec, nil, nil, nil)
}

func gateMachine(dir, kind string, r *Registry, m *Machine) {
	rec := gate.Record{Name: r.name, Kind: kind, BuildOK: true}
	var tables []byte
	switch kind {
	case gate.KindSynthesized:
		rec.RulesErr = "a synthesized repair is a table, not rules"
		var err error
		if tables, err = m.convergenceTables(); err != nil {
			rec.TablesErr = err.Error()
		}
	default:
		rec.RulesErr = "not exported for a " + kind + " machine"
		rec.TablesErr = "a " + kind + " machine has no global tables"
	}
	gateWrite(dir, rec, nil, nil, tables)
}

// gateRules exports the registry's rules and declared pairs, or says why not.
func gateRules(r *Registry) (rules, pairs []byte, err error) {
	var rb, pb bytes.Buffer
	if err = r.WriteMachineAST(&rb); err != nil {
		return nil, nil, err
	}
	if err = r.WriteDeclaredPairs(&pb); err != nil {
		return nil, nil, err
	}
	return rb.Bytes(), pb.Bytes(), nil
}

func gateWrite(dir string, rec gate.Record, rules, pairs, tables []byte) {
	if err := gate.WriteRecord(dir, rec, rules, pairs, tables); err != nil {
		panic("gsm gate: recording machine " + rec.Name + ": " + err.Error())
	}
}

// representativeTablesText is representativeTables in WriteConvergenceTables' format.
func (r *Registry) representativeTablesText() ([]byte, error) {
	tb, err := r.representativeTables()
	if err != nil {
		return nil, err
	}
	return formatTables(tb), nil
}
