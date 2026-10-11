package detect

import (
	"path"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("scripts-dir", detectScriptsDir) }

// scriptKinds extends the shared task-name table with the names GitHub's
// "scripts to rule them all" convention uses.
var scriptKinds = map[string]string{"cibuild": model.KindCheck}

// detectScriptsDir finds executable shell scripts in script/ and scripts/ at
// the root and in top-level directories (script/test, scripts/lint.sh, ...).
func detectScriptsDir(r *scan.Repo, f *Facts) {
	for _, file := range r.Files() {
		dir := path.Dir(file)
		base := path.Base(dir)
		if base != "script" && base != "scripts" {
			continue
		}
		owner := path.Dir(dir) // "." or a top-level dir
		if owner != "." && strings.Contains(owner, "/") {
			continue
		}
		name, ext := splitScriptName(path.Base(file))
		if ext != "" && ext != ".sh" && ext != ".bash" {
			continue // .py/.js/.ts scripts need an interpreter we cannot vouch for
		}
		kind := scriptKinds[strings.ToLower(name)]
		if kind == "" {
			kind = KindForTaskName(name)
		}
		if kind == "" || !isShellScript(r, file, ext) {
			continue
		}
		run := base + "/" + path.Base(file)
		if ext != "" {
			run = "bash " + run
		}
		src := model.Source{Path: file}
		f.AddTask(model.Task{Runner: "script", Name: name, Dir: owner, Source: src})
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: owner, Evidence: []string{model.EvidenceDeclared}, Source: src})
	}
}

func splitScriptName(file string) (name, ext string) {
	ext = path.Ext(file)
	if ext == file { // dotfile
		return file, ""
	}
	return strings.TrimSuffix(file, ext), ext
}

// isShellScript accepts .sh/.bash files and extension-less files with a shebang.
func isShellScript(r *scan.Repo, file, ext string) bool {
	if ext != "" {
		return true
	}
	head, err := r.Head(file, 2)
	return err == nil && string(head) == "#!"
}
