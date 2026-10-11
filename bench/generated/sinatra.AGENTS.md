# AGENTS.md

## Layout

This repo holds three gems, each with its own Gemfile, Rakefile and test suite:

- `sinatra` (root): `lib/sinatra/`, tests in `test/` (Minitest).
- `rack-protection/`: `lib/rack/protection/`, specs in `spec/` (RSpec).
- `sinatra-contrib/`: `lib/sinatra/*.rb` extensions, specs in `spec/` (RSpec).

The root `Gemfile` pulls in the other two via `path:`. The `rack`, `rack_session`, `puma`, `zeitwerk` env vars select a version (`stable`, `head`, or unset) when you run `bundle install`.

## Commands

```sh
bundle install
bundle exec rake                 # default = test (root Minitest suite)
bundle exec rake test:core       # core test files only, with Ruby warnings on
bundle exec rake test:coverage   # sets COVERAGE=1 (SimpleCov)

# single test file / single test (Minitest)
bundle exec ruby -Itest test/routing_test.rb
bundle exec ruby -Itest test/routing_test.rb -n "/some name/"

# rack-protection and sinatra-contrib: run from inside the subdirectory
cd rack-protection && bundle exec rake            # runs rspec
cd sinatra-contrib && bundle exec rake            # runs rspec
cd rack-protection && bundle exec rspec spec/lib/rack/protection/xss_header_spec.rb
```

Lint: `bundle exec rubocop` (config in `.rubocop.yml`, targets Ruby 2.7). CI (`.github/workflows/test.yml`) runs only the three `bundle exec rake` suites, not rubocop.

Some template tests need system packages (CI installs `pandoc`, `nodejs`, `libxml2-dev`, `libxslt-dev`, `libyaml-dev`). Per the Gemfile comment, tests for engines that fail to load (e.g. commonmarker) are skipped.

## Architecture

- `lib/sinatra.rb` → `lib/sinatra/main.rb`: the classic-style entry point. It parses CLI flags (`-p`, `-s`, `-e`, `-o`, `-q`, `-x`) into `PARAMS_CONFIG` and defines `Sinatra::Application < Base`. The app file is the first file that required `sinatra`, and `run` is true only when that file is `$0`. Top-level DSL methods are forwarded to `Application` through `Sinatra::Delegator`.
- `lib/sinatra/base.rb` (~2k lines) holds nearly everything else: `Request`, `Response`, `Helpers`, `Templates` (Tilt-based rendering), `TemplateCache`, and `Base`, the modular-style app class. `Base` builds a Rack middleware stack (including `Rack::Protection`, host authorization, sessions, logging) and then dispatches routes and filters. Routes are matched with Mustermann.
- `rack-protection` is required unconditionally by `base.rb`; its protections are autoloaded in `lib/rack/protection.rb`, which assembles them into a `Rack::Builder` with `:use` / `:except` options.
- `sinatra-contrib` extensions (`namespace`, `reloader`, `respond_with`, `config_file`, …) build on `Sinatra::Base` and are loaded separately from `sinatra/contrib`.
- `test/test_helper.rb` sets `APP_ENV=test`, puts `lib/` on the load path and uses `test/contest.rb` for the test DSL. The `integration_*` tests start a real server process.
