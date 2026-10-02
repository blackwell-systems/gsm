package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// fixture writes a module under a temp root that requires this gsm checkout.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	gsm, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files["go.mod"] = "module example.com/fx\n\ngo 1.22\n\nrequire github.com/blackwell-systems/gsm v0.0.0\n\nreplace github.com/blackwell-systems/gsm => " + gsm + "\n"
	for path, content := range files {
		p := filepath.Join(root, filepath.FromSlash(path))
		if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func scanFixture(t *testing.T, files map[string]string, catalog string) string {
	t.Helper()
	root := fixture(t, files)
	cat, err := gate.ParseCatalog(strings.NewReader(catalog))
	if err != nil {
		t.Fatal(err)
	}
	got, err := scan(root, cat)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(strings.Join(got, "\n"), root+string(filepath.Separator), "")
}

const imp = "import \"github.com/blackwell-systems/gsm\"\n"

func TestScanFindsEveryMaker(t *testing.T) {
	got := scanFixture(t, map[string]string{
		"listed/main.go":   "package main\n" + imp + "func main() { gsm.NewRegistry(\"a\").Build() }\n",
		"alias/main.go":    "package main\nimport g \"github.com/blackwell-systems/gsm\"\nfunc main() { g.NewFederation(\"f\") }\n",
		"dot/main.go":      "package main\nimport . \"github.com/blackwell-systems/gsm\"\nfunc main() { NewRegistry(\"c\") }\n",
		"value/main.go":    "package main\n" + imp + "var mk = gsm.NewRegistry\nfunc main() { _ = mk }\n",
		"method/main.go":   "package main\n" + imp + "func main() { var r *gsm.Registry; b := r.Build; _ = b }\n",
		"synth/main.go":    "package main\n" + imp + "func main() { var s *gsm.Synthesis; _ = s.Machine() }\n",
		"lib/lib.go":       "package lib\n" + imp + "func Make() *gsm.Registry { return gsm.NewRegistry(\"l\") }\n",
		"uses/main.go":     "package main\n" + imp + "func main() { var m *gsm.Machine; _ = m }\n", // depends on gsm, so it must be listed too
		"tested/x.go":      "package tested\n",
		"tested/x_test.go": "package tested\n" + imp + "func f() { gsm.NewRegistry(\"t\") }\n", // test code is not shipped
		"tagged/main.go":   "//go:build never\n\npackage main\n" + imp + "func main() { gsm.NewRegistry(\"h\") }\n",
		".hidden/main.go":  "package main\n" + imp + "func main() { gsm.NewRegistry(\"h\") }\n",
	}, "listed a certified\n")
	for _, want := range []string{
		"alias makes gsm machines",
		"dot makes gsm machines",
		"value makes gsm machines",
		"method makes gsm machines",
		"synth makes gsm machines",
		"lib makes gsm machines",
		"tagged/main.go imports gsm but is in no loaded package",
		".hidden is a main package that depends on gsm",
		"uses is a main package that depends on gsm",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"listed ", "tested"} {
		if strings.Contains(got, not) {
			t.Errorf("unexpected %q in:\n%s", not, got)
		}
	}
}

func TestScanProgramRules(t *testing.T) {
	got := scanFixture(t, map[string]string{
		"lib/lib.go":    "package lib\n" + imp + "func Make() *gsm.Registry { return gsm.NewRegistry(\"l\") }\n",
		"env/main.go":   "package main\nimport (\"os\"\n\"github.com/blackwell-systems/gsm\")\nfunc main() { gsm.NewRegistry(os.Getenv(\"N\")) }\n",
		"flags/main.go": "package main\nimport (\"flag\"\n\"github.com/blackwell-systems/gsm\")\nvar n = flag.String(\"n\", \"x\", \"\")\nfunc main() { flag.Parse(); gsm.NewRegistry(*n) }\n",
		"plain/main.go": "package main\n" + imp + "func main() { gsm.NewRegistry(\"p\") }\n",
	}, "lib l certified\nenv e certified\nflags f certified\nplain p certified\n")
	for _, want := range []string{
		"lib is a catalog program but not a main package",
		"program env reads os.Getenv",
		"program flags reads flag.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "plain") {
		t.Errorf("plain should pass:\n%s", got)
	}
}

func TestScanDocs(t *testing.T) {
	got := scanFixture(t, map[string]string{
		"main.go":         "package main\nfunc main() {}\n",
		"docs/plain.md":   "```go\nr := gsm.NewRegistry(\"order\")\n```\n",
		"docs/alias.mdx":  "```go\nimport g \"github.com/blackwell-systems/gsm\"\nr := g.NewFederation(\"f\")\n```\n",
		"docs/dot.rst":    "import . \"github.com/blackwell-systems/gsm\"\n\nr := NewRegistry(\"x\")\n",
		"docs/other.md":   "r := plan.NewRegistry()\n",
		"docs/listed.md":  "r := gsm.NewRegistry(\"y\")\n",
		"docs/stale.md":   "nothing here\n",
		"docs/notdot.txt": "r := plan.NewRegistry()\nimport \"github.com/blackwell-systems/gsm\"\n",
	}, "@doc docs/listed.md shown\n@doc docs/stale.md stale\n")
	want := strings.Join([]string{
		"@doc docs/stale.md: the document shows no gsm machine",
		"docs/alias.mdx shows a gsm machine but is not listed with @doc",
		"docs/dot.rst shows a gsm machine but is not listed with @doc",
		"docs/plain.md shows a gsm machine but is not listed with @doc",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
