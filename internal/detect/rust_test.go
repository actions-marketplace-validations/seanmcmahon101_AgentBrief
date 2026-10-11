package detect

import "testing"

func TestRustWorkspace(t *testing.T) {
	files := map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/*", "tools/xtask"]
exclude = ["crates/old"]
[workspace.package]
rust-version = "1.80"
`,
		"Cargo.lock":                        "",
		"rust-toolchain.toml":               "[toolchain]\nchannel = \"1.82.0\"\n",
		".config/nextest.toml":              "[profile.default]\n",
		".cargo/config.toml":                "[alias]\nxtask = \"run --package xtask --\"\nlint = [\"clippy\", \"--all\"]\ndev = \"run -p dev\"\n",
		"crates/a/Cargo.toml":               "[package]\nname = \"crate-a\"\n",
		"crates/b/Cargo.toml":               "[package]\nname = \"crate-b\"\n",
		"crates/old/Cargo.toml":             "[package]\nname = \"old\"\n",
		"tools/xtask/Cargo.toml":            "[package]\nname = \"xtask\"\n",
		"crates/a/tests/fixture/Cargo.toml": "[package]\nname = \"fx\"\n",
	}
	f := detectWith(files, detectRust)
	needCmds(t, f,
		".|build|cargo build --workspace", ".|test|cargo test --workspace",
		".|test-single|cargo test -p <crate> <test_name>", ".|lint|cargo clippy --all-targets --workspace",
		".|format|cargo fmt --check", ".|test|cargo nextest run", ".|lint|cargo lint")
	forbidCmds(t, f, ".|dev|cargo dev")
	ws := map[string]string{}
	for _, p := range f.Projects {
		ws[p.Name] = p.Workspace
	}
	if ws["crate-a"] != "." || ws["crate-b"] != "." || ws["xtask"] != "." {
		t.Errorf("members: %v", ws)
	}
	if w, ok := ws["old"]; !ok || w != "" {
		t.Errorf("excluded crate must not be a member: %v", ws)
	}
	if !hasToolchain(f, "rust", "1.82.0") || !hasToolchain(f, "rust", ">=1.80") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	if len(f.PackageManagers) != 1 || f.PackageManagers[0].Source.Path != "Cargo.lock" {
		t.Errorf("pm: %+v", f.PackageManagers)
	}
	if tk := findTask(f, "cargo-alias", "xtask"); tk == nil || tk.Body != "cargo run --package xtask --" {
		t.Errorf("alias: %+v", tk)
	}
	if tk := findTask(f, "cargo-alias", "lint"); tk == nil || tk.Body != "cargo clippy --all" {
		t.Errorf("alias array: %+v", tk)
	}
	for _, c := range f.Commands {
		if c.Dir != "." {
			t.Errorf("only the workspace root gets commands: %+v", c)
		}
	}
}

func TestRustSinglePackage(t *testing.T) {
	files := map[string]string{
		"Cargo.toml":     "[package]\nname = \"solo\"\nrust-version = \"1.75\"\n",
		"rust-toolchain": "stable\n",
	}
	f := detectWith(files, detectRust)
	needCmds(t, f, ".|build|cargo build", ".|test|cargo test", ".|test-single|cargo test <test_name>", ".|lint|cargo clippy --all-targets")
	forbidCmds(t, f, ".|test|cargo nextest run")
	if !hasToolchain(f, "rust", "stable") || !hasToolchain(f, "rust", ">=1.75") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
}

func TestRustInvalidManifest(t *testing.T) {
	f := detectWith(map[string]string{"Cargo.toml": "[package\n"}, detectRust)
	if len(f.Commands) != 0 {
		t.Error("invalid manifest should yield nothing")
	}
}

func TestRustNestedStandaloneIsTooling(t *testing.T) {
	files := map[string]string{
		"Cargo.toml":      "[package]\nname = \"main\"\n",
		"fuzz/Cargo.toml": "[package]\nname = \"fuzz\"\n[workspace]\n",
	}
	f := detectWith(files, detectRust)
	forbidCmds(t, f, "fuzz|build|cargo build --workspace")
	if len(f.Projects) != 2 {
		t.Errorf("projects: %+v", f.Projects)
	}
}
