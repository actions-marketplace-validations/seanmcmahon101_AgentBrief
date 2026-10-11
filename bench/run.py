#!/usr/bin/env python3
"""AgentBrief orientation benchmark runner (stdlib only).

Arms: none | llm | agentbrief.  For each repo x arm x rep, ask a read-only Claude Code
session five orientation questions and save the raw `claude -p --output-format json` result
to results/raw/<repo>.<arm>.<rep>.json. Resumable: existing result files are skipped.
Grading is separate (grade.py), reporting too (report.py).
"""
import argparse, json, os, shutil, subprocess, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from concurrent.futures import ThreadPoolExecutor, as_completed

BENCH = os.path.dirname(os.path.abspath(__file__))
WORK_DEFAULT = os.environ.get("AGENTBRIEF_BENCH_WORK", "/tmp/agentbrief-bench")
CORPUS_DEFAULT = os.environ.get("AGENTBRIEF_CORPUS", os.path.expanduser("~/agentbrief-corpus"))
REPOS = ["excalidraw", "vite", "httpx", "fastapi", "cobra", "ripgrep", "spring-petclinic", "sinatra"]
ARMS = ["none", "llm", "agentbrief"]
DRIFT_REPOS = ["excalidraw", "vite", "fastapi", "ripgrep"]
DRIFT_ARMS = ["none", "llm-stale", "agentbrief"]

# Instruction files other agents/tools might load. All removed in every arm.
STRIP_NAMES = {"AGENTS.md", "CLAUDE.md", "CLAUDE.local.md", "GEMINI.md", ".cursorrules", ".clinerules",
               ".git", ".claude", ".cursor", ".junie", ".windsurfrules"}
STRIP_PATHS = [".github/copilot-instructions.md", ".github/instructions"]

READ_TOOLS = ['Read', 'Glob', 'Grep', 'Bash(ls:*)', 'Bash(find:*)', 'Bash(cat:*)', 'Bash(head:*)', 'Bash(grep:*)']

LLM_PROMPT = ("Analyze this codebase and create an AGENTS.md file at the repository root containing the "
              "commands needed to build, lint and test (including how to run a single test), and the "
              "high-level architecture that requires reading multiple files to understand. Keep it concise. "
              "Do not include generic development advice, and do not list every file or component. "
              "Do not make up information; only include what you verified in the repository.")

QUESTION_PROMPT = ("You are about to start working in this repository. Answer the following questions precisely, "
                   "giving exact shell commands to run from the repository root. Do not run any commands other "
                   "than reading files. Reply ONLY with a JSON object {{\"q1\": \"...\", \"q2\": \"...\", "
                   "\"q3\": \"...\", \"q4\": \"...\", \"q5\": \"...\"}} (string values).\n\n{qs}")


def log(*a):
    print(time.strftime("%H:%M:%S"), *a, file=sys.stderr, flush=True)


def strip_copy(src, dst):
    """Fresh copy of src at dst with .git and all agent-instruction files removed."""
    if os.path.exists(dst):
        shutil.rmtree(dst)
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copytree(src, dst, symlinks=True, ignore=shutil.ignore_patterns(".git"))
    for root, dirs, files in os.walk(dst):
        for n in list(dirs) + files:
            low = n.lower()
            if n in STRIP_NAMES or low.startswith(".windsurf"):
                p = os.path.join(root, n)
                shutil.rmtree(p) if os.path.isdir(p) and not os.path.islink(p) else os.remove(p)
                if n in dirs:
                    dirs.remove(n)
    for p in STRIP_PATHS:
        p = os.path.join(dst, p)
        if os.path.isdir(p):
            shutil.rmtree(p)
        elif os.path.exists(p):
            os.remove(p)
    # Safety check: nothing instruction-like left.
    for root, dirs, files in os.walk(dst):
        for n in files + dirs:
            assert n not in ("AGENTS.md", "CLAUDE.md", "GEMINI.md", ".cursorrules"), os.path.join(root, n)


def claude(cwd, prompt, model, extra, timeout=900):
    cmd = ["claude", "-p", "--model", model, "--output-format", "json", "--setting-sources", "user",
           "--strict-mcp-config", "--no-session-persistence"] + extra
    t = time.time()
    p = subprocess.run(cmd, cwd=cwd, input=prompt, capture_output=True, text=True, timeout=timeout)
    try:
        out = json.loads(p.stdout)
    except Exception:
        out = {"is_error": True, "result": p.stdout[-2000:], "stderr": p.stderr[-2000:]}
    out["_wall_s"] = round(time.time() - t, 1)
    out["_returncode"] = p.returncode
    return out


def write_instruction_files(dst, agents_md):
    with open(os.path.join(dst, "AGENTS.md"), "w") as f:
        f.write(agents_md)
    with open(os.path.join(dst, "CLAUDE.md"), "w") as f:
        f.write("@AGENTS.md\n")


def generate_llm(repo, a):
    """One-off per repo: have Claude Code write AGENTS.md. Cached in generated/."""
    md = os.path.join(BENCH, "generated", f"{repo}.AGENTS.md")
    meta = os.path.join(BENCH, "generated", f"{repo}.gen.json")
    if os.path.exists(md):
        return open(md).read()
    log("generating LLM AGENTS.md for", repo)
    d = os.path.join(a.work, "gen", repo)  # always the unmutated repo
    strip_copy(os.path.join(a.corpus, repo), d)
    out = claude(d, LLM_PROMPT, "sonnet", ["--allowedTools", "Read,Glob,Grep,Write,Bash(ls:*),Bash(find:*),Bash(cat:*)",
                                          "--permission-mode", "acceptEdits", "--max-turns", "40"], timeout=1200)
    path = os.path.join(d, "AGENTS.md")
    if not os.path.exists(path):
        raise SystemExit(f"llm generation for {repo} produced no AGENTS.md: {str(out)[:500]}")
    text = open(path).read()
    open(md, "w").write(text)
    json.dump(out, open(meta, "w"), indent=2)
    return text


def results_dir(tag):
    return os.path.join(BENCH, "results", tag)


def generate_agentbrief(repo, a, dst):
    """Run `agentbrief sync` on the stripped copy and return the AGENTS.md it wrote."""
    p = subprocess.run([a.agentbrief, "sync", "-C", dst, "--no-imports"], capture_output=True, text=True, timeout=300)
    if p.returncode != 0 or not os.path.exists(os.path.join(dst, "AGENTS.md")):
        raise SystemExit(f"agentbrief sync failed on {repo}: {p.stdout[-500:]} {p.stderr[-500:]}")
    text = open(os.path.join(dst, "AGENTS.md")).read()
    keep = os.path.join(results_dir(a.tag), "agentbrief-md")
    os.makedirs(keep, exist_ok=True)
    open(os.path.join(keep, f"{repo}.AGENTS.md"), "w").write(text)
    return text


def setup_arm(repo, arm, a):
    dst = os.path.join(a.work, a.tag, arm, repo)
    strip_copy(os.path.join(a.corpus, repo), dst)
    if a.drift:  # repo changes AFTER the (cached, unmutated-repo) llm AGENTS.md was written
        import drift
        drift.MUTATIONS[repo](dst)
    if arm in ("llm", "llm-stale"):
        write_instruction_files(dst, generate_llm(repo, a))
    elif arm == "agentbrief":
        text = generate_agentbrief(repo, a, dst)
        write_instruction_files(dst, text)
    return dst


def task_path(repo, drift):
    return os.path.join(BENCH, "tasks", "drift" if drift else "", f"{repo}.json")


def build_prompt(repo, drift):
    task = json.load(open(task_path(repo, drift)))
    qs = "\n".join(f"{q['id']}. {q['question']}" for q in task["questions"])
    return QUESTION_PROMPT.format(qs=qs)


def one_run(repo, arm, rep, dst, a):
    out_path = os.path.join(results_dir(a.tag), "raw", f"{repo}.{arm}.{rep}.json")
    out = claude(dst, build_prompt(repo, a.drift), a.model,
                 ["--max-turns", str(a.max_turns), "--allowedTools", ",".join(READ_TOOLS)])
    out["_meta"] = {"repo": repo, "arm": arm, "rep": rep, "model": a.model, "tag": a.tag, "drift": a.drift}
    json.dump(out, open(out_path, "w"), indent=2)
    return out_path, out


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repos", default=None)
    ap.add_argument("--tag", default="sonnet", help="results go to results/<tag>/ (default: sonnet)")
    ap.add_argument("--drift", action="store_true",
                    help="drift experiment: mutate repos after llm generation; uses tasks/drift/, arms llm-stale")
    ap.add_argument("--arms", default=None)
    ap.add_argument("--reps", type=int, default=2)
    ap.add_argument("--jobs", type=int, default=3)
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--max-turns", type=int, default=30)
    ap.add_argument("--agentbrief", help="path to the agentbrief binary (needed for the agentbrief arm)")
    ap.add_argument("--corpus", default=CORPUS_DEFAULT)
    ap.add_argument("--work", default=WORK_DEFAULT)
    ap.add_argument("--setup-only", action="store_true", help="prepare arm directories, make no agent runs")
    a = ap.parse_args()
    assert not a.drift or a.tag != "sonnet", "use a separate --tag for the drift experiment"
    assert a.work.startswith("/tmp/"), "work dir must be under /tmp (ancestor CLAUDE.md files leak otherwise)"
    a.repos = a.repos or ",".join(DRIFT_REPOS if a.drift else REPOS)
    a.arms = a.arms or ",".join(DRIFT_ARMS if a.drift else ARMS)
    for d in ("generated", f"results/{a.tag}/raw", f"results/{a.tag}/grades"):
        os.makedirs(os.path.join(BENCH, d), exist_ok=True)
    repos, arms = a.repos.split(","), a.arms.split(",")
    if "agentbrief" in arms and not a.agentbrief:
        ap.error("--agentbrief PATH is required for the agentbrief arm")

    jobs = []
    for repo in repos:
        for arm in arms:
            todo = [r for r in range(1, a.reps + 1)
                    if not os.path.exists(os.path.join(results_dir(a.tag), "raw", f"{repo}.{arm}.{r}.json"))]
            if not todo and not a.setup_only:
                continue
            dst = setup_arm(repo, arm, a)  # sequential: LLM generation + copies happen up front
            log("ready", repo, arm, "todo reps", todo)
            jobs += [(repo, arm, r, dst) for r in todo]
    if a.setup_only:
        return
    log(f"{len(jobs)} runs, {a.jobs} parallel")
    done = 0
    with ThreadPoolExecutor(a.jobs) as ex:
        futs = {ex.submit(one_run, repo, arm, r, dst, a): (repo, arm, r) for repo, arm, r, dst in jobs}
        for f in as_completed(futs):
            done += 1
            try:
                p, out = f.result()
                log(f"[{done}/{len(jobs)}] {futs[f]} turns={out.get('num_turns')} "
                    f"cost={out.get('total_cost_usd')} err={out.get('is_error')}")
            except Exception as e:
                log(f"[{done}/{len(jobs)}] {futs[f]} FAILED: {e!r}")


if __name__ == "__main__":
    main()
