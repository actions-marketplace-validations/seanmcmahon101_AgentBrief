// Package model defines the repository facts AgentBrief extracts.
//
// Everything in a Repo is derived statically from files in the repository.
// AgentBrief never executes repository code, so a Command being present means
// it is *declared* somewhere (a manifest, task file or CI workflow), not that it
// has been run successfully. Evidence records where each fact came from so
// consumers can judge how much to trust it.
package model

// SchemaVersion identifies the JSON output format. Bump it on breaking changes.
const SchemaVersion = "agentbrief/v1"

// Source points at the file (and optionally line) a fact was read from.
// Paths are slash-separated and relative to the repository root.
type Source struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// Evidence levels, strongest first.
const (
	// EvidenceCI: the command (or one equivalent to it) is run by a CI workflow.
	EvidenceCI = "ci"
	// EvidenceDeclared: the command is defined in a manifest or task file.
	EvidenceDeclared = "declared"
	// EvidenceConvention: the ecosystem's standard command; not written down in
	// the repo but implied by its manifest (e.g. `cargo test` for a Cargo.toml).
	EvidenceConvention = "convention"
)

// Command kinds. Detectors should use these; render code relies on them.
const (
	KindInstall   = "install"
	KindBuild     = "build"
	KindTest      = "test"
	KindTestOne   = "test-single" // how to run one test file / case
	KindLint      = "lint"
	KindFormat    = "format"
	KindTypecheck = "typecheck"
	KindDev       = "dev" // start a dev server / watch mode
	KindRun       = "run"
	KindGenerate  = "generate" // codegen
	KindCheck     = "check"    // aggregate "run everything CI runs" command
)

// Repo is the complete set of facts for one repository.
type Repo struct {
	Schema string `json:"schema"`
	// Name is the repository directory name, or a project name from the root
	// manifest when one exists.
	Name string `json:"name"`

	Languages       []Language        `json:"languages"`
	Toolchains      []Toolchain       `json:"toolchains"`
	PackageManagers []PackageManager  `json:"package_managers"`
	Projects        []Project         `json:"projects"`
	Commands        []Command         `json:"commands"`
	Tasks           []Task            `json:"tasks"`
	Generated       []PathRule        `json:"generated"`
	CI              []CIStep          `json:"ci"`
	Instructions    []InstructionFile `json:"instruction_files"`
	Notes           []Note            `json:"notes"`

	// Stats about the scan itself; useful for debugging and benchmarks.
	Stats ScanStats `json:"stats"`
}

// Language is a programming language detected by file extension.
type Language struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
}

// Toolchain is a pinned or required tool version, e.g. node 20 from .nvmrc.
type Toolchain struct {
	Name    string `json:"name"`    // "node", "go", "python", "rust", "java", ...
	Version string `json:"version"` // as written, e.g. ">=3.10", "1.22.3", "stable"
	Source  Source `json:"source"`
}

// PackageManager is the tool that must be used to install/run things in Dir.
type PackageManager struct {
	Name      string `json:"name"`      // "pnpm", "npm", "yarn", "bun", "uv", "poetry", "cargo", "maven", "gradle", ...
	Ecosystem string `json:"ecosystem"` // see Project.Ecosystem
	Version   string `json:"version,omitempty"`
	Dir       string `json:"dir"` // "." for repo root
	Source    Source `json:"source"`
}

// Project is a buildable unit: the root project or a workspace member.
type Project struct {
	Dir       string `json:"dir"`       // "." for repo root
	Name      string `json:"name"`      // from the manifest, else the directory name
	Ecosystem string `json:"ecosystem"` // "node", "python", "go", "rust", "java", "ruby", "php", "dotnet", "elixir", "swift", "c-cpp", "deno", ...
	Manifest  string `json:"manifest"`  // path of the manifest file
	// Workspace is the Dir of the workspace root this project belongs to, if any.
	Workspace string `json:"workspace,omitempty"`
	// IsWorkspaceRoot is true for a monorepo root that declares members.
	IsWorkspaceRoot bool `json:"is_workspace_root,omitempty"`
}

// Command is a curated, ready-to-run command for a common development action.
type Command struct {
	Kind     string   `json:"kind"`           // one of the Kind* constants
	Run      string   `json:"run"`            // exact command line to run from Dir
	Dir      string   `json:"dir"`            // working directory, "." for root
	Evidence []string `json:"evidence"`       // Evidence* constants, strongest first
	Source   Source   `json:"source"`         // where it was declared
	Note     string   `json:"note,omitempty"` // short qualifier, e.g. "append a file path"
}

// Task is a raw entry from a task inventory: a package.json script, Makefile
// target, just recipe, etc. Tasks are used to validate commands that appear in
// instruction files, and to derive Commands.
type Task struct {
	Runner string `json:"runner"` // "npm-script", "make", "just", "task", "mise", "composer", "rake", "poe", "cargo-alias", ...
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Body   string `json:"body,omitempty"` // the command it runs, when short and known
	Desc   string `json:"desc,omitempty"` // human description, e.g. Makefile "## help" comment
	Source Source `json:"source"`
}

// PathRule marks files agents should not hand-edit.
type PathRule struct {
	Pattern string `json:"pattern"` // gitignore-style pattern or exact path
	Reason  string `json:"reason"`  // "generated", "vendored", "lockfile", "build-output"
	Source  Source `json:"source"`  // evidence: a header marker, .gitattributes line, generator config, ...
}

// CIStep is a shell command a CI pipeline runs.
type CIStep struct {
	System string `json:"system"` // "github-actions", "gitlab-ci", ...
	Job    string `json:"job"`
	Run    string `json:"run"`
	Dir    string `json:"dir,omitempty"` // working-directory if set
	Source Source `json:"source"`
}

// InstructionFile is an existing agent-instruction file (AGENTS.md, CLAUDE.md,
// Cursor rules, Copilot instructions, ...).
type InstructionFile struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`  // "agents-md", "claude-md", "gemini-md", "cursor-rules", "copilot", "windsurf", "cline", "junie", "aider", "other"
	Scope string `json:"scope"` // directory it applies to, "." for root
	// Imports lists files pulled in by reference (e.g. CLAUDE.md "@AGENTS.md").
	Imports []string `json:"imports,omitempty"`
	// Managed is true if the file contains an AgentBrief managed block.
	Managed bool `json:"managed"`
	Lines   int  `json:"lines"`
}

// Note is a short, evidence-backed fact that does not fit elsewhere, e.g.
// "uses Git LFS" or "Python tests need the `src` layout installed (pip install -e .)".
type Note struct {
	Text   string `json:"text"`
	Source Source `json:"source"`
}

// ScanStats describes the scan.
type ScanStats struct {
	Files      int   `json:"files"`
	Truncated  bool  `json:"truncated"` // file limit reached
	DurationMs int64 `json:"duration_ms"`
}

// Finding is a problem reported by `agentbrief check`.
type Finding struct {
	Rule       string `json:"rule"`     // stable identifier, e.g. "missing-path"
	Severity   string `json:"severity"` // "error" or "warning"
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)
