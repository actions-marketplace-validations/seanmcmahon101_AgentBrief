# AGENTS.md

Cobra (`github.com/spf13/cobra`): Go library for building CLI apps. Module root is package `cobra`; `doc/` is a sub-package (`package doc`) in the same module. Dependencies: `pflag`, `mousetrap` (Windows), `go.yaml.in/yaml/v3`.

## Commands
- Format check: `make fmt` (gofmt only; fails and prints diff)
- Lint: `make lint` (requires `golangci-lint`; config in `.golangci.yml`, v2 format, also enforces gofmt/goimports)
- Test all: `make test` (runs `go get -v ./...` then `go test -v ./...`); CI uses `make richtest` (needs `richgo`)
- Plain test run: `go test ./...`
- Single test: `go test -run '^TestName$' .` (for docs: `go test -run '^TestName$' ./doc`)
- Default `make` target = `fmt` + `test`

## Architecture
- `command.go` — core `Command` type: command tree, flag sets (local/persistent via pflag), `Find`/`Traverse` (resolve subcommand from args), `ExecuteC` → `execute` (parses flags, runs the PreRun/Run/PostRun hooks). `args.go` holds positional-arg validators; `flag_groups.go` holds flag-group constraints (required together, mutually exclusive, one required). `command_win.go`/`command_notwin.go` are platform-specific.
- Shell completion is split across several files:
  - `completions.go` — shell-agnostic engine: the hidden `__complete` command (`initCompleteCmd`), `getCompletions`, `ShellCompDirective`, and the generated `completion` subcommand.
  - `bash_completions.go` (legacy V1 bash), `bash_completionsV2.go`, `zsh_completions.go`, `fish_completions.go`, `powershell_completions.go` — per-shell script generators that call back into `__complete`.
  - `active_help.go` — Active Help messages delivered through the same completion mechanism.
  - `shell_completions.go` — `MarkFlagRequired`/`MarkFlagCustom` flag annotations used by the completion generators.
- `doc/` — generators that walk the command tree to emit man pages, Markdown, ReST, and YAML (`man_docs.go`, `md_docs.go`, `rest_docs.go`, `yaml_docs.go`, shared helpers in `util.go`).
- User docs are in `site/content/` and `README.md`.
