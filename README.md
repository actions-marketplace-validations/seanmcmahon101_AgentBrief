# AgentBrief

Keep your agent instruction files true. AgentBrief checks `AGENTS.md`, `CLAUDE.md`, Cursor rules and Copilot instructions against the repository in CI, and can maintain a small, sourced block of operational facts for you.

Instruction files for coding agents are usually right on the day they are written, whether a person or an agent wrote them. Then scripts get renamed, test directories move, the package manager changes, and nothing tells you. Agents follow stale instructions faithfully. AgentBrief reads your manifests, lockfiles, task runners and CI workflows (statically: no LLM, no API key, no network, never executing repository code) and:

- **`agentbrief check`** fails CI when an instruction file tells agents to run a script, target, crate, task or service that no longer exists, points at a missing path, names the wrong package manager or a runtime version that disagrees with the repo's pin, or isn't loaded by Claude Code at all. It works on any instruction file, however it was written.
- **`agentbrief sync`** writes a small managed block of verified facts into `AGENTS.md` (package manager and toolchain, the exact install / build / test / single-test / lint commands, files not to edit), each with its source. `check` fails when the block is out of date, so this part of your instructions cannot rot.
- **`agentbrief brief <path> --json`** gives an agent the same facts on demand, scoped to the package it is about to change.
- **`agentbrief plan <changed-files> --json`** selects maintainer-declared validation checks and their prerequisites, explains each selection, and flags unmapped files. It never runs commands or claims test coverage. See [validation plans](docs/plans.md).
- **Optional [maintainer-selected workflows](docs/workflows.md)** preserve your actual build/test wrappers and working directories, including subtrees without manifests. Declare them once in `.agentbrief.yaml`; briefs use them and `check` catches stale references.

- [Catching drift](#catching-drift)
- [Maintainer-selected workflows](#maintainer-selected-workflows)
- [Validation plans](#validation-plans) / [Where this is useful](#where-this-is-useful)
- [The facts block](#the-facts-block)
- [Should I use this, or just ask an agent to write AGENTS.md?](#should-i-use-this-or-just-ask-an-agent-to-write-agentsmd)
- [Install](#install) / [Usage](#usage) / [For coding agents](#for-coding-agents) / [CI and pre-commit](#ci-and-pre-commit)
- [What it detects](#what-it-detects) / [Check rules](#check-rules) / [Evidence labels](#evidence-labels)
- [Evaluation](#evaluation)
- [Security model](#security-model) / [Limitations](#limitations)

## Catching drift
Excalidraw's `CLAUDE.md` tells agents to run `yarn test:typecheck`. In a scratch copy, rename that script in `package.json` to `typecheck`, and `agentbrief check` reports it (see [`examples/check-drift.md`](examples/check-drift.md)):

```console
$ agentbrief check
CLAUDE.md:1: warning: CLAUDE.md does not import AGENTS.md, so Claude Code will not read it [agents-md-not-loaded]
  add `@AGENTS.md` as the first line (`agentbrief sync` does this)
CLAUDE.md:17: error: `yarn test:typecheck` runs script `test:typecheck`, which is not defined in any package.json [unknown-script]
  did you mean `yarn typecheck`?
CLAUDE.md:22: error: `yarn test:typecheck` runs script `test:typecheck`, which is not defined in any package.json [unknown-script]
  did you mean `yarn typecheck`?
2 errors, 1 warning
$ echo $?
1
```

The same applies to the generated block. After `agentbrief sync` on Vite, renaming the `typecheck` script gives:

```console
$ agentbrief check
AGENTS.md:5: error: the AgentBrief block no longer matches the repository [stale-block]
  run `agentbrief sync` and commit the result
1 error, 0 warnings
```

## Maintainer-selected workflows

If your preferred command is `npm run test:focused -- <file>` rather than raw
Vitest, declare that choice in an optional root `.agentbrief.yaml`:

```yaml
version: 1
workflows:
  - scope: .
    commands:
      - kind: test-single
        run: npm run test:focused -- <file>
```

Scopes can also select subsystem or website commands independently of package
boundaries. Every command retains its own working directory. Selected commands
are labelled `maintainer` and execution remains unverified. `check` validates
these declarations even without a managed block; `brief` and `sync` refuse known
invalid declarations. See [workflow configuration](docs/workflows.md) for
precedence, generated paths, JSON evidence, and static-analysis limits.

## Validation plans

A frontier agent can understand a command without knowing whether maintainers
require it for a particular change. Declare file-to-check mappings and
prerequisites alongside workflow preferences:

```yaml
version: 1
checks:
  - id: build
    kind: build
    run: pnpm run build
  - id: integration
    kind: test
    run: pnpm run test-serve
    paths: [playground/**]
    requires: [build]
```

```sh
agentbrief plan playground/assets/__tests__/assets.spec.ts --json --strict
```

The plan puts `build` before `integration`, keeps exact commands and working
directories, and cites both command and matching-pattern declarations.
Overlapping checks accumulate; shared prerequisites appear once. Deleted paths
are accepted; supply both names for renames. Unmapped files remain explicit
unknowns. `--strict` fails incomplete declared mappings; it does not certify
coverage. `check` catches stale configured command references even when no
managed block exists.

See [the full contract](docs/plans.md) for file-list/NUL input, exclusions,
exit codes, and limitations.

## Where this is useful

We reproduced these operational cases on pinned public-repository snapshots:

| Repository | Concrete workflow issue | What the example policy lets AB preserve |
|---|---|---|
| [Vite](https://github.com/vitejs/vite) | Integration tests require a build and have separate serve/build modes; docs have their own check. | Select both integration modes after one shared build, or select the docs workflow. |
| [Ruff](https://github.com/astral-sh/ruff) | Lint-rule changes involve tests and generation; documentation has a separate preparation/build sequence. | Keep those sequences explicit and flag declared generated outputs. |
| [FastAPI](https://github.com/fastapi/fastapi) | Its test wrapper sets an import path and includes additional script tests. | Preserve the actual wrapper for framework and documentation-example changes. |

**These are tested example policies, not upstream adoption or endorsements.**
[Sources, pinned revisions, policies and reproduction instructions](examples/case-studies/README.md)
are included. Six static selection scenarios passed, and all three planted
command-drift cases were detected and caused plan refusal. Repository commands
were not executed. This demonstrates selection and drift checking; it does not
measure frontier-agent speed, task success or actual test coverage.

## The facts block
Running `agentbrief` in a checkout of [Vite](https://github.com/vitejs/vite) (shallow clone, 2026-10-11, output verbatim):

```console
$ agentbrief
## Repository facts

_Generated by [AgentBrief](https://github.com/seanmcmahon101/AgentBrief) from repository files; do not edit inside this block. Refresh with `agentbrief sync`._

- Toolchain: Node ^20.19.0 || >=22.12.0 (`package.json`), pnpm 12.9.1 (`package.json`). Use pnpm, not npm or yarn.

Commands (run from the repo root):
- install: `pnpm install` (pnpm convention, unverified)
- build: `pnpm build` (package.json, CI)
- test: `pnpm test` (package.json)
- single test: `pnpm exec vitest run <file> -t "<name>"` (vitest convention, unverified)
- lint: `pnpm lint` (package.json, CI)
- format: `pnpm format` (package.json, CI)
- typecheck: `pnpm typecheck` (package.json, CI)
- dev: `pnpm dev` (package.json)

Workspace (pnpm, 4 packages): `docs` (`@vitejs/monorepo-docs`), `packages/create-vite`, `packages/plugin-legacy` (`@vitejs/plugin-legacy`), `packages/vite` — per-package commands: `agentbrief brief <dir>` (if installed), otherwise see each package's manifest.

Do not edit by hand: lockfiles `pnpm-lock.yaml`.

Other agent instructions: `.github/copilot-instructions.md`.
```

Every command carries its source in parentheses. `agentbrief sync` writes exactly this between `<!-- agentbrief:start -->` and `<!-- agentbrief:end -->` markers in `AGENTS.md`. More generated briefs are in [`examples/`](examples/): [httpx](examples/httpx.md), [ripgrep](examples/ripgrep.md), [Spring PetClinic](examples/spring-petclinic.md).

## Should I use this, or just ask an agent to write AGENTS.md?

Use both: they do different jobs. We measured this rather than assuming it (details in [Evaluation](#evaluation)):

- **A frontier model writes an accurate AGENTS.md.** An independent audit of `AGENTS.md` files that Claude Sonnet wrote for 8 well-known repositories found 89 of 89 commands correct, and the files held things AgentBrief deliberately leaves out: gotchas that block a test run, real single-test examples, architecture notes, contribution policy. Write that kind of file, by hand or with `/init`.
- **Nothing keeps it true afterwards.** That is what `agentbrief check` is for. It is deterministic, takes milliseconds, costs no tokens, and runs on every pull request.
- **The AgentBrief block is a small, self-checking baseline.** It covers only operational facts, carries the source of each one, and is about a third the size of a generated file (8 KB against 24 KB across those 8 repos). If you have no `AGENTS.md` yet, it is a correct place to start. If you have a good one, the block is optional, and `check` alone is the main value.

Two things the research and our own runs agree on:
- **More context is not automatically better.** Gloaguen et al. (ETH Zurich, ICLR 2026, [arXiv 2602.11988](https://arxiv.org/abs/2602.11988)) found LLM-generated context files slightly reduced task success and raised cost by about 20%. Developer-written files with minimal operational facts helped by about 4%. Keep instruction files short and specific.
- **On well-documented repositories, a strong agent orients itself fine without any file.** In our benchmark, no file, an LLM-written file and an AgentBrief block all scored about 4.8-4.9 out of 5 on orientation questions, at the same cost. AgentBrief does not make agents faster there, and this README does not claim it does.

Claude Code reads `CLAUDE.md`, not `AGENTS.md`, and ignores `AGENTS.md` when a `CLAUDE.md` exists that does not import it. `agentbrief sync` adds an `@AGENTS.md` line to an existing root `CLAUDE.md` / `GEMINI.md`, and `check` warns if it is missing.

### When not to use it

AgentBrief writes no architecture prose, no code maps and no style advice, and it will not tell an agent what your project is for. If you already maintain a good `AGENTS.md`, you may not need `sync` at all: run `agentbrief check` alone to keep it honest. The check rules lint hand-written files without requiring the managed block.

## Install

Go 1.23 or newer:

```sh
go install github.com/seanmcmahon101/AgentBrief/cmd/agentbrief@latest
```

Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are attached to each [GitHub Release](https://github.com/seanmcmahon101/AgentBrief/releases): download, extract, and put `agentbrief` on your `PATH`.

From source:

```sh
git clone https://github.com/seanmcmahon101/AgentBrief
cd AgentBrief
make build        # writes ./agentbrief
```

## Usage

Four workflows:

1. **First time.** Run `agentbrief sync` at the repository root and commit the result. It creates `AGENTS.md` (or updates only the managed block in an existing one) and adds `@AGENTS.md` to an existing root `CLAUDE.md` / `GEMINI.md`.
   ```console
   $ agentbrief sync
   AGENTS.md: created
   $ agentbrief sync
   AGENTS.md: unchanged
   ```
   Preview with `agentbrief sync --dry-run`.
2. **In CI.** Run `agentbrief check`. It exits non-zero when the block is stale or an instruction file references scripts or paths that no longer exist.
3. **For agents, on demand.** `agentbrief brief <path> --json` returns the facts for the package that owns `<path>`.
4. **Before completing a change.** `agentbrief plan <changed-files> --json --strict` selects configured checks and their prerequisites. Review unmatched paths and execute appropriate checks yourself.

### Commands

| Command | Purpose |
|---|---|
| `agentbrief [brief] [PATH] [--json] [--all]` | Print the facts block (default command). With `PATH`, scope to the project that owns it: its own commands first, then root commands that still apply. `--json` emits schema `agentbrief/brief/v1`; `--all` with `--json` prints the full repository model. |
| `agentbrief sync [--dry-run] [--no-imports]` | Write or refresh the managed block in `AGENTS.md`. Idempotent: unchanged files are not rewritten. In `AGENTS.md` only the text between the markers changes; the one other edit is a single `@AGENTS.md` line added at the top of an existing root `CLAUDE.md` / `GEMINI.md`, which `--no-imports` skips. |
| `agentbrief check [--json] [--strict]` | Verify the block is current and lint instruction files. `--json` emits schema `agentbrief/check/v1`. `--strict` makes warnings fail. |
| `agentbrief plan PATH... [--json] [--strict]` | Select declared checks for changed filenames, including prerequisites. `--files-from FILE` (or `-` for stdin) accepts lists; `--null` accepts NUL delimiters. JSON schema `agentbrief/plan/v1`. |
| `agentbrief version` | Print the version. |
| `-C DIR` (all commands) | Repository root. Default: the nearest ancestor containing `.git`, otherwise the current directory. |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success / `check` found no errors |
| 1 | `check` found errors (or warnings with `--strict`), or `plan --strict` found incomplete declared mappings |
| 2 | Usage or I/O error (for example `-C` pointing at a missing directory) |

## For coding agents

If you are an agent working in a repository that uses AgentBrief:

- Read `AGENTS.md` first. The block between the `agentbrief` markers lists the package manager, the commands to use and the files not to edit by hand.
- In a monorepo, run `agentbrief brief <dir> --json` for the package you are about to change, before running its build or tests. Commands whose `dir` is `.` run from the repo root; others run from the given directory.
- Use maintainer-selected commands (`"preferred": true`) when supplied. Otherwise, prefer commands with `"verified": true`, which confirms repository evidence rather than successful execution. Commands labelled `convention` are the usual commands for the tool and have not been confirmed against the repository.
- Before finishing, supply the actual changed filenames to `agentbrief plan --json`. Follow prerequisites and working directories, inspect unmatched files and protected-path notices, and run the checks yourself. Even `mapping_complete: true` leaves test coverage unknown.
- If `check` reports an unknown script, trust the repository's manifest over the instruction file.

Excerpt of `agentbrief brief packages/vite --json` in Vite (trimmed with `…`):

```json
{
  "schema": "agentbrief/brief/v1",
  "scope": "packages/vite",
  "project": {
    "dir": "packages/vite",
    "name": "vite",
    "ecosystem": "node",
    "manifest": "packages/vite/package.json"
  },
  "package_managers": [
    {
      "name": "pnpm",
      "version": "12.9.1",
      "ecosystem": "node",
      "source": "package.json",
      "avoid": ["npm", "yarn"]
    }
  ],
  "commands": [
    {
      "kind": "build",
      "run": "pnpm build",
      "dir": "packages/vite",
      "source": "package.json, CI",
      "evidence": ["ci", "declared"],
      "verified": true
    },
    …
    {
      "kind": "test-single",
      "run": "pnpm exec vitest run <file> -t \"<name>\"",
      "dir": ".",
      "source": "vitest convention, unverified",
      "evidence": ["convention"],
      "verified": false
    }
  ],
  …
}
```


## CI and pre-commit

GitHub Actions, one step:

```yaml
name: agentbrief
on: [pull_request, push]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: seanmcmahon101/AgentBrief@v0.1.0
```

The action installs the matching AgentBrief version and runs `agentbrief check`. Pass `with: { args: "check --strict" }` to fail on warnings too (a `CLAUDE.md` that does not import `AGENTS.md`, a wrong package manager or runtime version in the instructions, an oversized file). Without `--strict`, warnings are printed and the step passes.

[pre-commit](https://pre-commit.com) (`.pre-commit-config.yaml`):

```yaml
repos:
  - repo: https://github.com/seanmcmahon101/AgentBrief
    rev: v0.1.0
    hooks:
      - id: agentbrief-check
      # - id: agentbrief-sync   # optional: refresh the AGENTS.md block on every commit
```

More setups, including per-agent notes, are in [docs/integrations.md](docs/integrations.md).

## What it detects

Ecosystems and what is read (all static parsing of files in the repository):

| Area | Files and tools |
|---|---|
| Node / TypeScript | `package.json` (scripts, `packageManager`, workspaces, engines), lockfiles for npm, pnpm, yarn, bun; test runners vitest, jest, mocha, node:test, bun test, playwright; Deno (`deno.json`) |
| Python | `pyproject.toml`, `Pipfile`, `setup.py`, `setup.cfg`, `requirements*.txt`, `uv.lock`; uv, poetry, pdm, hatch, pipenv, pip; poe, pdm, hatch, tox and nox task inventories; pytest, ruff, mypy conventions |
| Go | `go.mod`, `go.work`, `//go:generate` |
| Rust | `Cargo.toml` workspaces, `Cargo.lock` |
| JVM | `pom.xml`, Maven wrapper, `build.gradle(.kts)`, `settings.gradle(.kts)`, `gradlew` |
| Ruby | `Gemfile`, `Gemfile.lock`, `Rakefile` and `lib/tasks/*.rake`, minitest / RSpec |
| PHP | `composer.json`, `composer.lock`, PHPUnit, Psalm, Pint, PHP_CodeSniffer |
| .NET | `.sln`, `.slnx`, `.csproj`, `.fsproj`, `.vbproj`, `global.json` |
| Elixir | `mix.exs`, `mix.lock`, `.credo.exs` |
| Swift | `Package.swift`, `.swiftlint.yml` |
| C / C++ | `CMakeLists.txt`, `CMakePresets.json`, `meson.build`, `.bazelversion` |
| Task runners | `Makefile` / `GNUmakefile`, `justfile`, `Taskfile.yml`, mise (`mise.toml`, mise task directories), `scripts/` directories |
| CI | GitHub Actions workflows (`.github/workflows/*.yml`, `*.yaml`), `.gitlab-ci.yml` |
| Other | Docker Compose services, generated-file markers and lockfiles ("do not edit"), toolchain pins (`.nvmrc`, `.node-version`, `.python-version`, `.tool-versions`, `rust-toolchain.toml`, versions in CI setup actions), existing agent instruction files |

Instruction files that `check` lints include `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.cursorrules`, `.cursor/rules/`, `.windsurfrules`, `.windsurf/rules/`, `CONVENTIONS.md`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md` and `.claude/rules/`.

### Check rules

| Rule | Severity | Meaning |
|---|---|---|
| `invalid-config` | error | `.agentbrief.yaml` has malformed or unsupported workflow declarations. |
| `stale-block` | error | The managed block differs from what `sync` would write now. |
| `unknown-script` | error | An instruction file tells agents to run a script, target or task that does not exist: `npm run X` / `pnpm X` / `yarn X` / `bun run X`, `make X` (including `make -C dir X`), `just X`, `task X`, `mise run X`, `composer X`, `poe X`, `tox -e X`, `nox -s X`, `cargo … -p X` (workspace crates), `docker compose up X` (services). Warning instead of error when the inventory may be incomplete. |
| `missing-path` | error for Markdown links and paths inside commands; warning for code spans and prose | A referenced repository path does not exist: Markdown links, code spans that look like paths, and path arguments of commands such as `pytest tests/x.py`, `python scripts/x.py`, `bash scripts/x.sh`, `go test ./pkg/...`. |
| `wrong-package-manager` | warning | Instructions use one JS/Python package manager but the lockfile says another. |
| `stale-version` | warning | Prose names a runtime version (Node, Python, Go, Ruby, Java) that disagrees with the repository's exact pin (`.nvmrc`, `.python-version`, …). Floors ("Node 18+"), history and compatibility sentences, code fences and the managed block are ignored. |
| `agents-md-not-loaded` | warning | A root `CLAUDE.md` (or `GEMINI.md`) exists next to `AGENTS.md` but does not import it. |
| `oversized` | warning | A file is over 32 KiB (Codex's default cap) or `CLAUDE.md` is over 200 lines. |

The rules are conservative by design. Placeholders (`<file>`, `$VAR`, `{x}`), globs, URLs, absolute paths and built-in package-manager subcommands (`pnpm install`, `yarn add`, `npm ci`) are not reported. Fenced code blocks are checked for commands (including their path arguments) but not for bare paths, and commands mentioned in negated or hypothetical wording ("don't run …", "for example …") are skipped.

### Evidence labels

The parentheses after each command say where it came from:

| Label | Meaning |
|---|---|
| `package.json`, `Makefile`, `pom.xml` … (a file name) | **Declared**: the command is defined in that file (a script, target, recipe or plugin configuration). |
| `CI` | A CI workflow runs this command. Shown together with the file when both apply, for example `(package.json, CI)`. |
| `.agentbrief.yaml, maintainer-selected; execution unverified` | **Maintainer choice**: explicit scoped preference; not executed. JSON includes `preferred: true` and declaration file/line. |
| `… convention, unverified` | **Convention**: the standard command for that tool (for example `cargo test`), inferred from the tool being present. Not confirmed anywhere in the repository. |

When several candidates exist for one kind of command, the best is shown in this order: declared and run in CI; declared; run in CI; convention. At equal evidence, the repository's own task runner wins over the raw tool. Convention-only commands are shown only when nothing better exists.

## Evaluation

The historical benchmark below predates maintainer workflow policies and validation plans. The [new case studies](examples/case-studies/README.md) test static selection and drift, without measuring agent outcomes.

The benchmark numbers come from runs in [`bench/`](bench/), which holds the harness, the hand-written ground truth, every graded result and a plain-language write-up ([`bench/results/FINDINGS.md`](bench/results/FINDINGS.md)).

**Accuracy of the facts block.** An independent reviewer judged every command AgentBrief emitted for 17 real repositories (Node, Python, Go, Rust, Java, Ruby, PHP, .NET, Elixir; monorepos and polyglot repos): 94 of 105 correct (89%). The remaining 11 were convention commands that the repository does not actually use (for example `go vet` as the lint command where none is configured); they are labelled `convention, unverified` in the output. On the 8 benchmark repositories the block had about 48 of 50 commands correct.

**Drift detection.** The same reviewer planted 15 realistic kinds of drift in instruction files; `check` caught 10 (missing scripts, Makefile targets, crates, poe tasks, tox environments, compose services, test files, scripts and Go packages). It did not catch commands of unknown tools (`uv run nothing`, `mage build`). On the 17 repositories as they are, `check` raised one finding (a real one: Excalidraw's `CLAUDE.md` does not import its `AGENTS.md`). On a stress file made of every README and CONTRIBUTING file in the corpus (8,400 lines of prose and examples), every error it raised was a genuinely missing file.

**`check` on LLM-written files.** On the 8 `AGENTS.md` files Claude Code wrote for the benchmark, `check` reported nothing (8 of 8 clean, no false positives). After the drift experiment renamed scripts and test targets in 4 of those repositories, it flagged the stale instructions in 3 of them with 10 errors, all genuine ([`bench/results/check-llm-files.md`](bench/results/check-llm-files.md)). The benchmark's agents mostly worked around those stale lines by re-reading manifests; `check` makes the file itself correct again.

**Agent orientation benchmark.** A read-only Claude Code session answered five questions (install, full test suite, single test file, CI lint, generated files) about 8 repositories, graded blind against hand-written ground truth. Three arms: no instruction file, an `AGENTS.md` written by Claude Code, and the AgentBrief block.

| Model | Runs per arm | No file | LLM-written file | AgentBrief | Turns (no file / AgentBrief) |
|---|---|---|---|---|---|
| Claude Sonnet | 24 | 4.88 / 5 | 4.79 / 5 | 4.88 / 5 | 6.6 / 6.1 |
| Claude Haiku | 16 | 4.91 / 5 | 4.88 / 5 | 4.81 / 5 | 12.3 / 13.2 |

Differences in accuracy, turns, tokens and cost are all within noise. A constructed drift experiment (the LLM-written file left stale after renaming scripts in 4 repositories) was also null: agents re-read the manifests and mostly ignored the stale lines. The honest summary: on well-documented repositories, today's agents do not need help answering these questions, and AgentBrief's value is in keeping instruction files correct and cheap to maintain, not in speeding agents up. The benchmark did not test end-to-end coding tasks, poorly documented repositories, or long-running drift in real projects.

The harness, ground truth and threats to validity are described in [bench/README.md](bench/README.md).

## Security model

Repository contents are treated as untrusted.

- AgentBrief never executes anything from the repository, and that includes `git` (a repository's `.git/config` can run commands through hooks such as `core.fsmonitor`). Detectors only parse files.
- Files matching a sensitive-file list (`.env*`, private keys, certificates, credentials) are never read.
- Symlinks are not followed. Reads are capped at 1 MiB.
- Every repository-derived string that reaches output is length-capped and passed through secret redaction. Command lines are capped at 160 characters.
- No free-form prose (README text, task descriptions) is copied into the generated block; only identifiers, paths, versions and command lines. These are validated when the facts are built, so a value that fails (a version like `20 ; curl …`, a package named like an instruction) is dropped from every output, JSON included. This limits prompt-injection and secret-leak surface.
- `sync` writes only `AGENTS.md`, `CLAUDE.md` and `GEMINI.md` at the root, and changes only the text between its markers plus the single import line.

See [SECURITY.md](SECURITY.md) to report a vulnerability.

## Limitations

- **Validation plans require explicit mappings.** They do not discover transitive source dependencies, prove tests sufficient, or establish that the caller supplied all changed files. The public examples are partial policies, not complete CI replacements.

- **Static analysis only.** Commands are read from files, never run. A command that looks right can still fail at runtime.
- **Convention commands are unverified.** They are labelled as such; treat them as defaults.
- **`check` is heuristic and conservative.** It prefers missing a problem to raising a false alarm, so a clean result does not prove the instruction files are correct. It checks what it can establish from files: scripts, targets, paths, package manager and the managed block.
- **Git is not invoked**, so AgentBrief does not know what is tracked. Untracked files in the working tree can change the result, and `.gitignore` is honoured by parsing it directly. Run it on a clean checkout (as CI does) for reproducible output.
- **Nested `AGENTS.md` files are not generated.** Per-package detail is served on demand by `agentbrief brief <dir>`.
- Ecosystems not listed above are not detected; the block then says little rather than guessing.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Reports of wrong or misleading facts are especially welcome; there is an issue template for them.

## License

MIT, see [LICENSE](LICENSE).
