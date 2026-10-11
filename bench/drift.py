"""Deterministic 'repository drift' mutations for the drift experiment (constructed, NOT natural drift).

Each mutation is applied to the stripped copy of a repo AFTER the llm-arm AGENTS.md was generated
from the unmutated repo, simulating a repo that changed after its instruction file was written.
Every edit asserts that it applied (exact expected match count), so a corpus change fails loudly.
Only manifests, scripts and CI workflows are edited, as a maintainer doing the rename would; prose docs
(README/CONTRIBUTING) are deliberately left alone, as is common in practice.
"""
import glob, os, re


def _sub(path, old, new, count):
    s = open(path).read()
    n = len(re.findall(old, s))
    assert n == count, f"{path}: expected {count} matches of {old!r}, found {n}"
    open(path, "w").write(re.sub(old, new, s))


def _rename(dst, a, b):
    assert os.path.exists(os.path.join(dst, a)) and not os.path.exists(os.path.join(dst, b))
    os.rename(os.path.join(dst, a), os.path.join(dst, b))


def excalidraw(dst):
    """Rename the test/lint scripts: test:app->test:unit, test:code->test:lint,
    test:other->test:format, test:typecheck->test:types. Root package.json and CI updated."""
    pj = os.path.join(dst, "package.json")
    _sub(pj, r"test:app\b", "test:unit", 4)        # test:all, def, test:update, test
    _sub(pj, r"test:code\b", "test:lint", 3)       # test:all, def, fix:code
    _sub(pj, r"test:other\b", "test:format", 2)    # test:all, def
    _sub(pj, r"test:typecheck\b", "test:types", 2)  # test:all, def
    wf = os.path.join(dst, ".github/workflows")
    _sub(f"{wf}/lint.yml", r"test:other\b", "test:format", 1)
    _sub(f"{wf}/lint.yml", r"test:code\b", "test:lint", 1)
    _sub(f"{wf}/lint.yml", r"test:typecheck\b", "test:types", 1)
    _sub(f"{wf}/test.yml", r"test:app\b", "test:unit", 1)


def vite(dst):
    """Rename scripts test-unit->test:unit and lint->lint:js; bump packageManager pnpm 12.9.1->12.10.0."""
    pj = os.path.join(dst, "package.json")
    _sub(pj, r'"test-unit"', '"test:unit"', 1)
    _sub(pj, r"pnpm test-unit\b", "pnpm test:unit", 1)
    s = open(pj).read()
    assert s.count('    "lint": "eslint --cache . --concurrency auto",') == 1
    open(pj, "w").write(s.replace('    "lint": "eslint', '    "lint:js": "eslint', 1))
    _sub(pj, r"pnpm@12\.9\.1", "pnpm@12.10.0", 1)
    ci = os.path.join(dst, ".github/workflows/ci.yml")
    _sub(ci, r"pnpm run test-unit\b", "pnpm run test:unit", 1)
    _sub(ci, r"pnpm run lint\b", "pnpm run lint:js", 1)


def fastapi(dst):
    """Rename scripts/test.sh -> scripts/run-tests.sh and scripts/lint.sh -> scripts/check-style.sh;
    update callers (scripts/test-cov.sh, test-redistribute workflow)."""
    _rename(dst, "scripts/test.sh", "scripts/run-tests.sh")
    _rename(dst, "scripts/lint.sh", "scripts/check-style.sh")
    _sub(os.path.join(dst, "scripts/test-cov.sh"), r"scripts/test\.sh", "scripts/run-tests.sh", 1)
    _sub(os.path.join(dst, ".github/workflows/test-redistribute.yml"), r"scripts/test\.sh", "scripts/run-tests.sh", 1)


def ripgrep(dst):
    """Rename the integration test target 'integration' -> 'it' (Cargo.toml) and add a clippy step to CI."""
    _sub(os.path.join(dst, "Cargo.toml"), r'name = "integration"', 'name = "it"', 1)
    ci = os.path.join(dst, ".github/workflows/ci.yml")
    _sub(ci, r"        components: rustfmt\n    - name: Check formatting\n      run: cargo fmt --all --check\n",
         "        components: rustfmt, clippy\n    - name: Check formatting\n      run: cargo fmt --all --check\n"
         "    - name: Clippy\n      run: cargo clippy --workspace --all-targets -- -D warnings\n", 1)


MUTATIONS = {"excalidraw": excalidraw, "vite": vite, "fastapi": fastapi, "ripgrep": ripgrep}
