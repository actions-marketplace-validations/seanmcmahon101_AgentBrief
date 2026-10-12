package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/seanmcmahon101/AgentBrief/internal/plan"
)

// File lists share the scanner's 1 MiB input budget. Read one extra byte to
// distinguish complete input from truncation; never silently plan a prefix.
const maxPlanInput = 1 << 20

const usagePlan = `Usage: agentbrief plan PATH... [-C DIR] [--json] [--strict]
       agentbrief plan --files-from FILE [--null] [-C DIR] [--json] [--strict]

Select checks declared in .agentbrief.yaml for changed files, including their
prerequisites. Paths are repository-relative filenames; deleted files are valid.
Include both old and new names for renames. AgentBrief never invokes git or runs
commands. No declared mapping means unknown, not that a check is unnecessary.

  --files-from FILE  newline-separated file list; - reads standard input
  --null             file list is NUL-delimited (e.g. git diff --name-only -z)
  --json             structured plan (schema agentbrief/plan/v1)
  --strict           exit 1 for unmapped paths or an incomplete repository scan
  -C DIR             repository root

Exit: 0 plan produced, 1 incomplete mapping with --strict, 2 usage/IO/config error.
`

func runPlan(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir := newFlags("plan")
	asJSON := fs.Bool("json", false, "")
	strict := fs.Bool("strict", false, "")
	from := fs.String("files-from", "", "")
	nul := fs.Bool("null", false, "")
	pos, code, done := parse(fs, args, usagePlan, stdout, stderr)
	if done {
		return code
	}
	if *nul && *from == "" {
		return fail(stderr, errors.New("--null requires --files-from"))
	}
	paths := append([]string{}, pos...)
	total := 0
	for _, p := range paths {
		total += len(p) + 1
	}
	if *from != "" {
		input := stdin
		if *from != "-" {
			file, err := os.Open(*from)
			if err != nil {
				return fail(stderr, errors.New("cannot open --files-from input"))
			}
			defer file.Close()
			input = file
		}
		b, err := io.ReadAll(io.LimitReader(input, maxPlanInput+1))
		if err != nil {
			return fail(stderr, errors.New("cannot read the complete changed-file list"))
		}
		total += len(b)
		if total > maxPlanInput {
			return fail(stderr, errors.New("changed-file input exceeds 1 MiB"))
		}
		sep := []byte{'\n'}
		if *nul {
			sep = []byte{0}
		}
		for _, line := range bytes.Split(b, sep) {
			if !*nul {
				line = bytes.TrimSuffix(line, []byte{'\r'})
			}
			if len(line) > 0 {
				paths = append(paths, string(line))
			}
		}
	}
	if total > maxPlanInput {
		return fail(stderr, errors.New("changed-file input exceeds 1 MiB"))
	}
	if len(paths) == 0 {
		return fail(stderr, errors.New("plan needs at least one changed filename"))
	}
	for i, file := range paths {
		p, ok := changedPath(file)
		if !ok {
			return fail(stderr, errors.New("changed paths must be relative filenames without traversal or control characters"))
		}
		paths[i] = p
	}
	root, _, err := findRoot(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	repo, err := load(root)
	if err != nil {
		return fail(stderr, err)
	}
	if err := policyError(root, repo); err != nil {
		return fail(stderr, err)
	}
	p, err := plan.Build(repo, paths)
	if err != nil {
		return fail(stderr, err)
	}
	if *asJSON {
		err = writeJSON(stdout, p)
	} else {
		err = p.Write(stdout)
	}
	if err != nil {
		return fail(stderr, err)
	}
	if *strict && !p.MappingComplete {
		return ExitFindings
	}
	return ExitOK
}

func changedPath(p string) (string, bool) {
	if p == "" || !utf8.ValidString(p) || strings.ContainsAny(p, "\\:") || strings.HasPrefix(p, "/") {
		return "", false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", false
		}
	}
	p = path.Clean(p)
	if p == "." {
		return "", false
	}
	return p, true
}
