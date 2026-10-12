// Package cli implements the agentbrief command line.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/brief"
	"github.com/seanmcmahon101/AgentBrief/internal/detect"
	"github.com/seanmcmahon101/AgentBrief/internal/instructions"
	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// Version is set at build time:
//
//	go build -ldflags "-X github.com/seanmcmahon101/AgentBrief/internal/cli.Version=1.2.3"
var Version = "dev"

// Exit codes.
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitUsage    = 2
)

// CheckSchema identifies the JSON output of `agentbrief check --json`.
const CheckSchema = "agentbrief/check/v1"

const usageMain = `agentbrief - verified repo facts for coding agents

Usage:
  agentbrief [brief] [PATH] [--json] [--all]   print the brief (default command)
  agentbrief sync [--dry-run] [--no-imports]   write/refresh the facts block in AGENTS.md
  agentbrief check [--json] [--strict]         fail if the block or instruction files are stale
  agentbrief version

Global flag:
  -C DIR    repository root (default: nearest parent containing .git, else cwd)

Optional .agentbrief.yaml selects scoped workflows; see docs/workflows.md.
AgentBrief reads manifests, lockfiles, task runners and CI files (never running
anything), and writes only commands and versions it can source. 'check' exits
1 on errors (or warnings with --strict), 2 on usage errors.

Examples:
  agentbrief sync                  create/update the block in AGENTS.md
  agentbrief brief packages/api    commands for the package owning that path
  agentbrief check --strict        use in CI

Run 'agentbrief <command> -h' for command help.
`

const usageBrief = `Usage: agentbrief [brief] [PATH] [-C DIR] [--json] [--all]

Print the facts block an agent needs: toolchain, package manager, the best
command per task (install, build, test, ...), workspace layout, and files not
to edit. With PATH (a file or directory), scope it to the project that owns
PATH: its own commands first, then root commands that still apply.

  --json    structured output (schema agentbrief/brief/v1)
  --all     with --json, print the full repository model instead
  -C DIR    repository root
`

const usageSync = `Usage: agentbrief sync [-C DIR] [--dry-run] [--no-imports]

Write the facts block between <!-- agentbrief:start --> and <!-- agentbrief:end -->
in AGENTS.md (created if missing). Text outside the markers is never touched.
Adds an '@AGENTS.md' line to an existing root CLAUDE.md / GEMINI.md so those
agents load it. Idempotent: unchanged files are not rewritten.

  --dry-run      show what would change, write nothing
  --no-imports   do not touch CLAUDE.md / GEMINI.md
  -C DIR         repository root
`

const usageCheck = `Usage: agentbrief check [-C DIR] [--json] [--strict]

Verify the AgentBrief block is current and lint every agent-instruction file
(AGENTS.md, CLAUDE.md, Cursor/Copilot/Windsurf/... rules).

Rules: invalid-config, stale-block, unknown-script, missing-path, wrong-package-manager,
agents-md-not-loaded, stale-version, oversized.

  --json     machine-readable findings (schema agentbrief/check/v1)
  --strict   treat warnings as failures
  -C DIR     repository root

Exit: 0 clean, 1 errors (or warnings with --strict), 2 usage/IO error.
`

// Main runs the CLI. args includes the program name (like os.Args).
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		args = args[1:]
	}
	cmd, rest := splitCommand(args)
	switch cmd {
	case "help":
		if len(rest) > 0 {
			switch rest[0] {
			case "brief":
				fmt.Fprint(stdout, usageBrief)
			case "sync":
				fmt.Fprint(stdout, usageSync)
			case "check":
				fmt.Fprint(stdout, usageCheck)
			default:
				fmt.Fprint(stdout, usageMain)
			}
			return ExitOK
		}
		fmt.Fprint(stdout, usageMain)
		return ExitOK
	case "version":
		fmt.Fprintf(stdout, "agentbrief %s\n", versionString())
		return ExitOK
	case "sync":
		return runSync(rest, stdout, stderr)
	case "check":
		return runCheck(rest, stdout, stderr)
	default:
		return runBrief(rest, stdout, stderr)
	}
}

// splitCommand finds the subcommand among args (skipping flags and the value
// of -C). Anything else, including a path, selects the default `brief`.
func splitCommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version":
			return "version", nil
		case a == "-C" || a == "--C":
			i++
		case strings.HasPrefix(a, "-"):
			if a == "-h" || a == "--help" || a == "-help" {
				// top-level help unless a subcommand follows
				for _, b := range args[i+1:] {
					switch b {
					case "brief", "sync", "check":
						return b, append(append([]string(nil), args[:i]...), append([]string{"-h"}, args[i+1:]...)...)
					}
				}
				return "help", nil
			}
		default:
			switch a {
			case "brief", "sync", "check", "version", "help":
				rest := append(append([]string(nil), args[:i]...), args[i+1:]...)
				if a == "help" {
					// keep positional topic only
					var pos []string
					for _, r := range rest {
						if !strings.HasPrefix(r, "-") {
							pos = append(pos, r)
						}
					}
					return a, pos
				}
				return a, rest
			}
			return "brief", args
		}
	}
	return "brief", args
}

func versionString() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// newFlags makes a quiet FlagSet with the shared -C flag.
func newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("C", "", "")
	return fs, dir
}

// parse parses interspersed flags and positionals. Returns (positionals, exit, done).
func parse(fs *flag.FlagSet, args []string, usage string, stdout, stderr io.Writer) ([]string, int, bool) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprint(stdout, usage)
				return nil, ExitOK, true
			}
			fmt.Fprintf(stderr, "agentbrief: %v\nRun 'agentbrief help' for usage.\n", err)
			return nil, ExitUsage, true
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, 0, false
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), 0, false
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "agentbrief: %v\n", err)
	return ExitUsage
}

// findRoot returns the repository root: dir if given, else the nearest ancestor
// of the working directory containing .git (dir or file), else the working directory.
func findRoot(dir string) (root, base string, err error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", "", err
		}
		fi, err := os.Stat(abs)
		if err != nil || !fi.IsDir() {
			return "", "", fmt.Errorf("-C %s: not a directory", dir)
		}
		return abs, abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	for d := cwd; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, cwd, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return cwd, cwd, nil
		}
		d = parent
	}
}

func load(root string) (*model.Repo, error) {
	sr, err := scan.Open(root, scan.Options{})
	if err != nil {
		return nil, err
	}
	return detect.Run(sr), nil
}

// resolveScope turns a user path into a repo-relative slash path inside root.
func resolveScope(root, base, p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(base, p)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository (%s)", p, root)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("%s: no such file or directory", p)
	}
	return filepath.ToSlash(rel), nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func runBrief(args []string, stdout, stderr io.Writer) int {
	fs, dir := newFlags("brief")
	asJSON := fs.Bool("json", false, "")
	all := fs.Bool("all", false, "")
	pos, code, done := parse(fs, args, usageBrief, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 1 {
		return fail(stderr, errors.New("brief takes at most one PATH"))
	}
	if *all && !*asJSON {
		return fail(stderr, errors.New("--all requires --json"))
	}
	root, base, err := findRoot(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	scope := "."
	if len(pos) == 1 {
		if scope, err = resolveScope(root, base, pos[0]); err != nil {
			if isBareWord(pos[0]) {
				return fail(stderr, fmt.Errorf("%q is neither a command nor an existing path (commands: brief, sync, check, version, help)", pos[0]))
			}
			return fail(stderr, err)
		}
	}
	repo, err := load(root)
	if err != nil {
		return fail(stderr, err)
	}
	if err := workflowError(root, repo); err != nil {
		return fail(stderr, err)
	}
	if *all {
		if err := writeJSON(stdout, repo); err != nil {
			return fail(stderr, err)
		}
		return ExitOK
	}
	b := brief.Build(repo, scope)
	if *asJSON {
		out, err := b.JSON()
		if err != nil {
			return fail(stderr, err)
		}
		stdout.Write(out)
		return ExitOK
	}
	fmt.Fprint(stdout, b.Body())
	return ExitOK
}

func runSync(args []string, stdout, stderr io.Writer) int {
	fs, dir := newFlags("sync")
	dry := fs.Bool("dry-run", false, "")
	noImports := fs.Bool("no-imports", false, "")
	pos, code, done := parse(fs, args, usageSync, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		return fail(stderr, errors.New("sync takes no arguments"))
	}
	root, _, err := findRoot(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	repo, err := load(root)
	if err != nil {
		return fail(stderr, err)
	}
	if err := workflowError(root, repo); err != nil {
		return fail(stderr, err)
	}
	res, err := instructions.Sync(root, repo, instructions.SyncOptions{DryRun: *dry, NoImports: *noImports})
	if err != nil {
		return fail(stderr, err)
	}
	for _, f := range res.Files {
		status := f.Status
		if *dry {
			switch f.Status {
			case instructions.StatusCreated:
				status = "would create"
			case instructions.StatusUpdated:
				status = "would update"
			}
		}
		line := fmt.Sprintf("%s: %s", f.Path, status)
		if f.Reason != "" {
			line += " (" + f.Reason + ")"
		}
		fmt.Fprintln(stdout, line)
		if *dry && f.Diff != "" {
			fmt.Fprint(stdout, f.Diff)
		}
	}
	return ExitOK
}

type checkOutput struct {
	Schema           string          `json:"schema"`
	Findings         []model.Finding `json:"findings"`
	ManagedBlock     bool            `json:"managed_block"`
	FilesChecked     int             `json:"files_checked"`
	WorkflowsChecked int             `json:"workflows_checked,omitempty"`
	Errors           int             `json:"errors"`
	Warnings         int             `json:"warnings"`
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs, dir := newFlags("check")
	asJSON := fs.Bool("json", false, "")
	strict := fs.Bool("strict", false, "")
	pos, code, done := parse(fs, args, usageCheck, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		return fail(stderr, errors.New("check takes no arguments"))
	}
	root, _, err := findRoot(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	repo, err := load(root)
	if err != nil {
		return fail(stderr, err)
	}
	findings := instructions.Check(root, repo, instructions.CheckOptions{})
	if findings == nil {
		findings = []model.Finding{}
	}
	out := checkOutput{Schema: CheckSchema, Findings: findings, FilesChecked: len(repo.Instructions), WorkflowsChecked: len(repo.Workflows)}
	if b, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err == nil {
		_, ferr := instructions.FindBlock(string(b))
		out.ManagedBlock = ferr == nil
	}
	for _, f := range findings {
		if f.Severity == model.SeverityError {
			out.Errors++
		} else {
			out.Warnings++
		}
	}
	if *asJSON {
		if err := writeJSON(stdout, out); err != nil {
			return fail(stderr, err)
		}
	} else {
		for _, f := range findings {
			loc := f.Path
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
			}
			fmt.Fprintf(stdout, "%s: %s: %s [%s]\n", loc, f.Severity, f.Message, f.Rule)
			if f.Suggestion != "" {
				fmt.Fprintf(stdout, "  %s\n", f.Suggestion)
			}
		}
		switch {
		case len(findings) == 0:
			if len(repo.Workflows) > 0 {
				fmt.Fprintf(stdout, "ok: %s and %s checked, no problems found\n", plural(len(repo.Instructions), "instruction file"), plural(len(repo.Workflows), "workflow"))
			} else {
				fmt.Fprintf(stdout, "ok: %s checked, no problems found\n", plural(len(repo.Instructions), "instruction file"))
			}
		default:
			fmt.Fprintf(stdout, "%s, %s\n", plural(out.Errors, "error"), plural(out.Warnings, "warning"))
		}
		if !out.ManagedBlock {
			fmt.Fprintln(stdout, "note: AGENTS.md has no AgentBrief block, so only hand-written instructions were checked; run `agentbrief sync` to add one")
		}
	}
	if out.Errors > 0 || (*strict && out.Warnings > 0) {
		return ExitFindings
	}
	return ExitOK
}

func plural(n int, w string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", w)
	}
	return fmt.Sprintf("%d %ss", n, w)
}

// isBareWord reports whether s looks like a mistyped subcommand rather than a path.
func isBareWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

func workflowError(root string, repo *model.Repo) error {
	for _, f := range instructions.CheckWorkflows(root, repo) {
		if f.Severity == model.SeverityError {
			return errors.New("invalid .agentbrief.yaml workflows; run `agentbrief check` for diagnostics")
		}
	}
	return nil
}
