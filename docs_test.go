package gsm_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Doc-snippet check: every ```go block in the top-level docs compiles, and every
// block marked `run` executes successfully (README examples Build their machine
// and panic on a verification error, so a non-convergent example fails here).
//
// Each block is preceded by a directive comment naming how it is checked:
//
//	<!-- gocheck: run -->               a complete example: type-checked, then executed
//	<!-- gocheck: check PRELUDE -->     statements type-checked after the named prelude,
//	                                    which declares what the surrounding prose set up
//	<!-- gocheck: excerpt REASON -->    not compiled: a simplified excerpt of gsm's
//	                                    internals or a design sketch of an unbuilt API
//
// A block without a directive fails the test, so a new example cannot slip in
// unchecked. Snippets see the gsm package both qualified (gsm.X) and dot-imported
// (X), plus fmt, log, os, and strings.

// docFiles are the top-level docs whose Go blocks are checked. EXPLAINER.md is
// excluded pending its author's review (its blocks are untouched by this check).
var docFiles = []string{
	"README.md", "ARCHITECTURE.md", "CONCEPTS.md", "THEORY.md",
	"FEDERATION-CONCEPTS.md", "CERTIFICATE-DESIGN.md", "CHANGELOG.md",
}

// preludes declare the variables a `check` snippet's surrounding prose has set up.
// Each runs in an outer scope; the snippet runs in a nested block, so it may
// redeclare any of these names.
var preludes = map[string]string{
	// A registry with order-style variables and events.
	"registry": `
	r := gsm.NewRegistry("doc")
	b := r
	status := r.Enum("status", "pending", "paid", "shipped")
	paid := r.Bool("paid")
	count := r.Int("count", 0, 15)
	balance := r.Int("balance", -5, 5)
	notified := r.Bool("notified")
	x := r.Int("x", 0, 7)
	a := r.Int("a", 0, 5)
	r.Event("deposit").Writes(balance).Apply(func(s State) State { return s }).Add()
	r.Event("withdraw").Writes(balance).Apply(func(s State) State { return s }).Add()
	r.Event("notify").Writes(notified).Apply(func(s State) State { return s }).Add()
	r.Event("send_notification").Writes(notified).Apply(func(s State) State { return s }).Add()
	cost := func(from, to State) int { return 0 }
	`,
	// A built machine and a state.
	"machine": `
	r := gsm.NewRegistry("doc")
	statusVar := r.Enum("status", "pending", "active")
	enabledVar := r.Bool("enabled")
	countVar := r.Int("count", 0, 100)
	r.Event("increment").Writes(countVar).Apply(func(s State) State { return s }).Add()
	r.Event("ship").Writes(statusVar).Apply(func(s State) State { return s }).Add()
	r.Event("ship_item").Writes(statusVar).Apply(func(s State) State { return s }).Add()
	r.Event("pay").Writes(enabledVar).Apply(func(s State) State { return s }).Add()
	machine, _, _ := r.Build()
	registry := r
	state := machine.NewState()
	s := state
	`,
	// Federation: registries, variables, and a federation under construction.
	"federation": `
	mfr := gsm.NewRegistry("manufacturer")
	mstate := mfr.Enum("mstate", "draft", "active", "suspended")
	sup := gsm.NewRegistry("supplier")
	sstate := sup.Enum("sstate", "idle", "listed", "stale", "err")
	hr, security, door := gsm.NewRegistry("hr"), gsm.NewRegistry("security"), gsm.NewRegistry("door")
	employed, cleared := hr.Bool("employed"), security.Bool("cleared")
	access := door.Enum("access", "denied", "granted")
	pricing, catalog, order := gsm.NewRegistry("pricing"), gsm.NewRegistry("catalog"), gsm.NewRegistry("order")
	inventory, fulfillment, shipping := gsm.NewRegistry("inventory"), gsm.NewRegistry("fulfillment"), gsm.NewRegistry("shipping")
	a, b := gsm.NewRegistry("A"), gsm.NewRegistry("B")
	fa, fb := a.Int("fa", 0, 1), b.Int("fb", 0, 1)
	fed := gsm.NewFederation("doc")
	sub := gsm.NewFederation("pricing")
	hrToDoor := func(src, dst State) State { return dst }
	securityToDoor := hrToDoor
	`,
}

var directiveRE = regexp.MustCompile(`^<!--\s*gocheck:\s*(run|check|excerpt)\b\s*(.*?)\s*-->\s*$`)

type docBlock struct {
	file, mode, arg, code string
	line                  int
}

func extractBlocks(t *testing.T, file string) []docBlock {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var out []docBlock
	var lastNonEmpty string
	var cur *docBlock
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		switch {
		case cur != nil && strings.HasPrefix(line, "```"):
			out = append(out, *cur)
			cur = nil
		case cur != nil:
			cur.code += line + "\n"
		case strings.TrimSpace(line) == "```go":
			cur = &docBlock{file: file, line: n}
			if m := directiveRE.FindStringSubmatch(lastNonEmpty); m != nil {
				cur.mode, cur.arg = m[1], m[2]
			}
		}
		if strings.TrimSpace(line) != "" {
			lastNonEmpty = strings.TrimSpace(line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return out
}

const snippetImports = `import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/blackwell-systems/gsm"
	. "github.com/blackwell-systems/gsm"
)

var _, _, _, _ = fmt.Sprint, log.Println, os.Exit, strings.Contains
var _ = gsm.NewRegistry
var _ = NewRegistry
`

// wrap returns the snippet as a Go source file: top-level declarations stay at
// file scope; statements go inside main, after the prelude.
func wrap(b docBlock) (string, error) {
	prelude := ""
	if b.mode == "check" {
		p, ok := preludes[b.arg]
		if !ok && b.arg != "" {
			return "", fmt.Errorf("unknown prelude %q", b.arg)
		}
		prelude = p
	}
	src := "package main\n\n" + snippetImports + "\nfunc main() {\n" + prelude + "\n{\n" + b.code + "\n}\n}\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, 0); err == nil {
		return src, nil
	}
	// Not statements: try the block as file-scope declarations.
	src = "package main\n\n" + snippetImports + "\n" + b.code + "\nfunc main() {}\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, 0); err != nil {
		return "", fmt.Errorf("does not parse as statements or declarations: %v", err)
	}
	return src, nil
}

func TestDocSnippets(t *testing.T) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	var runs []docBlock
	var excerpts []string
	for _, file := range docFiles {
		for _, b := range extractBlocks(t, file) {
			where := fmt.Sprintf("%s:%d", b.file, b.line)
			switch b.mode {
			case "":
				t.Errorf("%s: Go block has no <!-- gocheck: ... --> directive", where)
				continue
			case "excerpt":
				if b.arg == "" {
					t.Errorf("%s: excerpt directive needs a reason", where)
				}
				excerpts = append(excerpts, where)
				continue
			}
			src, err := wrap(b)
			if err != nil {
				t.Errorf("%s: %v\n%s", where, err, b.code)
				continue
			}
			f, err := parser.ParseFile(fset, where+".go", src, 0)
			if err != nil {
				t.Errorf("%s: %v", where, err)
				continue
			}
			var hard []string
			conf := types.Config{
				Importer: imp,
				Error: func(err error) {
					// Unused variables and imports are soft errors: snippets show
					// fragments whose results the reader uses later.
					if te, ok := err.(types.Error); ok && te.Soft {
						return
					}
					hard = append(hard, err.Error())
				},
			}
			// conf.Error sees every error; Check also returns the first, already collected.
			if _, err := conf.Check("main", fset, []*ast.File{f}, nil); err != nil && len(hard) == 0 {
				if te, ok := err.(types.Error); !ok || !te.Soft {
					hard = append(hard, err.Error())
				}
			}
			if len(hard) > 0 {
				t.Errorf("%s: does not compile:\n  %s\n--- snippet ---\n%s", where, strings.Join(hard, "\n  "), b.code)
				continue
			}
			if b.mode == "run" {
				runs = append(runs, b)
			}
		}
	}
	sort.Strings(excerpts)
	t.Logf("not compiled (excerpts): %v", excerpts)

	if testing.Short() {
		t.Skip("skipping execution of run snippets in -short mode")
	}
	repo, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range runs {
		b := b
		where := fmt.Sprintf("%s:%d", b.file, b.line)
		src, err := wrap(b)
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		dir := t.TempDir()
		mod := "module docsnippet\n\ngo 1.22\n\nrequire github.com/blackwell-systems/gsm v0.0.0\n\nreplace github.com/blackwell-systems/gsm => " + repo + "\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "run", ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: example does not run cleanly: %v\n%s\n--- snippet ---\n%s", where, err, out, b.code)
		}
	}
}
