package detect

import "testing"

const testCsproj = `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup>
<PackageReference Include="Microsoft.NET.Test.Sdk" Version="17" />
<PackageReference Include="xunit" Version="2" /></ItemGroup></Project>`

func TestDotnetSingleSolution(t *testing.T) {
	files := map[string]string{
		"App.sln": `Project("{FAE04EC0}") = "App", "src\App\App.csproj", "{1}"
Project("{FAE04EC0}") = "App.Tests", "tests\App.Tests\App.Tests.csproj", "{2}"`,
		"global.json":                      `{"sdk": {"version": "8.0.100", "rollForward": "latestFeature"}}`,
		"src/App/App.csproj":               `<Project Sdk="Microsoft.NET.Sdk"/>`,
		"tests/App.Tests/App.Tests.csproj": testCsproj,
		"src/App/obj/Generated.csproj":     "",
		"testdata/x/Fixture.csproj":        "",
	}
	f := detectWith(files, detectDotnet)
	needCmds(t, f, ".|install|dotnet restore", ".|build|dotnet build", ".|test|dotnet test",
		`.|test-single|dotnet test --filter "FullyQualifiedName~Namespace.Class.Method"`, ".|format|dotnet format --verify-no-changes")
	if !hasToolchain(f, "dotnet", "8.0.100") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	ws := map[string]string{}
	for _, p := range f.Projects {
		ws[p.Dir] = p.Workspace
	}
	if len(f.Projects) != 3 || ws["tests/App.Tests"] != "." || ws["src/App"] != "." {
		t.Errorf("projects: %+v", f.Projects)
	}
}

func TestDotnetMultipleSolutions(t *testing.T) {
	files := map[string]string{"A.sln": "", "B.sln": "", "a/a.csproj": "<Project/>"}
	f := detectWith(files, detectDotnet)
	needCmds(t, f, ".|build|dotnet build A.sln", ".|build|dotnet build B.sln")
	forbidCmds(t, f, ".|build|dotnet build")
	if len(f.Notes) != 1 {
		t.Errorf("notes: %+v", f.Notes)
	}
	for _, c := range f.Commands {
		if c.Kind == "test" {
			t.Errorf("no test projects: %+v", c)
		}
	}
}

func TestDotnetSlnPlusCsprojSameDirNeedsExplicitFile(t *testing.T) {
	f := detectWith(map[string]string{"X.sln": "", "X.csproj": "<Project/>"}, detectDotnet)
	needCmds(t, f, ".|build|dotnet build X.sln")
}

func TestDotnetCsprojOnlyAndSlnx(t *testing.T) {
	f := detectWith(map[string]string{"Tool.csproj": "<Project/>"}, detectDotnet)
	needCmds(t, f, ".|build|dotnet build")
	f = detectWith(map[string]string{"src/Lib.slnx": `<Solution><Project Path="Lib/Lib.csproj" /></Solution>`, "src/Lib/Lib.csproj": "<Project/>"}, detectDotnet)
	needCmds(t, f, "src|build|dotnet build")
	if f.Projects[0].Workspace != "src" {
		t.Errorf("slnx membership: %+v", f.Projects)
	}
	f = detectWith(map[string]string{"README.md": ""}, detectDotnet)
	if len(f.Commands) != 0 {
		t.Error("nothing to detect")
	}
}
