package instructions

import (
	"fmt"
	"path"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

var jsPMs = set(`npm pnpm yarn bun`)

// subcommands that make a JS package-manager mention an instruction to use it.
var pmUseSubs = map[string]map[string]bool{
	"npm":  set(`install i ci run run-script test`),
	"pnpm": set(`install i add run test`),
	"yarn": set(`install add run test`),
	"bun":  set(`install i add run`),
}

type wrongPM struct {
	c        *checker
	f        model.InstructionFile
	reported map[string]bool
}

// governing returns the package manager of ecosystem governing the file's scope
// when it is certain (lockfile or an explicit version/packageManager pin).
func (w *wrongPM) governing(eco string) (model.PackageManager, bool) {
	scope := w.f.Scope
	if scope == "" {
		scope = "."
	}
	var best model.PackageManager
	found := false
	for _, pm := range w.c.repo.PackageManagers {
		if pm.Ecosystem != eco {
			continue
		}
		if pm.Dir == "." || scope == pm.Dir || strings.HasPrefix(scope, pm.Dir+"/") {
			if !found || len(pm.Dir) > len(best.Dir) {
				best, found = pm, true
			}
		}
	}
	if !found || !certainPM(best) {
		return best, false
	}
	return best, true
}

func certainPM(pm model.PackageManager) bool {
	if pm.Version != "" {
		return true
	}
	b := path.Base(pm.Source.Path)
	return strings.HasSuffix(b, ".lock") || strings.HasSuffix(b, "-lock.yaml") || strings.HasSuffix(b, "-lock.json") ||
		strings.HasSuffix(b, ".lockb") || b == "uv.lock" || b == "poetry.lock" || b == "pdm.lock" || b == "Pipfile.lock"
}

func (w *wrongPM) scan(it mdItem) {
	if negation.MatchString(it.full) {
		return
	}
	for _, toks := range segments(it.text) {
		w.js(it.line, toks)
		w.py(it.line, toks)
	}
}

func (w *wrongPM) js(line int, toks []string) {
	tool := toks[0]
	if !jsPMs[tool] || len(toks) < 2 {
		return
	}
	for _, a := range toks[1:] {
		if a == "-g" || a == "--global" {
			return
		}
	}
	sub := toks[1]
	if !pmUseSubs[tool][sub] {
		return
	}
	pm, ok := w.governing("node")
	if !ok || pm.Name == tool || !jsPMs[pm.Name] || w.reported[tool] {
		return
	}
	w.reported[tool] = true
	cmd := strings.Join(toks[:2], " ")
	w.c.add(model.Finding{Rule: RuleWrongPM, Severity: model.SeverityWarning, Path: w.f.Path, Line: line,
		Message:    fmt.Sprintf("uses `%s`, but this repository uses %s (%s)", cmd, pm.Name, pm.Source.Path),
		Suggestion: fmt.Sprintf("use `%s` instead", pm.Name)})
}

func (w *wrongPM) py(line int, toks []string) {
	if len(toks) >= 3 && (toks[0] == "python" || toks[0] == "python3") && toks[1] == "-m" && toks[2] == "pip" {
		toks = toks[2:]
	}
	if len(toks) < 2 || (toks[0] != "pip" && toks[0] != "pip3") || toks[1] != "install" {
		return
	}
	for _, a := range toks[2:] {
		switch a {
		case "uv", "poetry", "pdm", "pipx", "pipenv", "hatch", "virtualenv":
			return
		}
	}
	pm, ok := w.governing("python")
	if !ok || w.reported["pip"] {
		return
	}
	switch pm.Name {
	case "uv", "poetry", "pdm", "pipenv":
	default:
		return
	}
	w.reported["pip"] = true
	w.c.add(model.Finding{Rule: RuleWrongPM, Severity: model.SeverityWarning, Path: w.f.Path, Line: line,
		Message:    fmt.Sprintf("uses `%s`, but this repository uses %s (%s)", strings.Join(toks[:2], " "), pm.Name, pm.Source.Path),
		Suggestion: fmt.Sprintf("use `%s` instead", pm.Name)})
}
