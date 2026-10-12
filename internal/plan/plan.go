// Package plan selects maintainer-declared validation checks for changed files.
// It does not infer dependency impact or execute any command.
package plan

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

const Schema = "agentbrief/plan/v1"

// Match explains exactly which declaration selected a check.
type Match struct {
	Path    string       `json:"path"`
	Pattern string       `json:"pattern"`
	Source  model.Source `json:"source"`
}

// Step preserves a command and its prerequisites. Shared prerequisites appear
// once, before their dependents. Selection is not evidence of execution.
type Step struct {
	ID         string        `json:"id"`
	Command    model.Command `json:"command"`
	Requires   []string      `json:"requires"`
	SelectedBy []Match       `json:"selected_by"`
	RequiredBy []string      `json:"required_by"`
	Source     model.Source  `json:"source"`
}

type ProtectedPath struct {
	Path string         `json:"path"`
	Rule model.PathRule `json:"rule"`
}

// Plan separates declared path mapping from actual test coverage, which remains
// unknown even when every input file matches a check.
type Plan struct {
	Schema          string          `json:"schema"`
	Paths           []string        `json:"paths"`
	Steps           []Step          `json:"steps"`
	UnmatchedPaths  []string        `json:"unmatched_paths"`
	ProtectedPaths  []ProtectedPath `json:"protected_paths"`
	MappingComplete bool            `json:"mapping_complete"`
	ScanTruncated   bool            `json:"scan_truncated"`
	PolicyChanged   bool            `json:"policy_changed"`
	Coverage        string          `json:"coverage"`
	Executed        bool            `json:"executed"`
}

// Build consumes validated, parsed policy and canonical, root-relative file
// names. Missing files are intentional: deletions and rename sources matter.
func Build(repo *model.Repo, paths []string) (Plan, error) {
	budget := matchBudget{remaining: 64 << 20, output: 16 << 20, depths: map[string]int{}}
	p := Plan{Schema: Schema, Paths: append([]string{}, paths...),
		Steps: []Step{}, UnmatchedPaths: []string{}, ProtectedPaths: []ProtectedPath{},
		ScanTruncated: repo.Stats.Truncated, Coverage: "unknown"}
	sort.Strings(p.Paths)
	p.Paths = unique(p.Paths)
	for _, file := range p.Paths {
		if err := budget.reserve(3*len(file) + 64); err != nil {
			return Plan{}, err
		}
	}
	checks := map[string]model.ValidationCheck{}
	matches := map[string][]Match{}
	mapped := map[string]bool{}
	for _, check := range repo.Checks {
		checks[check.ID] = check
		for _, file := range p.Paths {
			excluded, err := anyMatch(check.Exclude, file, &budget)
			if err != nil {
				return Plan{}, err
			}
			if excluded {
				continue
			}
			for _, selector := range check.Paths {
				matched, err := budget.match(selector.Pattern, file)
				if err != nil {
					return Plan{}, err
				}
				if matched {
					if err := budget.reserve(3*len(file) + len(selector.Pattern) + 512); err != nil {
						return Plan{}, err
					}
					matches[check.ID] = append(matches[check.ID], Match{
						Path: file, Pattern: selector.Pattern, Source: selector.Source})
					mapped[file] = true
					break // one sourced selection explanation per check and input path
				}
			}
		}
	}
	selected := map[string]bool{}
	queue := []string{}
	for _, check := range repo.Checks {
		if len(matches[check.ID]) > 0 {
			selected[check.ID] = true
			queue = append(queue, check.ID)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, id := range checks[queue[i]].Requires {
			if !selected[id] {
				selected[id] = true
				queue = append(queue, id)
			}
		}
	}
	// Topological order with lexical tie-breaking is independent of YAML order.
	indegree := map[string]int{}
	requiredBy := map[string][]string{}
	ready := []string{}
	for _, check := range repo.Checks {
		if !selected[check.ID] {
			continue
		}
		indegree[check.ID] = len(check.Requires)
		for _, id := range check.Requires {
			requiredBy[id] = append(requiredBy[id], check.ID)
		}
		if len(check.Requires) == 0 {
			ready = append(ready, check.ID)
		}
	}
	for len(ready) > 0 {
		sort.Strings(ready)
		id := ready[0]
		ready = ready[1:]
		c := checks[id]
		by := append([]string{}, requiredBy[id]...)
		sort.Strings(by)
		if err := budget.reserve(1024 + 64*(len(c.Requires)+len(by))); err != nil {
			return Plan{}, err
		}
		p.Steps = append(p.Steps, Step{ID: id, Command: c.Command,
			Requires: append([]string{}, c.Requires...), Source: c.Source,
			SelectedBy: append([]Match{}, matches[id]...), RequiredBy: by})
		for _, next := range requiredBy[id] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	for _, file := range p.Paths {
		if !mapped[file] {
			p.UnmatchedPaths = append(p.UnmatchedPaths, file)
		}
		p.PolicyChanged = p.PolicyChanged || file == ".agentbrief.yaml"
		for _, rule := range repo.Generated {
			matched, err := budget.match(rule.Pattern, file)
			if err != nil {
				return Plan{}, err
			}
			if matched {
				if err := budget.reserve(3*len(file) + 512); err != nil {
					return Plan{}, err
				}
				p.ProtectedPaths = append(p.ProtectedPaths, ProtectedPath{Path: file, Rule: rule})
			}
		}
	}
	p.MappingComplete = len(p.UnmatchedPaths) == 0 && !p.ScanTruncated
	return p, nil
}

// Bound both matching work and representation expansion before allocation.
// A refused plan is never returned as a misleadingly complete prefix.
var errPlanBudget = errors.New("validation plan exceeds its matching/output budget; narrow the input or policy")

type matchBudget struct {
	remaining int
	output    int
	depths    map[string]int
}

// Reserve a conservative representation estimate before appending records.
// Filename escaping/fences, pretty JSON, command fields and graph edges are
// accounted for; repeated long names cannot multiply a small input unboundedly.
func (b *matchBudget) reserve(n int) error {
	if n > b.output {
		return errPlanBudget
	}
	b.output -= n
	return nil
}

func (b *matchBudget) match(pattern, file string) (bool, error) {
	depth := func(s string) int {
		n, ok := b.depths[s]
		if !ok {
			n = strings.Count(s, "/") + 1
			b.depths[s] = n
		}
		return n
	}
	work := depth(pattern)*depth(file) + len(pattern) + len(file)
	if work > b.remaining {
		return false, errPlanBudget
	}
	b.remaining -= work
	return scan.MatchPath(pattern, file), nil
}

func anyMatch(selectors []model.PathSelector, file string, budget *matchBudget) (bool, error) {
	for _, s := range selectors {
		matched, err := budget.match(s.Pattern, file)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
}

func unique(s []string) []string {
	out := s[:0]
	for _, v := range s {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

// Write emits a readable plan. It never emits a shell script: commands keep
// their explicit working directories and prerequisite identities.
func (p Plan) Write(w io.Writer) error {
	var b strings.Builder
	b.WriteString("Validation plan (maintainer-declared; execution and test coverage unverified)\n\n")
	for i, step := range p.Steps {
		fmt.Fprintf(&b, "%d. %s: %s (dir %s; %s:%d)\n",
			i+1, code(step.ID), code(step.Command.Run), code(step.Command.Dir),
			step.Command.Source.Path, step.Command.Source.Line)
		for _, m := range step.SelectedBy {
			fmt.Fprintf(&b, "   Selected by %s matching %s (%s:%d).\n",
				code(m.Path), code(m.Pattern), m.Source.Path, m.Source.Line)
		}
		if len(step.RequiredBy) > 0 {
			fmt.Fprintf(&b, "   Required by: %s.\n", strings.Join(step.RequiredBy, ", "))
		}
	}
	if len(p.Steps) == 0 {
		b.WriteString("No declared checks selected. Inspect repository instructions and CI.\n")
	}
	for _, file := range p.UnmatchedPaths {
		fmt.Fprintf(&b, "No declared check for %s.\n", code(file))
	}
	for _, item := range p.ProtectedPaths {
		fmt.Fprintf(&b, "Review %s: %s matches %s (%s:%d).\n", code(item.Path),
			item.Rule.Reason, code(item.Rule.Pattern), item.Rule.Source.Path, item.Rule.Source.Line)
	}
	if p.ScanTruncated {
		b.WriteString("Repository scan truncated; policy discovery may be incomplete.\n")
	}
	if p.PolicyChanged {
		b.WriteString("Validation policy changed; review its mappings and prerequisites.\n")
	}
	b.WriteString("\nPath mappings do not establish test coverage. AgentBrief ran no commands.\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func code(s string) string {
	longest, current := 0, 0
	for _, c := range s {
		if c == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	return fence + " " + s + " " + fence
}
