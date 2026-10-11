# Security policy

## Threat model

AgentBrief is run on repositories you may not trust (a freshly cloned dependency, a pull request from a fork), often in CI. Its output is fed to coding agents, so two things matter: the tool must not be a way to run attacker code, and its output must not be a way to inject instructions or leak secrets into an agent's context.

What AgentBrief does about that:

- **No execution.** It only reads and parses files. It never runs repository scripts, build tools or `git` (git configuration can execute commands through settings such as `core.fsmonitor`).
- **Sensitive files are not read.** `.env*`, private keys, certificates, keystores and similar files are refused at the file-reading layer.
- **Bounded reads.** Symlinks are not followed and reads are capped at 1 MiB.
- **Redaction and caps.** Every repository-derived string that reaches output is length-capped and passed through secret redaction.
- **No prose in the block.** README text, task descriptions and other free-form content are not copied into generated instruction blocks, only identifiers, paths, versions and validated command lines. This limits prompt injection through a malicious repository.
- **Narrow writes.** `sync` writes only `AGENTS.md`, `CLAUDE.md` and `GEMINI.md` at the repository root, and changes only the text between its markers plus one import line.

Out of scope: a command AgentBrief reports is taken from the repository's own scripts, so running it is as trustworthy as running the repository's scripts. AgentBrief does not sandbox your agent.

Things we consider vulnerabilities: code execution while scanning, reading files the sensitive-file list should exclude, writes outside the files above, secrets surviving redaction, and repository content that gets copied into generated output as free text.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting: open the repository's **Security** tab and choose **Report a vulnerability**. Do not file a public issue. Include the AgentBrief version (`agentbrief version`), a minimal repository layout that reproduces the problem, and the impact you see.

This is a small project maintained by one person. Expect an acknowledgement within a few days; we will agree a disclosure timeline with you once the issue is confirmed. Only the latest release is supported.
