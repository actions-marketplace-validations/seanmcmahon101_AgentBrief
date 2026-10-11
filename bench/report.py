#!/usr/bin/env python3
"""Aggregate results/raw + results/grades into results/summary.md (everything is reported)."""
import glob, json, os, statistics as st
from collections import defaultdict

BENCH = os.path.dirname(os.path.abspath(__file__))
ARMS = ["none", "llm", "llm-stale", "agentbrief"]


def usage_tokens(u):
    u = u or {}
    return sum(u.get(k, 0) or 0 for k in ("input_tokens", "cache_creation_input_tokens",
                                            "cache_read_input_tokens", "output_tokens"))


def load(tag):
    rows = []
    for g in sorted(glob.glob(os.path.join(BENCH, "results", tag, "grades", "*.json"))):
        gd = json.load(open(g))
        raw = json.load(open(os.path.join(BENCH, "results", tag, "raw", os.path.basename(g))))
        m = gd["meta"]
        rows.append({"repo": m["repo"], "arm": m["arm"], "rep": m["rep"], "model": m.get("model"),
                     "acc": sum(v["score"] for v in gd["grades"].values()),
                     "q": {k: v["score"] for k, v in gd["grades"].items()},
                     "turns": raw.get("num_turns"), "tokens": usage_tokens(raw.get("usage")),
                     "cost": raw.get("total_cost_usd"), "dur": (raw.get("duration_ms") or 0) / 1000,
                     "err": bool(raw.get("is_error"))})
    return rows


def stat(xs):
    xs = [x for x in xs if x is not None]
    if not xs:
        return "n/a"
    sd = st.stdev(xs) if len(xs) > 1 else 0
    return f"{st.mean(xs):.3f} (sd {sd:.3f}, {min(xs):.3f}-{max(xs):.3f})"


def table(rows, group_key, title):
    out = [f"\n### {title}\n", "| group | arm | n | accuracy /5 | turns | tokens | cost USD | duration s |",
           "|---|---|---|---|---|---|---|---|"]
    keys = sorted({group_key(r) for r in rows})
    for k in keys:
        for arm in ARMS:
            rs = [r for r in rows if group_key(r) == k and r["arm"] == arm]
            if rs:
                out.append(f"| {k} | {arm} | {len(rs)} | {stat([r['acc'] for r in rs])} | {stat([r['turns'] for r in rs])} | "
                           f"{stat([r['tokens'] for r in rs])} | {stat([r['cost'] for r in rs])} | {stat([r['dur'] for r in rs])} |")
    return out


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag", default="sonnet")
    tag = ap.parse_args().tag
    rows = load(tag)
    L = [f"# AgentBrief orientation benchmark - results (tag: {tag})\n",
         f"{len(rows)} graded runs. Cells: mean (sd, min-max) across all runs in the group. "
         "Accuracy = sum of five question scores (0/0.5/1) from the LLM judge.\n"]
    L += table(rows, lambda r: "ALL", "Overall")
    L += ["\n### Per-question mean score (overall)\n", "| arm | q1 | q2 | q3 | q4 | q5 |", "|---|---|---|---|---|---|"]
    for arm in ARMS:
        rs = [r for r in rows if r["arm"] == arm]
        if rs:
            L.append(f"| {arm} | " + " | ".join(f"{st.mean(r['q'][q] for r in rs):.3f}" for q in ("q1", "q2", "q3", "q4", "q5")) + " |")
    L += table(rows, lambda r: r["repo"], "Per repo")
    errs = [r for r in rows if r["err"]]
    L.append(f"\nRuns flagged is_error by the CLI: {len(errs)} " + ", ".join(f"{r['repo']}.{r['arm']}.{r['rep']}" for r in errs))
    # one-off llm generation cost
    gens = []
    for g in sorted(glob.glob(os.path.join(BENCH, "generated", "*.gen.json"))):
        d = json.load(open(g))
        gens.append((os.path.basename(g)[:-9], d.get("total_cost_usd"), usage_tokens(d.get("usage")),
                     d.get("num_turns"), (d.get("duration_ms") or 0) / 1000))
    L += ["\n### One-off cost of generating the llm arm's AGENTS.md (per repo, not included above)\n",
          "| repo | cost USD | tokens | turns | duration s |", "|---|---|---|---|---|"]
    for r, c, t, n, d in gens:
        L.append(f"| {r} | {c} | {t} | {n} | {d:.0f} |")
    if gens:
        L.append(f"| **total** | {sum(g[1] or 0 for g in gens):.3f} | {sum(g[2] for g in gens)} | | {sum(g[4] for g in gens):.0f} |")
    L += ["\n### Every run\n", "| repo | arm | rep | acc | q1 | q2 | q3 | q4 | q5 | turns | tokens | cost | s |",
          "|---|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for r in sorted(rows, key=lambda r: (r["repo"], r["arm"], r["rep"])):
        L.append(f"| {r['repo']} | {r['arm']} | {r['rep']} | {r['acc']} | " + " | ".join(str(r['q'][q]) for q in ("q1","q2","q3","q4","q5"))
                 + f" | {r['turns']} | {r['tokens']} | {r['cost']} | {r['dur']:.0f} |")
    path = os.path.join(BENCH, "results", tag, "summary.md")
    open(path, "w").write("\n".join(L) + "\n")
    print("wrote", path)


if __name__ == "__main__":
    main()
