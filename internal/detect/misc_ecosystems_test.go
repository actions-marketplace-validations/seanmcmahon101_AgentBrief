package detect

import (
	"strings"
	"testing"
)

func TestMixProject(t *testing.T) {
	mix := `defmodule App.MixProject do
  use Mix.Project

  def project do
    [
      app: :app,
      elixir: "~> 1.15",
      deps: deps(),
      aliases: aliases()
    ]
  end

  defp deps do
    [{:credo, "~> 1.7"}, {:dialyxir, "~> 1.4"}]
  end

  defp aliases do
    [
      setup: ["deps.get", "ecto.setup"],
      "ecto.reset": ["ecto.drop", "ecto.setup"],
      lint: "credo --strict",
      ci: fn _ -> :ok end,
      assets: [
        "a"
      ]
    ]
  end
end
`
	f := detectWith(map[string]string{"mix.exs": mix, "mix.lock": "%{}"}, detectElixir)
	needCmds(t, f, ".|install|mix deps.get", ".|test|mix test", ".|test-single|mix test <file>:<line>",
		".|format|mix format --check-formatted", ".|lint|mix credo", ".|typecheck|mix dialyzer", ".|install|mix setup", ".|lint|mix lint")
	if tk := findTask(f, "mix-alias", "setup"); tk == nil || tk.Body != "mix deps.get && mix ecto.setup" {
		t.Errorf("setup: %+v", tk)
	}
	if tk := findTask(f, "mix-alias", "ecto.reset"); tk == nil {
		t.Errorf("quoted alias: %s", taskNames(f, "mix-alias"))
	}
	if tk := findTask(f, "mix-alias", "ci"); tk == nil || tk.Body != "" {
		t.Errorf("fn alias: %+v", tk)
	}
	if !hasToolchain(f, "elixir", "~> 1.15") {
		t.Errorf("toolchains %+v", f.Toolchains)
	}
}

func TestMixUmbrella(t *testing.T) {
	files := map[string]string{
		"mix.exs":        "defmodule U.MixProject do\n  def project do\n    [apps_path: \"apps\", deps: []]\n  end\nend\n",
		"apps/a/mix.exs": "defmodule A.MixProject do\n  def project, do: [app: :a]\nend\n",
		"deps/x/mix.exs": "defmodule X do end",
	}
	f := detectWith(files, detectElixir)
	if len(f.Projects) != 2 || !f.Projects[0].IsWorkspaceRoot || f.Projects[1].Workspace != "." || f.Projects[1].Name != "a" {
		t.Errorf("projects: %+v", f.Projects)
	}
}

func TestSwift(t *testing.T) {
	pkg := "// swift-tools-version: 5.9\nimport PackageDescription\nlet package = Package(\n    name: \"Lib\",\n    targets: [.testTarget(name: \"LibTests\")]\n)\n"
	f := detectWith(map[string]string{"Package.swift": pkg, ".swiftlint.yml": ""}, detectSwift)
	needCmds(t, f, ".|build|swift build", ".|test|swift test", ".|test-single|swift test --filter <Target>.<Class>/<testMethod>", ".|lint|swiftlint")
	if !hasToolchain(f, "swift", ">=5.9") || f.Projects[0].Name != "Lib" {
		t.Errorf("%+v %+v", f.Toolchains, f.Projects)
	}
	f = detectWith(map[string]string{"Package.swift": "// swift-tools-version:5.5\nlet package = Package(name: \"X\")\n"}, detectSwift)
	forbidCmds(t, f, ".|test|swift test")
	if !hasToolchain(f, "swift", ">=5.5") {
		t.Errorf("%+v", f.Toolchains)
	}
}

func TestCMakePresets(t *testing.T) {
	presets := `{"version":6,
"configurePresets":[{"name":"base","hidden":true},{"name":"dev"}],
"buildPresets":[{"name":"dev","configurePreset":"dev"}],
"testPresets":[{"name":"dev","configurePreset":"dev"}]}`
	files := map[string]string{"CMakeLists.txt": "cmake_minimum_required(VERSION 3.21)\nproject(Demo)\n", "CMakePresets.json": presets}
	f := detectWith(files, detectCpp)
	needCmds(t, f, ".|configure|cmake --preset dev", ".|build|cmake --build --preset dev", ".|test|ctest --preset dev")
	forbidCmds(t, f, ".|install|cmake --preset base", ".|build|cmake --build build")
	if c := cmdKeys(f)[".|test|ctest --preset dev"]; c.Evidence[0] != "declared" {
		t.Errorf("evidence %v", c.Evidence)
	}
	if !hasToolchain(f, "cmake", ">=3.21") {
		t.Errorf("%+v", f.Toolchains)
	}
}

func TestCMakePlain(t *testing.T) {
	f := detectWith(map[string]string{"CMakeLists.txt": "project(P)\nadd_subdirectory(tests)\n", "tests/CMakeLists.txt": "enable_testing()\nadd_test(NAME x COMMAND y)\n"}, detectCpp)
	needCmds(t, f, ".|configure|cmake -S . -B build", ".|build|cmake --build build", ".|test|ctest --test-dir build")
	f = detectWith(map[string]string{"CMakeLists.txt": "project(P)\n"}, detectCpp)
	forbidCmds(t, f, ".|test|ctest --test-dir build")
	needCmds(t, f, ".|build|cmake --build build")
}

func TestMesonAndBazel(t *testing.T) {
	f := detectWith(map[string]string{"meson.build": "project('demo', 'c')\ntest('t', exe)\n"}, detectCpp)
	needCmds(t, f, ".|build|meson compile -C build", ".|test|meson test -C build")
	f = detectWith(map[string]string{"meson.build": "project('demo', 'c')\n"}, detectCpp)
	forbidCmds(t, f, ".|test|meson test -C build")

	f = detectWith(map[string]string{"MODULE.bazel": "module(name='x')", ".bazelversion": "7.1.0\n"}, detectCpp)
	needCmds(t, f, ".|build|bazel build //...", ".|test|bazel test //...")
	if !hasToolchain(f, "bazel", "7.1.0") {
		t.Errorf("%+v", f.Toolchains)
	}
}

func TestDockerCompose(t *testing.T) {
	compose := `services:
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: hunter2
  redis:
    image: redis:7
  web:
    build: .
`
	files := map[string]string{"compose.yaml": compose, ".devcontainer/devcontainer.json": "{}"}
	f := detectWith(files, detectDocker)
	var texts []string
	for _, n := range f.Notes {
		texts = append(texts, n.Text)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "docker compose services: db (postgres:16), redis (redis:7), web") {
		t.Errorf("notes: %s", joined)
	}
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "devcontainer") {
		t.Errorf("notes: %s", joined)
	}
	if f.Notes[0].Source.Path == "" || f.Notes[0].Source.Line != 1 {
		t.Errorf("source: %+v", f.Notes[0].Source)
	}
	f = detectWith(map[string]string{"docker-compose.yml": "version: '3'\n"}, detectDocker)
	if len(f.Notes) != 0 {
		t.Errorf("no services: %+v", f.Notes)
	}
}
