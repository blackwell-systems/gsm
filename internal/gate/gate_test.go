package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The checkers are faked by this test binary: run with GATE_FAKE_CHECKER=1, it
// reads its first argument, whose first line is "<exit code> <output>", prints
// the output and exits with that code.
func TestMain(m *testing.M) {
	if os.Getenv("GATE_FAKE_CHECKER") == "1" {
		b, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Println(err)
			os.Exit(9)
		}
		line, _, _ := strings.Cut(string(b), "\n")
		code, msg, _ := strings.Cut(line, " ")
		n, err := strconv.Atoi(code)
		if err != nil {
			fmt.Println(err)
			os.Exit(9)
		}
		fmt.Println(msg)
		os.Exit(n)
	}
	if err := os.Setenv("GATE_FAKE_CHECKER", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func fake() Checkers { return Checkers{Table: os.Args[0], Rules: os.Args[0]} }

// in returns a fake checker input that makes the checker exit with code.
func in(code int, msg string) []byte { return []byte(fmt.Sprintf("%d %s\n", code, msg)) }

type rec struct {
	prog          string
	r             Record
	rules, tables []byte
}

func runGate(t *testing.T, catalog string, recs ...rec) *Report {
	t.Helper()
	cat, err := ParseCatalog(strings.NewReader(catalog))
	if err != nil {
		t.Fatal(err)
	}
	dumps := t.TempDir()
	for _, x := range recs {
		var pairs []byte
		if x.rules != nil {
			pairs = []byte("pairs all\n")
		}
		if err = WriteRecord(filepath.Join(dumps, filepath.FromSlash(x.prog)), x.r, x.rules, pairs, x.tables); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Run(dumps, cat, fake())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func problems(rep *Report) string {
	var p []string
	p = append(p, rep.Problems...)
	for _, o := range rep.Outcomes {
		p = append(p, o.Problems...)
	}
	return strings.Join(p, "\n")
}

func TestRun(t *testing.T) {
	ok := Record{Name: "m", Kind: KindBuild, BuildOK: true}
	cc := Record{Name: "m", Kind: KindBuild, BuildErr: "CC violated", CCFail: true}
	wfc := Record{Name: "m", Kind: KindBuild, BuildErr: "WFC violated", WFCFail: true}
	closures := ok
	closures.RulesErr = "closures"
	cases := []struct {
		name    string
		catalog string
		recs    []rec
		want    string // "" passes; otherwise a substring of the problems
	}{
		{"certified", "p m certified", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, ""},
		{"certified, rules checker rejects", "p m certified", []rec{{"p", ok, in(1, "no"), in(0, "ok")}}, "rules checker does not verify"},
		{"certified, table checker rejects", "p m certified", []rec{{"p", ok, in(0, "ok"), in(1, "no")}}, "table checker does not verify"},
		{"certified, rules outside the fragment", "p m certified", []rec{{"p", ok, in(1, "outside the certified fragment"), in(0, "ok")}}, "rules checker does not verify"},
		{"certified, checker crashed", "p m certified", []rec{{"p", ok, in(0, "ok"), in(3, "boom")}}, "exit 3"},
		{"certified, rules not exported", "p m certified", []rec{{"p", closures, nil, in(0, "ok")}}, "rules were not exported"},
		{"certified-tables", "p m certified-tables", []rec{{"p", closures, nil, in(0, "ok")}}, ""},
		{"certified-tables, rules export", "p m certified-tables", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, "list it certified"},
		{"certified-tables, tables missing", "p m certified-tables", []rec{{"p", closures, nil, nil}}, "tables were not exported"},
		{"input refused", "p m certified", []rec{{"p", ok, in(2, "bad"), in(0, "ok")}}, "refused the exported input"},
		{"rejected for CC, both checkers reject", "p m rejected", []rec{{"p", cc, in(1, "CC fails"), in(1, "no")}}, ""},
		{"rejected for CC, table checker verifies", "p m rejected", []rec{{"p", cc, in(1, "CC fails"), in(0, "ok")}}, "table checker verifies it"},
		{"rejected for CC, rules checker verifies", "p m rejected", []rec{{"p", cc, in(0, "ok"), in(1, "no")}}, "rules checker verifies it"},
		{"rejected for CC, rules checker says WFC", "p m rejected", []rec{{"p", cc, in(1, "not verified (WFC)"), in(1, "no")}}, "different reason"},
		{"rejected for WFC, rules checker agrees", "p m rejected", []rec{{"p", wfc, in(1, "not verified (WFC)"), nil}}, ""},
		{"rejected for WFC, rules outside fragment", "p m rejected", []rec{{"p", wfc, in(1, "outside the certified fragment (WFC)"), nil}}, "outside its fragment"},
		{"rejected, nothing ran", "p m rejected", []rec{{"p", wfc, nil, nil}}, "no checker could run"},
		{"rejected before CC", "p m rejected", []rec{{"p", Record{Name: "m", Kind: KindBuild, BuildErr: "bad name"}, nil, nil}}, "before the convergence check"},
		{"not built", "p m not-built", []rec{{"p", Record{Name: "m", Kind: KindBuild, BuildErr: "no Repair"}, nil, nil}}, ""},
		{"not built, rejected for WFC with nothing exported", "p m not-built", []rec{{"p", wfc, nil, nil}}, ""},
		{"not built, but a checker can run", "p m not-built", []rec{{"p", cc, nil, in(1, "x")}}, "list it rejected"},
		{"not built, but Build accepts", "p m not-built", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, "listed not-built, but it is an accepted machine"},
		{"listed certified, Build rejects", "p m certified", []rec{{"p", cc, in(1, "x"), in(1, "x")}}, "listed certified, but it is a rejected machine"},
		{"listed rejected, Build accepts", "p m rejected", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, "listed rejected, but it is an accepted machine"},
		{"unlisted machine", "p m certified", []rec{{"p", ok, in(0, ""), in(0, "")}, {"p", Record{Name: "n", Kind: KindBuild, BuildOK: true}, in(0, ""), in(0, "")}}, "\"n\" is not in the catalog"},
		{"listed machine not made", "p m certified\np n certified", []rec{{"p", ok, in(0, ""), in(0, "")}}, "did not make n"},
		{"program not listed", "p m certified", []rec{{"p", ok, in(0, ""), in(0, "")}, {"q/r", ok, in(0, ""), in(0, "")}}, "program q/r made gsm machines but is not in the catalog"},
		{"program listed @none makes a machine", "p m certified\n@none q builds nothing", []rec{{"p", ok, in(0, ""), in(0, "")}, {"q", ok, in(0, ""), in(0, "")}}, "program q is listed @none but made gsm machines"},
		{"synthesized", "p m synthesized", []rec{{"p", Record{Name: "m", Kind: KindSynthesized, BuildOK: true}, nil, in(0, "ok")}}, ""},
		{"synthesized, table checker rejects", "p m synthesized", []rec{{"p", Record{Name: "m", Kind: KindSynthesized, BuildOK: true}, nil, in(1, "no")}}, "table checker does not verify"},
		{"synthesized listed as certified", "p m certified", []rec{{"p", Record{Name: "m", Kind: KindSynthesized, BuildOK: true}, nil, in(0, "ok")}}, "listed certified, but it is a synthesized machine"},
		{"compositional", "p m certified", []rec{{"p", Record{Name: "m", Kind: KindCompositional, BuildOK: true}, nil, nil}}, "compositional machine"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := runGate(t, c.catalog, c.recs...)
			got := problems(rep)
			if c.want == "" {
				if !rep.OK() {
					t.Fatalf("want pass, got:\n%s\n%s", got, rep)
				}
				return
			}
			if rep.OK() {
				t.Fatalf("want a failure containing %q, got a pass:\n%s", c.want, rep)
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("want a failure containing %q, got:\n%s", c.want, got)
			}
		})
	}
}

// Identical checker inputs run once.
func TestRunCachesIdenticalInputs(t *testing.T) {
	ok := Record{Name: "m", Kind: KindBuild, BuildOK: true}
	rep := runGate(t, "p m certified\nq m certified",
		rec{"p", ok, in(0, "ok"), in(0, "ok")}, rec{"q", ok, in(0, "ok"), in(0, "ok")})
	if !rep.OK() || rep.Runs != 2 {
		t.Fatalf("want a pass with 2 checker runs, got %d:\n%s", rep.Runs, rep)
	}
}

func TestParseCatalog(t *testing.T) {
	c, err := ParseCatalog(strings.NewReader(`
# comment
README.md#1  order  certified-tables   # trailing comment
dir/prog     m      rejected
@doc docs/x.md shows a fragment
@none ExampleDoc builds no machine
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Entries) != 2 || c.Entries[0].Program != "README.md#1" || c.Entries[1].Verdict != Rejected {
		t.Fatalf("entries: %+v", c.Entries)
	}
	if c.Docs["docs/x.md"] != "shows a fragment" || c.None["ExampleDoc"] == "" {
		t.Fatalf("docs %v none %v", c.Docs, c.None)
	}
	for _, bad := range []string{"p m maybe", "p m", "p m certified\np m certified", "@doc only-a-path", "@doc a b\n@doc a c"} {
		if _, err := ParseCatalog(strings.NewReader(bad)); err == nil {
			t.Errorf("ParseCatalog(%q): want an error", bad)
		}
	}
}

// A checker killed by a signal has crashed: it has not run to a verdict, and the
// gate fails whatever the catalog expects, never reading it as "not run".
func TestRunCheckerCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell and signals")
	}
	crash := filepath.Join(t.TempDir(), "crash.sh")
	if err := os.WriteFile(crash, []byte("#!/bin/sh\nkill -SEGV $$\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ok := Record{Name: "m", Kind: KindBuild, BuildOK: true}
	cc := Record{Name: "m", Kind: KindBuild, BuildErr: "CC violated", CCFail: true}
	cases := []struct {
		name, catalog string
		r             Record
		crashRules    bool // which checker crashes; the other is the fake
		rules, tables []byte
	}{
		{"rejected, rules checker crashes", "p m rejected", cc, true, in(1, "CC fails"), in(1, "no")},
		{"rejected, table checker crashes", "p m rejected", cc, false, in(1, "CC fails"), in(1, "no")},
		{"certified, rules checker crashes", "p m certified", ok, true, in(0, "ok"), in(0, "ok")},
		{"certified, table checker crashes", "p m certified", ok, false, in(0, "ok"), in(0, "ok")},
		{"synthesized, table checker crashes", "p m synthesized", Record{Name: "m", Kind: KindSynthesized, BuildOK: true}, false, nil, in(0, "ok")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cat, err := ParseCatalog(strings.NewReader(c.catalog))
			if err != nil {
				t.Fatal(err)
			}
			dumps := t.TempDir()
			var pairs []byte
			if c.rules != nil {
				pairs = []byte("pairs all\n")
			}
			if err = WriteRecord(filepath.Join(dumps, "p"), c.r, c.rules, pairs, c.tables); err != nil {
				t.Fatal(err)
			}
			ck := fake()
			if c.crashRules {
				ck.Rules = crash
			} else {
				ck.Table = crash
			}
			rep, err := Run(dumps, cat, ck)
			if err != nil {
				t.Fatal(err)
			}
			if rep.OK() || !strings.Contains(problems(rep), "crashed") {
				t.Fatalf("want a failure naming the crash, got:\n%s", rep)
			}
		})
	}
}
