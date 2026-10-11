package detect

import (
	"encoding/xml"
	"path"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("jvm", detectJVM) }

func detectJVM(r *scan.Repo, f *Facts) {
	detectMaven(r, f)
	detectGradle(r, f)
}

// ===================== Maven =====================

type pomProp struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type pomPlugin struct {
	ArtifactID string `xml:"artifactId"`
}

type pomXML struct {
	ArtifactID string `xml:"artifactId"`
	Parent     struct {
		ArtifactID string `xml:"artifactId"`
	} `xml:"parent"`
	Modules    []string `xml:"modules>module"`
	Properties struct {
		Items []pomProp `xml:",any"`
	} `xml:"properties"`
	Plugins        []pomPlugin `xml:"build>plugins>plugin"`
	ManagedPlugins []pomPlugin `xml:"build>pluginManagement>plugins>plugin"`
}

type pomInfo struct {
	dir, file string
	pom       pomXML
}

const maxMavenDepth = 6

func detectMaven(r *scan.Repo, f *Facts) {
	visited := map[string]bool{}
	for _, p := range filesNamed(r, "pom.xml") {
		if visited[dirOf(p)] || strings.Contains("/"+p, "/src/") {
			continue
		}
		var tree []pomInfo
		collectPoms(r, dirOf(p), p, 0, visited, &tree)
		if len(tree) > 0 {
			addMavenTree(r, f, tree)
		}
	}
}

func collectPoms(r *scan.Repo, dir, file string, level int, visited map[string]bool, out *[]pomInfo) {
	if visited[dir] || level > maxMavenDepth || !r.Has(file) {
		return
	}
	visited[dir] = true
	info := pomInfo{dir: dir, file: file}
	if err := xml.Unmarshal([]byte(r.ReadString(file)), &info.pom); err != nil {
		return
	}
	*out = append(*out, info)
	for _, m := range info.pom.Modules {
		m = strings.TrimSpace(m)
		if m == "" || strings.Contains(m, "${") {
			continue
		}
		child := path.Clean(joinPath(dir, m))
		childFile := joinPath(child, "pom.xml")
		if strings.HasSuffix(m, ".xml") {
			childFile, child = child, dirOf(child)
		}
		if strings.HasPrefix(child, "..") {
			continue
		}
		collectPoms(r, child, childFile, level+1, visited, out)
	}
}

var mavenWrapperVersionRe = regexp.MustCompile(`apache-maven-([0-9][^/-]*(?:-[A-Za-z0-9.]+)?)-bin`)

func addMavenTree(r *scan.Repo, f *Facts, tree []pomInfo) {
	root := tree[0]
	multi := len(tree) > 1
	for i, t := range tree {
		name := t.pom.ArtifactID
		if name == "" {
			name = path.Base(t.dir)
		}
		p := model.Project{Dir: t.dir, Name: name, Ecosystem: "java", Manifest: t.file}
		if i == 0 {
			p.IsWorkspaceRoot = multi
		} else {
			p.Workspace = root.dir
		}
		f.AddProject(p)
	}

	mvn := "mvn"
	if r.Has(joinPath(root.dir, "mvnw")) {
		mvn = "./mvnw"
	}
	pm := model.PackageManager{Name: "maven", Ecosystem: "java", Dir: root.dir, Source: at(root.file, 1)}
	if wp := joinPath(root.dir, ".mvn/wrapper/maven-wrapper.properties"); r.Has(wp) {
		if m := mavenWrapperVersionRe.FindStringSubmatch(r.ReadString(wp)); m != nil {
			pm.Version = m[1]
			pm.Source = at(wp, 1)
		}
	}
	f.AddPackageManager(pm)

	rootContent := r.ReadString(root.file)
	for _, key := range []string{"maven.compiler.release", "java.version", "maven.compiler.source", "maven.compiler.target"} {
		if v := pomProperty(root.pom, key); v != "" && !strings.Contains(v, "${") {
			f.AddToolchain(model.Toolchain{Name: "java", Version: v, Source: at(root.file, lineNumber(rootContent, "<"+key+">"))})
			break
		}
	}

	plugins := map[string]model.Source{} // artifactId -> where it is declared
	for _, t := range tree {
		for _, pl := range append(append([]pomPlugin(nil), t.pom.Plugins...), t.pom.ManagedPlugins...) {
			if _, ok := plugins[pl.ArtifactID]; !ok {
				plugins[pl.ArtifactID] = at(t.file, lineNumber(r.ReadString(t.file), "<artifactId>"+pl.ArtifactID+"</artifactId>"))
			}
		}
	}
	addMavenCommands(r, f, root, mvn, multi, plugins)
}

func pomProperty(p pomXML, key string) string {
	for _, it := range p.Properties.Items {
		if it.XMLName.Local == key {
			return strings.TrimSpace(it.Value)
		}
	}
	return ""
}

func addMavenCommands(r *scan.Repo, f *Facts, root pomInfo, mvn string, multi bool, plugins map[string]model.Source) {
	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	src := at(root.file, 1)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: root.dir, Evidence: ev, Source: s, Note: note})
	}
	add(model.KindCheck, mvn+" -B verify", "", conv, src)
	add(model.KindBuild, mvn+" package", "", conv, src)
	add(model.KindTest, mvn+" test", "", conv, src)
	if multi {
		add(model.KindTestOne, mvn+" test -pl <module> -am -Dtest=ClassName#method -Dsurefire.failIfNoSpecifiedTests=false",
			"replace <module>, ClassName and method; -am builds the module's dependencies", conv, src)
	} else {
		add(model.KindTestOne, mvn+" test -Dtest=ClassName#method", "replace ClassName and method", conv, src)
	}
	if file, ok := plugins["spotless-maven-plugin"]; ok {
		add(model.KindFormat, mvn+" spotless:apply", "", decl, file)
		add(model.KindLint, mvn+" spotless:check", "", decl, file)
	}
	if file, ok := plugins["spring-javaformat-maven-plugin"]; ok {
		add(model.KindFormat, mvn+" spring-javaformat:apply", "", decl, file)
		add(model.KindLint, mvn+" spring-javaformat:validate", "", decl, file)
	}
	if file, ok := plugins["maven-checkstyle-plugin"]; ok {
		add(model.KindLint, mvn+" checkstyle:check", "", decl, file)
	}
	// spring-boot:run only works from the root of a single-module build.
	if bootPom, boot := plugins["spring-boot-maven-plugin"]; boot && !multi {
		add(model.KindDev, mvn+" spring-boot:run", "", decl, bootPom)
	}
}

// ===================== Gradle =====================

var (
	gradleIncludeRe  = regexp.MustCompile(`(?m)^\s*include\s*\(?\s*((?:["'][^"'\n]+["']\s*,?\s*)+)`)
	gradleQuotedRe   = regexp.MustCompile(`["']([^"']+)["']`)
	gradleRootNameRe = regexp.MustCompile(`rootProject\.name\s*=\s*["']([^"']+)["']`)
	gradleDistRe     = regexp.MustCompile(`gradle-([0-9][^/]*?)-(?:bin|all)\.zip`)
	javaLangVerRe    = regexp.MustCompile(`JavaLanguageVersion\.of\(\s*(\d+)\s*\)`)
	jvmToolchainRe   = regexp.MustCompile(`jvmToolchain\(\s*(\d+)\s*\)`)
	javaCompatRe     = regexp.MustCompile(`sourceCompatibility\s*=\s*['"]?(?:JavaVersion\.VERSION_)?([0-9][0-9._]*)`)
	blockCommentRe   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRe    = regexp.MustCompile(`(?m)(^|\s)//.*$`)

	gradleSpotlessRe = regexp.MustCompile(`com\.diffplug\.spotless|libs\.plugins\.spotless|id\(?\s*["']spotless["']`)
	gradleKtlintRe   = regexp.MustCompile(`org\.jlleitschuh\.gradle\.ktlint|libs\.plugins\.ktlint`)
	gradleDetektRe   = regexp.MustCompile(`io\.gitlab\.arturbosch\.detekt|libs\.plugins\.detekt`)
	gradleBootRe     = regexp.MustCompile(`id\(?\s*["']org\.springframework\.boot["']|libs\.plugins\.spring\.?[Bb]oot`)
)

const maxGradleMemberFiles = 60

type gradleBuild struct {
	dir     string
	setting string // settings file, or "" for a settings-less root build
	members []string
}

func gradleBuildFile(r *scan.Repo, dir string) string {
	return firstExisting(r, dir, "build.gradle.kts", "build.gradle")
}

func detectGradle(r *scan.Repo, f *Facts) {
	candidates := map[string]string{}
	for _, p := range filesNamed(r, "settings.gradle", "settings.gradle.kts") {
		if _, ok := candidates[dirOf(p)]; !ok {
			candidates[dirOf(p)] = p
		}
	}
	if _, ok := candidates["."]; !ok && gradleBuildFile(r, ".") != "" {
		candidates["."] = ""
	}
	for dir, settings := range candidates {
		if hasAncestorBuild(candidates, dir) {
			continue // included/composite builds are not entry points
		}
		addGradleBuild(r, f, gradleBuild{dir: dir, setting: settings})
	}
}

func hasAncestorBuild(candidates map[string]string, dir string) bool {
	for d := dir; d != "."; {
		d = path.Dir(d)
		if _, ok := candidates[d]; ok {
			return true
		}
	}
	return false
}

func stripGradleComments(s string) string {
	return lineCommentRe.ReplaceAllString(blockCommentRe.ReplaceAllString(s, ""), "$1")
}

func addGradleBuild(r *scan.Repo, f *Facts, b gradleBuild) {
	settings := ""
	if b.setting != "" {
		settings = stripGradleComments(r.ReadString(b.setting))
	}
	name := path.Base(b.dir)
	if b.dir == "." {
		name = "root"
	}
	if m := gradleRootNameRe.FindStringSubmatch(settings); m != nil {
		name = m[1]
	}
	for _, m := range gradleIncludeRe.FindAllStringSubmatch(settings, -1) {
		for _, q := range gradleQuotedRe.FindAllStringSubmatch(m[1], -1) {
			dir := path.Clean(joinPath(b.dir, strings.ReplaceAll(strings.Trim(q[1], ":"), ":", "/")))
			if r.IsDir(dir) && dir != b.dir {
				b.members = append(b.members, dir)
			}
		}
	}
	manifest := b.setting
	if manifest == "" {
		manifest = gradleBuildFile(r, b.dir)
	}
	f.AddProject(model.Project{Dir: b.dir, Name: name, Ecosystem: "java", Manifest: manifest, IsWorkspaceRoot: len(b.members) > 0})
	for _, d := range b.members {
		mf := gradleBuildFile(r, d)
		if mf == "" {
			mf = manifest
		}
		f.AddProject(model.Project{Dir: d, Name: path.Base(d), Ecosystem: "java", Manifest: mf, Workspace: b.dir})
	}

	gw := "gradle"
	if r.Has(joinPath(b.dir, "gradlew")) {
		gw = "./gradlew"
	}
	pm := model.PackageManager{Name: "gradle", Ecosystem: "java", Dir: b.dir, Source: at(manifest, 1)}
	if wp := joinPath(b.dir, "gradle/wrapper/gradle-wrapper.properties"); r.Has(wp) {
		content := r.ReadString(wp)
		if m := gradleDistRe.FindStringSubmatch(content); m != nil {
			pm.Version = m[1]
			pm.Source = at(wp, lineNumber(content, "distributionUrl"))
			f.AddToolchain(model.Toolchain{Name: "gradle", Version: m[1], Source: pm.Source})
		}
	}
	f.AddPackageManager(pm)
	if r.Has(joinPath(b.dir, "pom.xml")) {
		f.AddNote(model.Note{Text: "both pom.xml and Gradle build files exist in " + b.dir + "; Maven and Gradle commands are both listed", Source: at(manifest, 1)})
	}
	scanGradleBuildFiles(r, f, b, gw, manifest)
}

// scanGradleBuildFiles reads the root and member build scripts once, looking
// for the Java toolchain and well-known plugins.
func scanGradleBuildFiles(r *scan.Repo, f *Facts, b gradleBuild, gw, manifest string) {
	files := []string{}
	if rf := gradleBuildFile(r, b.dir); rf != "" {
		files = append(files, rf)
	}
	for _, d := range b.members {
		if mf := gradleBuildFile(r, d); mf != "" && len(files) < maxGradleMemberFiles {
			files = append(files, mf)
		}
	}
	rootFile := gradleBuildFile(r, b.dir)
	javaDone := false
	plugins := map[string]string{} // plugin -> declaring file
	for _, p := range files {
		content := r.ReadString(p)
		if !javaDone {
			javaDone = addGradleJava(f, content, p)
		}
		for name, re := range map[string]*regexp.Regexp{"spotless": gradleSpotlessRe, "ktlint": gradleKtlintRe, "detekt": gradleDetektRe, "boot": gradleBootRe} {
			if _, ok := plugins[name]; !ok && re.MatchString(content) {
				plugins[name] = p
			}
		}
	}

	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	multi := len(b.members) > 0
	src := at(manifest, 1)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: b.dir, Evidence: ev, Source: s, Note: note})
	}
	add(model.KindBuild, gw+" build", "", conv, src)
	add(model.KindTest, gw+" test", "", conv, src)
	add(model.KindCheck, gw+" check", "", conv, src)
	if multi {
		add(model.KindTestOne, gw+" :module:test --tests 'com.example.ClassName.method'", "replace :module, com.example.ClassName and method", conv, src)
	} else {
		add(model.KindTestOne, gw+" test --tests 'com.example.ClassName.method'", "replace com.example.ClassName and method", conv, src)
	}
	if p, ok := plugins["spotless"]; ok {
		add(model.KindFormat, gw+" spotlessApply", "", decl, at(p, 1))
		add(model.KindLint, gw+" spotlessCheck", "", decl, at(p, 1))
	}
	if p, ok := plugins["ktlint"]; ok {
		add(model.KindFormat, gw+" ktlintFormat", "", decl, at(p, 1))
		add(model.KindLint, gw+" ktlintCheck", "", decl, at(p, 1))
	}
	if p, ok := plugins["detekt"]; ok {
		add(model.KindLint, gw+" detekt", "", decl, at(p, 1))
	}
	// bootRun on the root only makes sense when the root applies the plugin.
	if p, ok := plugins["boot"]; ok && (!multi || p == rootFile) {
		add(model.KindDev, gw+" bootRun", "", decl, at(p, 1))
	}
}

func addGradleJava(f *Facts, content, file string) bool {
	for _, re := range []*regexp.Regexp{javaLangVerRe, jvmToolchainRe, javaCompatRe} {
		if m := re.FindStringSubmatch(content); m != nil {
			f.AddToolchain(model.Toolchain{Name: "java", Version: strings.ReplaceAll(m[1], "_", "."), Source: at(file, lineNumber(content, m[0]))})
			return true
		}
	}
	return false
}
