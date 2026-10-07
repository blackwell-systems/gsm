# Invariant coverage: source

[`../invariant-coverage.md`](../invariant-coverage.md) is generated from the files here. Edit them,
not the page.

| File | Role |
|---|---|
| `catalog.py` | The catalog: one record per invariant (domain, kind, expressible, verifiable at scale, the extensions that would move it) |
| `template.md` | The page text, with `{{KEY}}` placeholders for computed counts and tables and `[[SNIP id]]` for runnable samples |
| `snippets.md` | The runnable samples (V, T and N ids), each a `gocheck: run` block that `docs_test.go` runs once inserted into the page |
| `gen.py` | Computes the counts, shares, rankings and per-extension lists, and writes the page |
| `analyze.py` | Consistency checks on the catalog and the ranking of extension sets |

Regenerate from the repository root:

```sh
python3 docs/coverage/gen.py
go test -run TestDocSnippets .
```

When an extension ships, remove it from `EXT` in `gen.py` and treat it as done in `alts` (as
`PARAM` is), then reclassify the rows it moves in `catalog.py`.
