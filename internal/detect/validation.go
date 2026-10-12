package detect

import (
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// workflowCommand is shared by preferences and validation checks: working
// directory, output safety, and provenance have exactly one interpretation.
func workflowCommand(m map[string]*yaml.Node, r *scan.Repo) (model.Command, bool) {
	kind, validKind := workflowString(m["kind"])
	run, validRun := workflowString(m["run"])
	dir, validDir := workflowPath(m["dir"], ".")
	if !validKind || !workflowKind(kind) || !validRun || strings.TrimSpace(run) != run ||
		!model.ValidCommand(run) || !validDir || !r.IsDir(dir) {
		return model.Command{}, false
	}
	run = redact.String(run)
	if !model.ValidCommand(run) {
		return model.Command{}, false
	}
	return model.Command{Kind: kind, Run: run, Dir: dir,
		Evidence: []string{model.EvidenceMaintainer},
		Source:   model.Source{Path: WorkflowFile, Line: m["run"].Line}}, true
}

func validationChecks(node *yaml.Node, r *scan.Repo) ([]model.ValidationCheck, bool) {
	checks := []model.ValidationCheck{}
	if node == nil {
		return checks, true
	}
	if node.Kind != yaml.SequenceNode {
		return nil, false
	}
	ids := map[string]bool{}
	for _, cn := range node.Content {
		m, ok := workflowMapping(cn, "id", "kind", "run", "dir", "paths", "exclude", "requires")
		id, validID := workflowString(m["id"])
		cmd, validCmd := workflowCommand(m, r)
		paths, validPaths := validationSelectors(m["paths"])
		exclude, validExclude := validationSelectors(m["exclude"])
		requires, validRequires := validationRequires(m["requires"])
		if !ok || !validID || !model.ValidName(id) || ids[id] || !validCmd ||
			!validPaths || !validExclude || !validRequires || len(exclude) > 0 && len(paths) == 0 {
			return nil, false
		}
		ids[id] = true
		checks = append(checks, model.ValidationCheck{ID: id, Command: cmd,
			Paths: paths, Exclude: exclude, Requires: requires,
			Source: model.Source{Path: WorkflowFile, Line: cn.Line}})
	}
	// Kahn's algorithm validates references and cycles without recursive depth
	// depending on untrusted YAML. Selection later consumes these parsed facts.
	indegree := map[string]int{}
	dependents := map[string][]string{}
	for _, check := range checks {
		indegree[check.ID] = len(check.Requires)
		for _, id := range check.Requires {
			if !ids[id] {
				return nil, false
			}
			dependents[id] = append(dependents[id], check.ID)
		}
	}
	queue := []string{}
	for _, check := range checks {
		if indegree[check.ID] == 0 {
			queue = append(queue, check.ID)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, id := range dependents[queue[i]] {
			indegree[id]--
			if indegree[id] == 0 {
				queue = append(queue, id)
			}
		}
	}
	if len(queue) != len(checks) {
		return nil, false
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })
	return checks, true
}

func validationSelectors(node *yaml.Node) ([]model.PathSelector, bool) {
	out := []model.PathSelector{}
	if node == nil {
		return out, true
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, item := range node.Content {
		p, ok := workflowString(item)
		if !ok || !validationPattern(p) || seen[p] {
			return nil, false
		}
		seen[p] = true
		out = append(out, model.PathSelector{Pattern: p,
			Source: model.Source{Path: WorkflowFile, Line: item.Line}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out, true
}

func validationPattern(p string) bool {
	if !model.ValidPattern(p) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "**") && seg != "**" {
			return false
		}
	}
	return true
}

func validationRequires(node *yaml.Node) ([]string, bool) {
	out := []string{}
	if node == nil {
		return out, true
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, item := range node.Content {
		id, ok := workflowString(item)
		if !ok || !model.ValidName(id) || seen[id] {
			return nil, false
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out, true
}
