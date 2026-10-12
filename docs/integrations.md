# Integrations

AgentBrief writes one canonical `AGENTS.md` and keeps it true. This page covers how each tool picks that file up. Tool behaviour changes quickly; check the tool's own documentation if something here no longer matches.

## Claude Code

Claude Code reads `CLAUDE.md`, not `AGENTS.md`, and ignores `AGENTS.md` when a `CLAUDE.md` exists that does not import it. The fix is an import line:

```markdown
@AGENTS.md
```

`agentbrief sync` adds this line to an existing root `CLAUDE.md` (use `--no-imports` to skip), and `agentbrief check` warns with `agents-md-not-loaded` if it is missing. If you have no `CLAUDE.md`, sync does not create one; create a file containing only `@AGENTS.md` if you want Claude Code to load the facts. Keep Claude-specific notes below the import. `check` also warns when `CLAUDE.md` exceeds 200 lines, which is Anthropic's guidance.

Agents can also query on demand with `agentbrief brief <dir> --json` (see the README). A Claude Code hook that runs `agentbrief check` at session start is possible, but the hook configuration format is not described here because it has not been verified against the current Claude Code documentation.

## Codex

Codex reads `AGENTS.md` natively. Its default cap on combined instruction files is 32 KiB, which the `oversized` rule warns about; the AgentBrief block is far below that.

## Cursor, GitHub Copilot, Windsurf, Aider and others

Most other tools read `AGENTS.md` directly or support it. `agentbrief check` also lints their own files (`.cursorrules`, `.cursor/rules/*`, `.windsurfrules`, `.windsurf/rules/*`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md`, `CONVENTIONS.md`), so commands and paths mentioned there stay valid. `agentbrief` lists any such files it finds under "Other agent instructions" in the block.

## Gemini CLI

Gemini CLI reads `GEMINI.md` by default. Either set `context.fileName` in its settings to include `AGENTS.md`, or put `@AGENTS.md` in `GEMINI.md`. `agentbrief sync` adds the import to an existing root `GEMINI.md`, and `check` warns if it is missing.

## GitHub Actions

```yaml
- uses: actions/checkout@v4
- uses: seanmcmahon101/AgentBrief@v0.1.0
  # with:
  #   args: check --strict        # fail on warnings too
  #   working-directory: path/to/repo
```

The optional `version` input accepts a release tag, `main`, `latest`, or a full
commit SHA. Use a commit pin for exact revision testing. Unsupported refs fail;
`main` bypasses mutable Go module-proxy caching.

Without the action:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: stable
- run: go install github.com/seanmcmahon101/AgentBrief/cmd/agentbrief@v0.1.0
- run: agentbrief check
```

Use `agentbrief check --json` if you want to post findings elsewhere; the schema is `agentbrief/check/v1`.

## pre-commit

```yaml
repos:
  - repo: https://github.com/seanmcmahon101/AgentBrief
    rev: v0.1.0
    hooks:
      - id: agentbrief-check
      - id: agentbrief-sync   # optional
```

pre-commit builds the hook with Go, so Go must be installed. With a prebuilt binary on `PATH` instead, use a `repo: local` hook with `entry: agentbrief check`, `language: system`, `pass_filenames: false`.

## Monorepos

`sync` writes one block at the root, summarising the workspace (package manager, root commands, a list of packages). Per-package commands are not written into nested files; ask for them when needed:

```console
$ agentbrief brief packages/vite
```

This prints the package's own commands first, then root commands that still apply. Use `--json` for tools.
