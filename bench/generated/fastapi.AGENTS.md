# AGENTS.md

## Commands

Uses `uv` (`uv.lock`, `.python-version` = 3.11; package supports >=3.10).

```bash
uv sync --group dev --extra all        # install (CI tests use: uv sync --no-dev --group tests --extra all)
bash scripts/test.sh                   # full suite: pytest -n auto --dist loadgroup tests scripts/tests/ (sets PYTHONPATH=./docs_src)
bash scripts/test-cov.sh               # same with coverage (CI requires 100%)
bash scripts/lint.sh                   # mypy fastapi; ty check; ruff check; ruff format --check
bash scripts/format.sh                 # ruff check --fix + ruff format
uv run prek run --all-files            # pre-commit hooks (.pre-commit-config.yaml)
```

Single test (set `PYTHONPATH` so tests can import `docs_src` examples; pytest config is in `pyproject.toml`):

```bash
PYTHONPATH=./docs_src pytest tests/test_application.py
PYTHONPATH=./docs_src pytest tests/test_application.py::test_name -p no:xdist
```

Pytest runs with `filterwarnings = error`, `--strict-markers`, `strict_xfail`, and a 20s per-test timeout, so any new warning fails a test. `docs_src` is excluded from collection (`--ignore=docs_src`). Benchmarks live in `tests/benchmarks` and `tests/memory_benchmarks` (run with `--codspeed` in CI).

Docs tooling: `python scripts/docs.py <command>` (typer app: `build-lang`, `build-all`, `serve`, `live`, `generate-readme`, ...). Docs are in `docs/<lang>/`, built with zensical.

## Architecture

FastAPI is a thin layer over Starlette plus Pydantic v2. Understanding a request path spans several files:

- `fastapi/applications.py` (`FastAPI`, subclasses `Starlette`) builds the middleware stack (including `middleware/asyncexitstack.py`), exception handlers, and the `/openapi.json`, `/docs`, `/redoc` routes. Most decorator methods delegate to `self.router`.
- `fastapi/routing.py` holds `APIRouter`, `APIRoute`, `APIWebSocketRoute`, and `get_request_handler()`, which produces the actual ASGI endpoint: parse body, call `solve_dependencies`, validate, run the user function, then serialize against `response_model`.
- `fastapi/dependencies/utils.py` + `models.py`: `get_dependant()` inspects an endpoint's signature (`Annotated`, `Depends`, `Security`, and the param classes in `fastapi/params.py` / `param_functions.py`) into a `Dependant` tree at route-creation time; `solve_dependencies()` resolves it per request (caching, yield-dependency exit stacks, validation errors).
- `fastapi/openapi/utils.py` (`get_openapi`) walks the same `Dependant` trees and route response models to emit the OpenAPI schema; `openapi/models.py` are its Pydantic models, `openapi/docs.py` the Swagger/ReDoc HTML.
- `fastapi/_compat/` wraps Pydantic: `v2.py` defines `ModelField` and schema helpers; `shared.py` has annotation-inspection helpers. `__init__.py` re-exports the names the rest of the package imports, so routing/dependencies/openapi code goes through `_compat` rather than using Pydantic internals directly.
- `fastapi/encoders.py` (`jsonable_encoder`), `responses.py`, `security/`, `sse.py`, `telemetry/`, and `cli.py` (`fastapi` console script) are supporting modules.

## Tests and docs examples

- `docs_src/<topic>/tutorialNNN*_py310.py` are the code samples included into the docs; they are linted by ruff (per-file ignores in `pyproject.toml`) and exercised by `tests/test_tutorial/test_<topic>/`, which import them via the `docs_src` path.
- Tests that touch the working directory use `tests/utils.py:workdir_lock` (an `xdist_group`), which is why `--dist loadgroup` is used.
