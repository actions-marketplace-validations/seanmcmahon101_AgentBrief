# Validation plans for changed files

`agentbrief plan` answers a specific question: which **maintainer-declared**
checks apply to these changed filenames, and which prerequisites must precede
them? It reads policy, reports sources, and never runs commands or Git.

## Declare checks once

Add checks alongside your optional workflow preferences in `.agentbrief.yaml`:

```yaml
version: 1
checks:
  - id: build
    kind: build
    run: npm run build:cached
  - id: tests
    kind: test
    run: npm run test
    paths: [src/**, tests/**, package.json]
    exclude: [src/vendor/**]
    requires: [build]
  - id: website
    kind: check
    run: python3 scripts/website.py check
    paths: [website/**, scripts/website.py]
    dir: .
```

This example assumes the scripts and directories exist. Checks share the
workflow command rules: supported `kind`, exact single-line `run`, an explicit
repository-relative `dir` (default `.`), redaction, and declaration provenance.
Checks are separate from brief preferences: several checks can have the same
kind, and declaring checks does not change the managed block.

- `id` is a unique lowercase identifier accepted by the existing model:
  1–30 letters, digits, `.`, `_`, `+`, `#`, or `-`.
- `paths` selects a check when any supplied filename matches any pattern.
  Omitting `paths` makes the check prerequisite-only.
- `exclude` suppresses matches for **that check only**. It requires `paths`.
- `requires` references other check ids. Shared prerequisites appear once.
  Unknown ids, duplicate ids/references, and dependency cycles invalidate the
  whole configuration.
- Present lists must be nonempty; `checks: []` is permitted to declare no checks.
  Unknown fields and aliases are rejected, as in workflow declarations.

Patterns are anchored to the repository root and case-sensitive on every OS.
`*` and `?` match within a segment; a complete `**` segment matches zero or
more segments. Thus `src/**/*.ts` includes `src/index.ts`, while `src/*.ts`
does not include `src/nested/index.ts`. Exact paths, including deleted files,
need not exist. Negation, character classes, brace expansion, absolute paths,
empty/dot/parent segments, and embedded double stars such as `src/**suffix`
are unsupported. Use a separate exclusion list rather than `!pattern`.

Matching checks are additive. No broad rule silently overrides a narrower rule.
Unrelated prerequisites are omitted. The plan uses topological order with
lexical id tie-breaking, independent of input order; command arguments are
never rewritten or interpolated.

## Supply the actual change list

```sh
agentbrief plan src/changed.ts website/index.html --json

# The caller invokes Git; AgentBrief only consumes its filename output.
git diff --name-only -z --no-renames HEAD > changed-files
agentbrief plan --files-from changed-files --null --json --strict
```

To pipe the list, use `--files-from -`. Without `--null`, the list is one
filename per line (LF or CRLF). Prefer NUL-delimited input for names containing
spaces or Git-quoted characters. Paths always refer to the repository root,
including when invoked from a nested directory. Use slash-separated paths on
Windows. Traversal, absolute paths and control characters are rejected.
Input is capped at 1 MiB; overflow and read failures reject the whole list.
Matching is bounded to 64 Mi weighted work units (segment pairs plus pattern/path
bytes) and a conservative 16 MiB
output-expansion budget. Exceeding either resource budget refuses the
whole plan; narrow the input or policy rather than accepting truncated output.

Deleted files are valid inputs. Include **both old and new filenames** for a
rename; `--no-renames` makes Git report it as deletion plus addition. Choose the
comparison deliberately: `HEAD` includes staged and unstaged tracked changes;
a PR usually needs its merge base. Untracked files must be supplied separately.
AgentBrief cannot establish that your caller supplied the full change list.

Exit 0 means a plan was produced. With `--strict`, unmatched paths or a
truncated repository scan cause exit 1 **after** printing the plan. Usage,
input, malformed configuration, or known-invalid command references cause
exit 2 without a partial plan. Strict mode does not certify test coverage.

## Evidence and unknowns

JSON uses schema `agentbrief/plan/v1`:

- `paths`: sorted, deduplicated filenames supplied by the caller.
- `steps`: ordered checks, exact `command`, `requires`, `selected_by` path
  matches and pattern source lines, and `required_by` dependents.
- `unmatched_paths`: inputs without any directly matching check.
- `mapping_complete`: every input matched declared policy and the scan was
  not truncated. This measures declared mappings, not behavioral coverage.
- `protected_paths`: input files matching discovered or declared do-not-edit
  patterns, with their rules and sources. Advisory; not an exhaustive ownership
  analysis. These comparisons use the root-anchored glob semantics above.
- `policy_changed`: the input includes `.agentbrief.yaml`; review changes to
  mappings and prerequisites themselves.
- `coverage: "unknown"` and `executed: false`: always explicit. No command
  results, host readiness or actual test coverage are established.

No configuration or no matching rule produces an empty plan and explicit
unmatched paths. It never silently falls back to guessed tests. AgentBrief
does not compute transitive source dependencies, inspect a diff's contents,
prove a test sufficient, or replace maintainer judgment. Map shared libraries,
build configuration and lockfiles conservatively when they affect many parts
of a repository.

`agentbrief check` also validates every check command, including checks not
selected by today's change list. Broken references invalidate plans and briefs;
dynamic/unknown tools and broken script bodies can remain unresolved.

See [reproducible public-repository examples](../examples/case-studies/README.md).
