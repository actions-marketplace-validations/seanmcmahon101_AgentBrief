# Contributing to AgentBrief

Thanks for helping. Read [docs/DESIGN.md](docs/DESIGN.md) first; it is the product spec, and [AGENTS.md](AGENTS.md) lists the ground rules.

## Setup

You need Go 1.23 or newer (`mise install` works if you use mise; see `mise.toml`).

```sh
git clone https://github.com/seanmcmahon101/AgentBrief
cd AgentBrief
make check
```

`make check` is what CI runs: `go vet`, a `gofmt` check, `go test ./...`, and then `agentbrief check` on this repository's own `AGENTS.md`. Other targets: `make build` (writes `./agentbrief`), `make test`, `make lint`, `make fmt`.

If you change what AgentBrief would generate for this repository, run `go run ./cmd/agentbrief sync` and commit the updated `AGENTS.md`, or CI will fail with `stale-block`.

## Golden outputs

`testdata/golden/` holds brief, JSON, check and scoped output for the pinned benchmark corpus (`bench/corpus.sh`, at `$AGENTBRIEF_CORPUS`).
`make golden` regenerates it and fails on any difference; `make golden-update` rewrites it.
Behaviour changes must come with an intentional golden update: run `make golden-update`, then review the diff of `testdata/golden` line by line before committing.
An unexplained change in a repo you did not target is a regression, not noise.

## Ground rules

- Never execute anything from a scanned repository, including `git`. Detectors only parse files.
- Every fact needs a `Source`. A wrong command is worse than no command; when unsure, emit nothing.
- Output must stay byte-for-byte deterministic: no timestamps, absolute paths or map-order iteration.
- Dependencies are the standard library plus BurntSushi/toml and go.yaml.in/yaml/v3. Open an issue before adding one.
- Do not copy free-form repository prose into output. Identifiers, paths, versions and command lines only.

## Adding a detector

1. Create `internal/detect/<ecosystem>.go` and register it from `init()`:
   ```go
   func init() { register("myeco", detectMyEco) }

   func detectMyEco(r *scan.Repo, f *Facts) { … }
   ```
   Read files only through the `*scan.Repo` (`r.Has`, `r.ReadString`, `r.Glob`). Add facts through the helpers on `*Facts` (`AddProject`, `AddToolchain`, `AddPackageManager`, and so on); look at `golang.go` or `rust.go` for a small example.
2. Give every fact a source file and line (`at(path, line)`). Mark commands you cannot find in a file as convention, so they render as unverified.
3. Be conservative. If the manifest is ambiguous, malformed or a fixture (see `noisyPath`), emit nothing. Prefer the repository's own task runner or script over the raw tool.
4. Test with `scan.FromMap`, which builds an in-memory repository from a `map[string]string`, so tests never touch the disk. Cover the true positive, a monorepo layout, and at least one case where the detector must say nothing. See `internal/detect/*_test.go` for the pattern.
5. Update the "What it detects" table in `README.md`.

## Adding a check rule

Rules live in `internal/instructions` (`lint*.go`, `check.go`). A new rule needs:

- an entry in the rule list and in the table in `docs/DESIGN.md` and `README.md`;
- true-positive tests **and** false-positive tests (placeholders, globs, URLs, fenced blocks, built-in subcommands, repositories without the relevant tool). A CI gate that cries wolf gets deleted, so a rule that cannot demonstrate its precision will not be merged;
- the lowest severity that is honest: `error` only when the instruction is certainly wrong.

## Commits and pull requests

- Keep changes focused; one logical change per PR.
- `make check` must pass. Add or update tests for behaviour changes.
- Explain the why in the PR description. For a wrong-fact fix, link the repository that exposed it and add the minimal `scan.FromMap` reproduction as a test.
- Use imperative commit subjects ("Detect Taskfile includes"), under about 72 characters.

## Reporting wrong facts

Use the "Wrong fact" issue template. Give the repository URL, the command AgentBrief emitted, and what is correct. Those reports are the most useful ones we get.

## Security

Do not open public issues for vulnerabilities; see [SECURITY.md](SECURITY.md).
