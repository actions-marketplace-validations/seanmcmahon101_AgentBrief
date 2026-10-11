package detect

import (
	"path"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("ci", detectCI) }

const maxCISteps = 300

// ciSkipWords mark steps that ship something. Recording them would suggest
// agents run release commands, so the whole step is dropped.
var ciSkipWords = []string{"publish", "docker push", "deploy", "gh release", "twine upload", "cargo publish", " login"}

func ciIsRelease(run string) bool {
	l := strings.ToLower(run)
	for _, w := range ciSkipWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

type ciCollector struct {
	f      *Facts
	seenTC map[string]bool
}

func detectCI(r *scan.Repo, f *Facts) {
	c := &ciCollector{f: f, seenTC: map[string]bool{}}
	for _, p := range r.Glob(".github/workflows/*.yml") {
		c.githubWorkflow(r, p)
	}
	for _, p := range r.Glob(".github/workflows/*.yaml") {
		c.githubWorkflow(r, p)
	}
	if r.Has(".gitlab-ci.yml") {
		c.gitlab(r, ".gitlab-ci.yml")
	}
}

func (c *ciCollector) full() bool { return len(c.f.CI) >= maxCISteps }

// ciNoise are shell builtins that never matter to an agent; recording them
// would only eat the step budget.
var ciNoise = setOf("echo", "exit", "printf", "export", "ls", "pwd", "true", "false", "test", "[", "[[", "cat", "mkdir", "sleep")

func (c *ciCollector) add(system, job, run, dir string, src model.Source) {
	if c.full() || run == "" || ciNoise[strings.Fields(run)[0]] {
		return
	}
	c.f.CI = append(c.f.CI, model.CIStep{System: system, Job: job, Run: redact.String(run), Dir: dir, Source: src})
}

// yamlRoot parses text and returns the document's top-level mapping.
func yamlRoot(text string) *yaml.Node {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(text), &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	n := doc.Content[0]
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

// ymap returns the value node for key in a mapping node, or nil.
func ymap(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func yscalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// ciDir normalises a working-directory; "" means the repo root.
func ciDir(d string) string {
	if d == "" || strings.Contains(d, "${{") {
		return ""
	}
	d = path.Clean(strings.TrimPrefix(d, "./"))
	if d == "." || strings.HasPrefix(d, "/") || strings.HasPrefix(d, "..") {
		return ""
	}
	return d
}

// envMap reads a literal `env:` mapping; values holding expressions are
// dropped because we cannot resolve them.
func envMap(n *yaml.Node) map[string]string {
	out := map[string]string{}
	if n == nil || n.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if v := n.Content[i+1]; v.Kind == yaml.ScalarNode && !strings.Contains(v.Value, "${{") {
			out[n.Content[i].Value] = v.Value
		}
	}
	return out
}

var envExpr = regexp.MustCompile(`\$\{\{\s*env\.(\w+)\s*\}\}`)

// substEnv replaces ${{ env.X }} where X is a literal env value defined at
// workflow, job or step level (innermost wins). Other expressions stay.
func substEnv(run string, envs ...map[string]string) string {
	return envExpr.ReplaceAllStringFunc(run, func(m string) string {
		name := envExpr.FindStringSubmatch(m)[1]
		for i := len(envs) - 1; i >= 0; i-- {
			if v, ok := envs[i][name]; ok {
				return v
			}
		}
		return m
	})
}

func (c *ciCollector) githubWorkflow(r *scan.Repo, file string) {
	root := yamlRoot(r.ReadString(file))
	jobs := ymap(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return
	}
	wfEnv := envMap(ymap(root, "env"))
	wfDir := yscalar(ymap(ymap(ymap(root, "defaults"), "run"), "working-directory"))
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		id, job := jobs.Content[i].Value, jobs.Content[i+1]
		jobDir := yscalar(ymap(ymap(ymap(job, "defaults"), "run"), "working-directory"))
		if jobDir == "" {
			jobDir = wfDir
		}
		jobEnv := envMap(ymap(job, "env"))
		steps := ymap(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			c.githubStep(file, id, jobDir, step, wfEnv, jobEnv)
		}
	}
}

func (c *ciCollector) githubStep(file, job, jobDir string, step *yaml.Node, wfEnv, jobEnv map[string]string) {
	if uses := yscalar(ymap(step, "uses")); uses != "" {
		c.setupAction(file, uses, ymap(step, "with"))
		return
	}
	runNode := ymap(step, "run")
	run := yscalar(runNode)
	if run == "" {
		return
	}
	switch strings.ToLower(yscalar(ymap(step, "shell"))) {
	case "pwsh", "powershell", "cmd":
		return
	}
	if ciIsRelease(run) {
		return
	}
	dir := yscalar(ymap(step, "working-directory"))
	if dir == "" {
		dir = jobDir
	}
	dir = ciDir(dir)
	first := runNode.Line
	if runNode.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		first++ // content starts after the "|" line
	}
	run = substEnv(run, wfEnv, jobEnv, envMap(ymap(step, "env")))
	for _, cmd := range ciSplitCommands(run, first) {
		c.add("github-actions", job, cmd.text, dir, model.Source{Path: file, Line: cmd.line})
	}
}

// setupActions maps a setup action to the toolchain and the "with" key that
// holds its version.
var setupActions = map[string]struct{ tool, key string }{
	"actions/setup-node":   {"node", "node-version"},
	"actions/setup-python": {"python", "python-version"},
	"actions/setup-go":     {"go", "go-version"},
	"actions/setup-java":   {"java", "java-version"},
	"pnpm/action-setup":    {"pnpm", "version"},
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// setupAction records the version a setup step pins, when it is a literal.
func (c *ciCollector) setupAction(file, uses string, with *yaml.Node) {
	action, ref, _ := strings.Cut(uses, "@")
	var tool, version string
	var line int
	if a, ok := setupActions[action]; ok {
		tool = a.tool
		if v := ymap(with, a.key); v != nil {
			version, line = yscalar(v), v.Line
		}
	} else if action == "dtolnay/rust-toolchain" {
		tool = "rust"
		if v := ymap(with, "toolchain"); v != nil {
			version, line = yscalar(v), v.Line
		} else if !shaRe.MatchString(ref) {
			version = ref // the ref IS the channel: dtolnay/rust-toolchain@stable
		}
	}
	version = strings.TrimSpace(version)
	if tool == "" || version == "" || strings.ContainsAny(version, "${\n,") || len(version) > 40 {
		return
	}
	key := tool + "@" + version
	if c.seenTC[key] {
		return
	}
	c.seenTC[key] = true
	c.f.AddToolchain(model.Toolchain{Name: tool, Version: version, Source: model.Source{Path: file, Line: line}})
}

// ---- GitLab CI ----

var gitlabReserved = setOf("stages", "variables", "default", "include", "workflow", "image", "services", "cache",
	"before_script", "after_script", "pages")

func (c *ciCollector) gitlab(r *scan.Repo, file string) {
	root := yamlRoot(r.ReadString(file))
	if root == nil {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		name, job := root.Content[i].Value, root.Content[i+1]
		if strings.HasPrefix(name, ".") || gitlabReserved[name] || job.Kind != yaml.MappingNode {
			continue
		}
		if stage := strings.ToLower(yscalar(ymap(job, "stage"))); strings.Contains(stage, "deploy") || strings.Contains(stage, "release") || strings.Contains(stage, "publish") {
			continue
		}
		for _, key := range []string{"before_script", "script"} {
			c.gitlabScript(file, name, ymap(job, key))
		}
	}
}

// gitlabScript handles a script list whose items are strings or nested lists.
func (c *ciCollector) gitlabScript(file, job string, n *yaml.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			c.gitlabScript(file, job, item)
		}
	case yaml.ScalarNode:
		if ciIsRelease(n.Value) {
			return
		}
		first := n.Line
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			first++
		}
		for _, cmd := range ciSplitCommands(n.Value, first) {
			c.add("gitlab-ci", job, cmd.text, "", model.Source{Path: file, Line: cmd.line})
		}
	}
}

// ---- splitting shell blocks into logical commands ----

type ciCmd struct {
	text string
	line int
}

var shellControl = regexp.MustCompile(`^(?:fi|done|then|else|esac|do|\{|\}|;;)$|^(?:if|elif|for|while|until|case)\b|^set\s+[-+]|^function\s|^\w+\(\)\s*\{`)

// ciSplitCommands turns a multi-line run block into logical commands: it joins
// backslash continuations, trailing &&, ||, | and unterminated quotes, and
// drops comments, blank lines, heredoc bodies, set -e style lines and shell
// control keywords. first is the file line of the block's first line.
func ciSplitCommands(block string, first int) []ciCmd {
	lines := strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n")
	var out []ciCmd
	for i := 0; i < len(lines); i++ {
		start := i
		cur := strings.TrimSpace(lines[i])
		if cur == "" || strings.HasPrefix(cur, "#") {
			continue
		}
		for i+1 < len(lines) && ciContinues(cur) {
			i++
			next := strings.TrimSpace(lines[i])
			cur = strings.TrimSuffix(cur, "\\")
			cur = strings.TrimSpace(cur) + " " + next
			if i-start > 30 {
				break
			}
		}
		if marker := heredocMarker(cur); marker != "" {
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != marker {
				i++
			}
			i++ // the terminator itself
		}
		if shellControl.MatchString(cur) {
			continue
		}
		out = append(out, ciCmd{text: strings.Join(strings.Fields(cur), " "), line: first + start})
	}
	return out
}

var heredocRe = regexp.MustCompile(`<<-?\s*["']?(\w+)["']?`)

func heredocMarker(s string) string {
	if m := heredocRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// ciContinues reports whether a command line is not finished.
func ciContinues(s string) bool {
	if strings.HasSuffix(s, "\\") || strings.HasSuffix(s, "&&") || strings.HasSuffix(s, "||") || strings.HasSuffix(s, "|") {
		return true
	}
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if !inD {
				inS = !inS
			}
		case '"':
			if !inS {
				inD = !inD
			}
		}
	}
	return inS || inD
}
