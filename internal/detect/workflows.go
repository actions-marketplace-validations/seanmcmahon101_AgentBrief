package detect

import (
	"bytes"
	"io"
	"path"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// WorkflowFile is the optional, repository-owned operational policy.
const WorkflowFile = ".agentbrief.yaml"
const RuleInvalidConfig = "invalid-config"

func init() { register("workflows", detectWorkflows) }

// Parse once into syntax nodes, rejecting aliases and duplicate/unknown fields.
// Parser errors may echo secrets, so diagnostics never include their raw text.
func detectWorkflows(r *scan.Repo, f *Facts) {
	if !r.Has(WorkflowFile) {
		return
	}
	fail := func(line int, message string) {
		f.ConfigFindings = append(f.ConfigFindings, model.Finding{
			Rule: RuleInvalidConfig, Severity: model.SeverityError,
			Path: WorkflowFile, Line: line, Message: message,
		})
	}
	b, err := r.Read(WorkflowFile)
	if err != nil || r.Size(WorkflowFile) > scan.MaxReadBytes {
		fail(1, "cannot read the complete workflow configuration")
		return
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc, extra yaml.Node
	if dec.Decode(&doc) != nil || len(doc.Content) != 1 || dec.Decode(&extra) != io.EOF {
		fail(1, "expected one valid workflow YAML document")
		return
	}
	root := doc.Content[0]
	fields, ok := workflowMapping(root, "version", "workflows", "generated", "checks")
	if !ok || fields["version"] == nil || fields["version"].Tag != "!!int" || fields["version"].Value != "1" {
		fail(root.Line, "expected version: 1 and only workflows/generated/checks fields")
		return
	}
	// Invalid configurations are atomic: never silently apply a partial policy.
	var workflows []model.Workflow
	var generated []model.PathRule
	seen := map[string]bool{}
	if list := fields["workflows"]; list != nil {
		if list.Kind != yaml.SequenceNode {
			fail(list.Line, "workflows must be a sequence")
			return
		}
		for _, node := range list.Content {
			m, ok := workflowMapping(node, "scope", "commands")
			scope, validScope := workflowPath(m["scope"], ".")
			commands := m["commands"]
			if !ok || !validScope || seen[scope] || !r.IsDir(scope) || commands == nil || commands.Kind != yaml.SequenceNode || len(commands.Content) == 0 {
				fail(node.Line, "workflow needs a unique existing scope and a nonempty commands sequence")
				return
			}
			seen[scope] = true
			w := model.Workflow{Scope: scope, Source: model.Source{Path: WorkflowFile, Line: node.Line}}
			kinds := map[string]bool{}
			for _, cn := range commands.Content {
				cm, ok := workflowMapping(cn, "kind", "run", "dir")
				cmd, valid := workflowCommand(cm, r)
				if !ok || !valid || kinds[cmd.Kind] {
					fail(cn.Line, "command needs a unique supported kind, a single-line run (1-160 bytes), and an existing repository dir")
					return
				}
				kinds[cmd.Kind] = true
				w.Commands = append(w.Commands, cmd)
			}
			sort.Slice(w.Commands, func(i, j int) bool { return w.Commands[i].Kind < w.Commands[j].Kind })
			workflows = append(workflows, w)
		}
	}
	if list := fields["generated"]; list != nil {
		if list.Kind != yaml.SequenceNode {
			fail(list.Line, "generated must be a sequence of repository-relative patterns")
			return
		}
		for _, node := range list.Content {
			pattern, ok := workflowString(node)
			if !ok || !model.ValidPattern(pattern) || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "..") {
				fail(node.Line, "generated paths must be repository-relative patterns")
				return
			}
			generated = append(generated, model.PathRule{Pattern: pattern, Reason: "generated", Source: model.Source{Path: WorkflowFile, Line: node.Line}})
		}
	}
	checks, valid := validationChecks(fields["checks"], r)
	if !valid {
		fail(fields["checks"].Line, "checks need unique ids, valid commands/path patterns, known prerequisites, and no dependency cycles")
		return
	}
	f.Checks = checks
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Scope < workflows[j].Scope })
	f.Workflows = workflows
	for _, rule := range generated {
		f.AddGenerated(rule)
	}
}

func workflowMapping(node *yaml.Node, allowed ...string) (map[string]*yaml.Node, bool) {
	out := map[string]*yaml.Node{}
	if node == nil || node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return out, false
	}
	for i := 0; i < len(node.Content); i += 2 {
		name, ok := workflowString(node.Content[i])
		known := false
		for _, field := range allowed {
			known = known || name == field
		}
		if !ok || !known || out[name] != nil {
			return out, false
		}
		out[name] = node.Content[i+1]
	}
	return out, true
}

func workflowString(node *yaml.Node) (string, bool) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", false
	}
	return node.Value, true
}

func workflowPath(node *yaml.Node, fallback string) (string, bool) {
	if node == nil {
		return fallback, true
	}
	p, ok := workflowString(node)
	if !ok || !model.ValidPath(p) || path.IsAbs(p) || path.Clean(p) != p {
		return "", false
	}
	return p, true
}

func workflowKind(kind string) bool {
	switch kind {
	case model.KindInstall, model.KindBuild, model.KindTest, model.KindTestOne,
		model.KindLint, model.KindFormat, model.KindTypecheck, model.KindCheck,
		model.KindGenerate, model.KindDev, model.KindRun, model.KindConfigure:
		return true
	}
	return false
}
