# Maintainer-selected workflows

AgentBrief discovers commands from manifests and CI. Discovery cannot establish
which wrapper your team wants an agent to use. An optional root
`.agentbrief.yaml` records those choices once, supplies them to
`brief` and `sync`, and lets `check` catch stale references.

No configuration is required. Repositories without this file keep their existing
command selection and output.

## Select the intended commands

For a repository with cached builds, a test wrapper, and a separate website:

```yaml
version: 1
workflows:
  - scope: .
    commands:
      - kind: build
        run: npm run build:cached
      - kind: test-single
        run: npm run test:focused -- <file>
  - scope: website
    commands:
      - kind: check
        run: python3 scripts/website.py check
        dir: .
generated:
  - skills/**
  - docs/public/product-catalog.json
```

The example assumes these scripts and the `website/` directory exist.
Commands are exact single lines; AgentBrief never executes them or expands
placeholders. Replace `<file>` when running the selected test command yourself.

```sh
agentbrief brief --json
agentbrief brief website/index.html --json
agentbrief check
agentbrief sync --dry-run
```

The first brief selects the root build and single-test wrappers. The second also
selects the website check, run from the root. The website does not need a package
manifest for its workflow to apply. The root brief does not include the website
check.

## Scope, working directory, and precedence

- `scope` is a repository-relative directory. Its workflow applies to that
  directory and its descendants; it defaults to `.`.
- Each command's `dir` is its working directory, relative to the repository
  root. It also defaults to `.`, even for a nested scope.
- Scopes and working directories must contain scanned files. Absolute paths,
  parent traversal, and glob scopes are rejected. Ignored or empty directories
  cannot establish a workflow scope or working directory.
- Matching workflows are applied from the root toward the queried path. A more
  specific workflow overrides the same command kind from an ancestor.
- A root workflow preserves a nested package's own detected command. To override
  it, declare a workflow at that package or inside it. Ancestor workflows can
  still supply missing command kinds and replace inherited root commands.
- Unselected kinds retain their detected commands. Nothing rewrites a discovered
  command's flags or arguments.
- `generated` is a repository-wide list of relative exact paths or globs.
  These paths need not exist: generated outputs are often absent in a clean
  checkout. They join the existing do-not-edit facts and their display budget.

Supported kinds: `install`, `configure`, `build`, `test`, `test-single`,
`lint`, `format`, `typecheck`, `check`, `generate`, `dev`, and `run`.
Each scope and each command kind within that scope must be unique. Unknown
fields, aliases, multiple YAML documents, multiline commands, and commands over
160 bytes are rejected. The configuration uses the normal 1 MiB file-read cap.

## Evidence and drift checking

Selected commands carry `evidence: ["maintainer"]`, `preferred: true`, and a
`declaration` with their source file and line in JSON. They carry
`verified: false`: a maintainer preference does not establish successful
execution or CI coverage. Markdown explicitly labels execution as unverified.
The full `brief --all --json` model keeps workflow choices separate from the
discovered command inventory.

`check` validates configured commands even when no instruction file or managed
block exists. It reuses the script, target, command-path, and package-manager
checks. npm script checks use the command's working directory (or explicit
package selection), so a script in another package does not validate the wrong
package's command. Make checks likewise honor the selected working directory.

A renamed wrapper therefore produces a finding at its configuration line.
Changing the configured root command after syncing produces `stale-block`.
Malformed configuration produces an `invalid-config` error. `brief` and
`sync` refuse malformed policy or commands the checker already knows are
invalid; sync leaves existing instruction files untouched.

These are static checks. Unknown tools, dynamic targets, configuration-dependent
builds, missing host prerequisites, and broken script bodies can remain
unresolved. Workflow declarations do not establish that a test covers a change
or that the current machine can run it. Keep those prerequisites in your
maintainer-written instructions.
