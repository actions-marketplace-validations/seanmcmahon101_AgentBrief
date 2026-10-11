#!/usr/bin/env python3
"""LLM-judge grading of results/raw/*.json -> results/grades/*.json (cached; blind to arm).

The judge sees only: question, ground truth (truth/accept/reject/evidence) and the answer.
It never sees the arm, the instruction file, or the transcript. One judge call per run,
each of the 5 questions graded independently.
"""
import argparse, glob, json, os, re, subprocess, sys
from concurrent.futures import ThreadPoolExecutor

BENCH = os.path.dirname(os.path.abspath(__file__))
JUDGE_CWD = "/tmp/claude-1000/-home-sm/dc3a09a6-ed56-427c-a632-10969d6c9cdc/scratchpad/bench-work/judge"

RUBRIC = """You are a strict, impartial grader for a benchmark about repository orientation. You will be given \
questions about a software repository, the verified ground truth for each (derived from the repo's manifests, \
lockfiles, task runners and CI), and a candidate's answers. Grade each answer independently.

Scoring per question:
- 1   : the answer gives a command/fact that matches an ACCEPT criterion, and contains nothing that contradicts \
the truth or matches a REJECT item (harmless extra detail is fine).
- 0.5 : partially right: correct tool but incomplete (e.g. only half of what CI runs), or a command that would work \
but is explicitly marked 0.5 in REJECT/ACCEPT, or right answer buried among hedged alternatives where a wrong \
alternative is equally prominent.
- 0   : wrong, missing, matches a REJECT item scored 0, guessed, says it cannot determine, or only a generic answer.

Rules: judge against the ground truth only, not your own beliefs. Do not reward verbosity. Commands must be \
exact enough to run from the stated place (a wrong subcommand, wrong package manager, or wrong path is wrong). \
Reply with ONLY a JSON object: {"q1": {"score": 0|0.5|1, "reason": "<one sentence>"}, ..., "q5": {...}}."""


def extract_answers(result_text):
    if not result_text:
        return {}
    t = result_text.strip()
    t = re.sub(r"^```(?:json)?\s*|\s*```$", "", t)
    for cand in (t, t[t.find("{"): t.rfind("}") + 1]):
        try:
            d = json.loads(cand)
            if isinstance(d, dict):
                return d
        except Exception:
            pass
    return {}


def stringify(v):
    return v if isinstance(v, str) else json.dumps(v)


def judge_prompt(task, answers):
    blocks = []
    for q in task["questions"]:
        a = answers.get(q["id"])
        blocks.append(f"### {q['id']}\nQUESTION: {q['question']}\nTRUTH: {q['truth']}\n"
                      f"ACCEPT: {json.dumps(q['accept'])}\nREJECT: {json.dumps(q['reject'])}\n"
                      f"EVIDENCE FILES: {', '.join(q['evidence'])}\n"
                      f"CANDIDATE ANSWER: {stringify(a) if a is not None else '(no answer given)'}")
    return RUBRIC + f"\n\nRepository: {task['repo']} ({task['ecosystem']})\n\n" + "\n\n".join(blocks)


def grade_one(path, model, force, tag):
    name = os.path.basename(path)
    out_path = os.path.join(BENCH, "results", tag, "grades", name)
    if os.path.exists(out_path) and not force:
        return name, "cached"
    raw = json.load(open(path))
    sub = "drift" if raw["_meta"].get("drift") else ""
    task = json.load(open(os.path.join(BENCH, "tasks", sub, raw["_meta"]["repo"] + ".json")))
    answers = extract_answers(raw.get("result", ""))
    grades = {}
    if answers or not raw.get("is_error"):
        os.makedirs(JUDGE_CWD, exist_ok=True)
        p = subprocess.run(["claude", "-p", "--model", model, "--output-format", "json", "--tools", "",
                            "--setting-sources", "user", "--strict-mcp-config", "--no-session-persistence",
                            "--max-turns", "3"],
                           cwd=JUDGE_CWD, input=judge_prompt(task, answers), capture_output=True, text=True, timeout=300)
        try:
            res = json.loads(p.stdout)
            grades = extract_answers(res.get("result", ""))
        except Exception:
            grades = {}
        if not grades:
            return name, "JUDGE FAILED (not cached): " + p.stdout[-200:]
    else:
        grades = {q["id"]: {"score": 0, "reason": "run errored / no parseable answer"} for q in task["questions"]}
    for q in task["questions"]:  # unparsed or absent => 0, recorded
        g = grades.get(q["id"])
        if not isinstance(g, dict) or g.get("score") not in (0, 0.5, 1):
            grades[q["id"]] = {"score": 0, "reason": "no gradable answer"}
    json.dump({"meta": raw["_meta"], "answers": answers, "grades": grades}, open(out_path, "w"), indent=2)
    return name, "graded"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag", default="sonnet")
    ap.add_argument("--jobs", type=int, default=4)
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--force", action="store_true", help="regrade even if cached")
    a = ap.parse_args()
    os.makedirs(os.path.join(BENCH, "results", a.tag, "grades"), exist_ok=True)
    files = sorted(glob.glob(os.path.join(BENCH, "results", a.tag, "raw", "*.json")))
    with ThreadPoolExecutor(a.jobs) as ex:
        for name, status in ex.map(lambda f: grade_one(f, a.model, a.force, a.tag), files):
            print(name, status, file=sys.stderr)


if __name__ == "__main__":
    main()
