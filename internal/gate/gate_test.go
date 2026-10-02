package gate

import (
	"fmt"
	"os"
	"path/filepath"
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
		{"not built, but rejected for CC", "p m not-built", []rec{{"p", cc, in(1, "x"), in(1, "x")}}, "list it rejected"},
		{"not built, but Build accepts", "p m not-built", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, "listed not-built, but it is an accepted machine"},
		{"listed certified, Build rejects", "p m certified", []rec{{"p", cc, in(1, "x"), in(1, "x")}}, "listed certified, but it is a rejected machine"},
		{"listed rejected, Build accepts", "p m rejected", []rec{{"p", ok, in(0, "ok"), in(0, "ok")}}, "listed rejected, but it is an accepted machine"},
		{"unlisted machine", "p m certified", []rec{{"p", ok, in(0, ""), in(0, "")}, {"p", Record{Name: "n", Kind: KindBuild, BuildOK: true}, in(0, ""), in(0, "")}}, "\"n\" is not in the catalog"},
		{"listed machine not made", "p m certified\np n certified", []rec{{"p", ok, in(0, ""), in(0, "")}}, "did not make n"},
		{"program not listed", "p m certified", []rec{{"p", ok, in(0, ""), in(0, "")}, {"q/r", ok, in(0, ""), in(0, "")}}, "program q/r made gsm machines but is not in the catalog"},
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
@exempt tools/gen generates machines at build time
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Entries) != 2 || c.Entries[0].Program != "README.md#1" || c.Entries[1].Verdict != Rejected {
		t.Fatalf("entries: %+v", c.Entries)
	}
	if c.Docs["docs/x.md"] != "shows a fragment" || c.Exempt["tools/gen"] == "" {
		t.Fatalf("docs %v exempt %v", c.Docs, c.Exempt)
	}
	for _, bad := range []string{"p m maybe", "p m", "p m certified\np m certified", "@doc only-a-path", "@doc a b\n@doc a c"} {
		if _, err := ParseCatalog(strings.NewReader(bad)); err == nil {
			t.Errorf("ParseCatalog(%q): want an error", bad)
		}
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const imp = "import \"github.com/blackwell-systems/gsm\"\n"
	write("ex/a/main.go", "package main\n"+imp+"func main() { gsm.NewRegistry(\"a\") }\n")
	write("ex/b/main.go", "package main\nimport g \"github.com/blackwell-systems/gsm\"\nfunc main() { g.NewFederation(\"f\") }\n")
	write("ex/c/main.go", "package main\nimport . \"github.com/blackwell-systems/gsm\"\nfunc main() { NewRegistry(\"c\") }\n")
	write("ex/d/main.go", "package main\n"+imp+"func main() { var m *gsm.Machine; _ = m }\n") // uses gsm, makes no machine
	write("ex/e/e_test.go", "package e\n"+imp+"func f() { gsm.NewRegistry(\"t\") }\n")        // test code is not shipped
	write("ex/.hidden/main.go", "package main\n"+imp+"func main() { gsm.NewRegistry(\"h\") }\n")
	write("docs/guide.md", "```go\nr := gsm.NewRegistry(\"order\")\n```\n")
	write("docs/other.md", "no machines here\n")

	cat, err := ParseCatalog(strings.NewReader("ex/a a certified\n@doc docs/guide.md a fragment\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Scan(root, cat)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ex/b creates gsm machines (ex/b/main.go) but is not a catalog program or @exempt",
		"ex/c creates gsm machines (ex/c/main.go) but is not a catalog program or @exempt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Scan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	cat, err = ParseCatalog(strings.NewReader("ex/a a certified\nex/b f certified\n@exempt ex/c reason\n@exempt ex/d stale\n@doc docs/other.md stale\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err = Scan(root, cat); err != nil {
		t.Fatal(err)
	}
	want = []string{
		"@doc docs/other.md: the document shows no gsm machine",
		"@exempt ex/d: no gsm machine is created there",
		"docs/guide.md shows a gsm machine but is not listed with @doc",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Scan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
