# AGENTS.md

## Commands

Scripts in `scripts/` use `venv/bin/` automatically if a `venv` directory exists.

- Install: `scripts/install` (creates `venv`, installs `requirements.txt`)
- Test: `scripts/test` — runs `scripts/check` first (locally), then `coverage run -m pytest "$@"`, then `scripts/coverage` which **fails under 100% coverage**
- Lint/type-check (no changes): `scripts/check` — `scripts/sync-version`, `ruff format --diff`, `mypy` (strict), `ruff check` over `httpx tests`
- Auto-fix: `scripts/lint` — `ruff check --fix` + `ruff format`
- Docs: `scripts/docs` (mkdocs serve); build/package: `scripts/build`

Single test (skips lint and coverage gate):

```
pytest tests/test_auth.py::test_basic_auth
pytest tests/client/test_client.py -k redirect
```

Or via the wrapper, passing pytest args: `scripts/test tests/test_auth.py`.

Notes:
- pytest is configured with `filterwarnings = ["error"]`, so any warning fails a test.
- Tests marked `network` need network access (`-m "not network"` to skip).
- `scripts/sync-version` fails if the version in `httpx/__version__.py` doesn't match the latest entry in `CHANGELOG.md`.

## Architecture

Package `httpx/` (private modules prefixed `_`, public API re-exported in `httpx/__init__.py`; `tests/test_exported_members.py` checks exports). CLI entry point is `httpx:main` (`_main.py`).

- **Client layer** (`_client.py`): `BaseClient` holds shared logic (request building, config, merging); `Client` (sync) and `AsyncClient` (async) duplicate the send pipeline: `send` → `_send_handling_auth` → `_send_handling_redirects` → `_send_single_request`. Changes to one usually need mirroring in the other. Top-level helpers (`httpx.get`, etc.) in `_api.py` wrap a temporary `Client`.
- **Transports** (`_transports/`): the client delegates actual I/O to a transport chosen per-URL by `_transport_for_url` (mounts/proxies). `base.py` defines `BaseTransport` / `AsyncBaseTransport`. `default.py` (`HTTPTransport`, `AsyncHTTPTransport`) wraps the external `httpcore` library and maps its exceptions to httpx exceptions. `asgi.py`, `wsgi.py`, and `mock.py` call apps directly without a network (used heavily in tests).
- **Models** (`_models.py`: `Request`, `Response`, `Headers`, `Cookies`; `_urls.py` + `_urlparse.py`: `URL`/`QueryParams` with a custom RFC 3986 parser). Request bodies are encoded in `_content.py` / `_multipart.py`; response decoding in `_decoders.py`.
- **Auth** (`_auth.py`): auth classes are generators that yield requests and receive responses, driven by `_send_handling_auth`.

## Testing

- `tests/conftest.py` provides a session-scoped real `uvicorn` `server` fixture (served in a thread, with trustme certs) plus small ASGI apps; many tests instead use `httpx.MockTransport`.
- Async tests use `@pytest.mark.anyio` (trio is in `requirements.txt`; `tests/concurrency.py` has backend-agnostic helpers).
