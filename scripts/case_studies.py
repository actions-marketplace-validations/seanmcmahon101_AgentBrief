#!/usr/bin/env python3
"""Reproduce static public-repository case studies without running their code."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
EXAMPLES = ROOT / "examples" / "case-studies"


def digest(p):
    # Git's checkout line endings must not affect evidence identity.
    return hashlib.sha256(p.read_bytes().replace(b"\r\n", b"\n")).hexdigest()


def invoke(binary, repo, *args, expected=0):
    result = subprocess.run(
        [str(binary), *args, "-C", str(repo)],
        capture_output=True, text=True, encoding="utf-8", check=False, timeout=120,
    )
    if result.returncode != expected:
        raise RuntimeError(
            f"AgentBrief {' '.join(args)}: exit {result.returncode}, "
            f"expected {expected}: {result.stderr[:500]}"
        )
    return json.loads(result.stdout) if "--json" in args else result.stdout


def ignored(directory, names):
    heavy = {".git", "node_modules", "target", ".venv", ".cache", "__pycache__"}
    return [n for n in names if n in heavy or (Path(directory) / n).is_symlink()]


def native_path(p):
    # Windows filesystem APIs need the extended prefix for Ruff's long fixture
    # filenames. This changes only host access, never emitted repository paths.
    if os.name == "nt" and not str(p).startswith("\\\\?\\"):
        return Path("\\\\?\\" + str(p.resolve()))
    return p


@contextmanager
def snapshot_directory():
    with tempfile.TemporaryDirectory(prefix="agentbrief-case-") as temp:
        root = Path(temp).resolve()
        extended = native_path(root)
        try:
            yield extended
        finally:
            # Delete exactly the directory this context created. The prefix is
            # necessary for Windows cleanup of long snapshot fixture paths.
            if not os.path.samefile(root, extended):
                raise RuntimeError("temporary directory identity changed")
            shutil.rmtree(extended)


def reproduce(binary, corpus, fixture):
    source = native_path(corpus / fixture["name"])
    for rel, expected in fixture["sources"].items():
        if digest(source / rel) != expected:
            raise RuntimeError(f"{fixture['name']}/{rel}: source differs from pinned evidence")
    with snapshot_directory() as temp:
        snapshot = temp / fixture["name"]
        shutil.copytree(source, snapshot, ignore=ignored)
        config = snapshot / ".agentbrief.yaml"
        if config.exists():
            raise RuntimeError("case-study snapshot already contains a policy")
        baseline = invoke(binary, snapshot, "brief", "--json")
        shutil.copyfile(EXAMPLES / (fixture["name"] + ".yaml"), config)
        cases = []
        for case in fixture["cases"]:
            p = invoke(binary, snapshot, "plan", "--json", "--strict", *case["paths"])
            ids = [s["id"] for s in p["steps"]]
            if ids != case["steps"] or not p["mapping_complete"] or p["executed"] or p["coverage"] != "unknown":
                raise AssertionError(f"{fixture['name']}/{case['name']}: unexpected plan {p}")
            for step in p["steps"]:
                if not step["command"]["source"].get("line"):
                    raise AssertionError("command has no exact declaration source")
            cases.append({
                "name": case["name"], "paths": p["paths"], "steps": ids,
                "mapping_complete": p["mapping_complete"],
                "coverage": p["coverage"], "executed": p["executed"],
                "protected_paths": sorted({i["path"] for i in p["protected_paths"]}),
            })
        # Deliberately remove a command reference in the disposable snapshot.
        if fixture["name"] == "vite":
            pkg = snapshot / "package.json"
            data = json.loads(pkg.read_text(encoding="utf-8"))
            data["scripts"]["renamed-test-serve"] = data["scripts"].pop("test-serve")
            pkg.write_text(json.dumps(data), encoding="utf-8")
        else:
            rel = "scripts/generate_mkdocs.py" if fixture["name"] == "ruff" else "scripts/test.sh"
            (snapshot / rel).rename(snapshot / (rel + ".removed"))
        findings = invoke(binary, snapshot, "check", "--json", expected=1)
        errors = [f for f in findings["findings"] if f["path"] == ".agentbrief.yaml" and f["severity"] == "error"]
        if not errors:
            raise AssertionError("removed policy command was not rejected")
        invoke(binary, snapshot, "plan", *fixture["cases"][0]["paths"], expected=2)
        return {
            "repository": fixture["repository"], "commit": fixture["commit"],
            "baseline_commands": [{"kind": c["kind"], "run": c["run"], "dir": c["dir"]} for c in baseline["commands"]],
            "cases": cases,
            "drift": {"detected_rules": sorted({f["rule"] for f in errors}), "plan_refused": True},
        }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--corpus", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    fixtures = json.loads((EXAMPLES / "fixtures.json").read_text(encoding="utf-8"))
    results = [reproduce(args.binary.resolve(), args.corpus.resolve(), f) for f in fixtures]
    report = {
        "scope": "Static plan selection and planted drift; no repository commands or agent trials.",
        "repositories": results,
    }
    text = json.dumps(report, indent=2, ensure_ascii=True) + "\n"
    if args.output:
        args.output.write_text(text, encoding="utf-8")
    else:
        print(text, end="")


if __name__ == "__main__":
    main()
