package detect

import "testing"

const rootPom = `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>g</groupId>
  <artifactId>parent</artifactId>
  <packaging>pom</packaging>
  <properties>
    <maven.compiler.release>17</maven.compiler.release>
  </properties>
  <modules>
    <module>core</module>
    <module>web</module>
  </modules>
  <build>
    <pluginManagement><plugins>
      <plugin><artifactId>spotless-maven-plugin</artifactId></plugin>
    </plugins></pluginManagement>
    <plugins>
      <plugin><artifactId>maven-checkstyle-plugin</artifactId></plugin>
    </plugins>
  </build>
</project>`

func TestMavenMultiModule(t *testing.T) {
	files := map[string]string{
		"pom.xml":                               rootPom,
		"mvnw":                                  "#!/bin/sh",
		".mvn/wrapper/maven-wrapper.properties": "distributionUrl=https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/3.9.6/apache-maven-3.9.6-bin.zip\n",
		"core/pom.xml":                          `<project><artifactId>core</artifactId><modules><module>inner</module></modules></project>`,
		"core/inner/pom.xml":                    `<project><artifactId>inner</artifactId></project>`,
		"web/pom.xml":                           `<project><artifactId>web</artifactId></project>`,
		"src/test/resources/x/pom.xml":          `<project><artifactId>fixture</artifactId></project>`,
	}
	f := detectWith(files, detectMaven)
	needCmds(t, f,
		".|check|./mvnw -B verify", ".|test|./mvnw test", ".|build|./mvnw package",
		".|test-single|./mvnw test -pl <module> -am -Dtest=ClassName#method -Dsurefire.failIfNoSpecifiedTests=false",
		".|format|./mvnw spotless:apply", ".|lint|./mvnw spotless:check", ".|lint|./mvnw checkstyle:check")
	forbidCmds(t, f, ".|dev|./mvnw spring-boot:run")
	names := map[string]string{}
	for _, p := range f.Projects {
		names[p.Name] = p.Workspace
	}
	if len(f.Projects) != 4 || names["inner"] != "." || names["fixture"] != "" {
		t.Errorf("projects: %+v", f.Projects)
	}
	if !hasToolchain(f, "java", "17") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	if len(f.PackageManagers) != 1 || f.PackageManagers[0].Version != "3.9.6" {
		t.Errorf("pm: %+v", f.PackageManagers)
	}
	if c := cmdKeys(f)[".|lint|./mvnw checkstyle:check"]; c.Source.Line != 18 {
		t.Errorf("plugin line: %+v", c.Source)
	}
}

func TestMavenSingleSpringBoot(t *testing.T) {
	pom := `<project><artifactId>app</artifactId>
<parent><artifactId>spring-boot-starter-parent</artifactId></parent>
<properties><java.version>21</java.version></properties>
<build><plugins><plugin><artifactId>spring-boot-maven-plugin</artifactId></plugin></plugins></build></project>`
	f := detectWith(map[string]string{"pom.xml": pom}, detectMaven)
	needCmds(t, f, ".|test-single|mvn test -Dtest=ClassName#method", ".|dev|mvn spring-boot:run", ".|test|mvn test")
	if !hasToolchain(f, "java", "21") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
}

func TestMavenInvalidPom(t *testing.T) {
	f := detectWith(map[string]string{"pom.xml": "<project><unclosed>"}, detectMaven)
	if len(f.Commands) != 0 {
		t.Errorf("commands: %+v", f.Commands)
	}
}

func TestGradleMultiProject(t *testing.T) {
	files := map[string]string{
		"settings.gradle.kts": `// comment include("ignored")
rootProject.name = "demo"
/* include("blocked") */
include("app", ":libs:core")
include(
    "svc",
    ":missing"
)
`,
		"gradlew": "",
		"gradle/wrapper/gradle-wrapper.properties": "distributionUrl=https\\://services.gradle.org/distributions/gradle-8.5-bin.zip\n",
		"build.gradle.kts":                         "plugins { id(\"com.diffplug.spotless\") version \"6\" }\n",
		"app/build.gradle.kts":                     "plugins { id(\"org.springframework.boot\") }\njava { toolchain { languageVersion.set(JavaLanguageVersion.of(21)) } }\n",
		"libs/core/build.gradle.kts":               "plugins { id(\"io.gitlab.arturbosch.detekt\") }\n",
		"svc/build.gradle.kts":                     "",
		"build-logic/settings.gradle.kts":          "rootProject.name = \"logic\"\n",
	}
	f := detectWith(files, detectGradle)
	needCmds(t, f,
		".|build|./gradlew build", ".|test|./gradlew test", ".|check|./gradlew check",
		".|test-single|./gradlew :module:test --tests 'com.example.ClassName.method'",
		".|format|./gradlew spotlessApply", ".|lint|./gradlew spotlessCheck", ".|lint|./gradlew detekt")
	forbidCmds(t, f, ".|dev|./gradlew bootRun") // plugin is in a member, not the root
	dirs := map[string]bool{}
	for _, p := range f.Projects {
		dirs[p.Dir] = true
	}
	for _, d := range []string{".", "app", "libs/core", "svc"} {
		if !dirs[d] {
			t.Errorf("missing project %s: %+v", d, f.Projects)
		}
	}
	if dirs["ignored"] || dirs["blocked"] || dirs["missing"] || dirs["build-logic"] {
		t.Errorf("bad projects: %+v", f.Projects)
	}
	if !hasToolchain(f, "gradle", "8.5") || !hasToolchain(f, "java", "21") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	if f.Projects[0].Name != "demo" {
		t.Errorf("root name: %+v", f.Projects[0])
	}
}

func TestGradleSingleGroovy(t *testing.T) {
	files := map[string]string{
		"settings.gradle": "rootProject.name = 'x'\ninclude 'a', 'b'\n",
		"build.gradle":    "plugins {\n id 'org.springframework.boot' version '3'\n}\njava { sourceCompatibility = '17' }\n",
		"a/x.txt":         "", "b/x.txt": "",
	}
	f := detectWith(files, detectGradle)
	needCmds(t, f, ".|build|gradle build", ".|dev|gradle bootRun")
	if !hasToolchain(f, "java", "17") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	// ktlint plugin with a version catalog alias
	f = detectWith(map[string]string{"build.gradle.kts": "plugins { alias(libs.plugins.ktlint) }\njvmToolchain(17)\n"}, detectGradle)
	needCmds(t, f, ".|lint|gradle ktlintCheck", ".|format|gradle ktlintFormat", ".|test-single|gradle test --tests 'com.example.ClassName.method'")
	if !hasToolchain(f, "java", "17") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
}

func TestGradleNone(t *testing.T) {
	f := detectWith(map[string]string{"README.md": "x"}, detectGradle)
	if len(f.Commands) != 0 {
		t.Error("no gradle files, no commands")
	}
}
