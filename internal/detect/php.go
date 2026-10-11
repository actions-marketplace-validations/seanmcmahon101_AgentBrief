package detect

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("php", detectPHP) }

type composerJSON struct {
	Name       string                     `json:"name"`
	Require    map[string]json.RawMessage `json:"require"`
	RequireDev map[string]json.RawMessage `json:"require-dev"`
	Scripts    map[string]json.RawMessage `json:"scripts"`
	ScriptDesc map[string]string          `json:"scripts-descriptions"`
}

// composerKinds maps conventional composer script names to command kinds;
// it extends KindForTaskName with names PHP projects favour.
var composerKinds = map[string]string{
	"phpunit": model.KindTest, "cs-fix": model.KindFormat, "fix": model.KindFormat,
	"cs": model.KindLint, "phpcs": model.KindLint, "cs-check": model.KindLint,
	"phpstan": model.KindTypecheck, "stan": model.KindTypecheck, "analyse": model.KindTypecheck,
	"analyze": model.KindTypecheck, "psalm": model.KindTypecheck,
}

func detectPHP(r *scan.Repo, f *Facts) {
	for _, p := range filesNamed(r, "composer.json") {
		if depth(dirOf(p)) > 2 {
			continue
		}
		var c composerJSON
		if err := json.Unmarshal([]byte(r.ReadString(p)), &c); err != nil {
			continue
		}
		detectComposer(r, f, p, c)
	}
}

func detectComposer(r *scan.Repo, f *Facts, file string, c composerJSON) {
	dir := dirOf(file)
	content := r.ReadString(file)
	name := c.Name
	if name == "" {
		name = dir
		if dir == "." {
			name = "root"
		}
	}
	f.AddProject(model.Project{Dir: dir, Name: name, Ecosystem: "php", Manifest: file})
	pmSrc := at(file, 1)
	if lock := joinPath(dir, "composer.lock"); r.Has(lock) {
		pmSrc = at(lock, 1)
	}
	f.AddPackageManager(model.PackageManager{Name: "composer", Ecosystem: "php", Dir: dir, Source: pmSrc})
	if raw, ok := c.Require["php"]; ok {
		var v string
		if json.Unmarshal(raw, &v) == nil && v != "" {
			f.AddToolchain(model.Toolchain{Name: "php", Version: v, Source: at(file, lineNumber(content, `"php"`))})
		}
	}

	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: dir, Evidence: ev, Source: s, Note: note})
	}
	add(model.KindInstall, "composer install", "", conv, at(file, 1))

	names := make([]string, 0, len(c.Scripts))
	for n := range c.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if isComposerEvent(n) {
			continue
		}
		src := at(file, lineNumber(content, `"`+n+`"`))
		f.AddTask(model.Task{Runner: "composer", Name: n, Dir: dir, Body: composerBody(c.Scripts[n]), Desc: truncate(c.ScriptDesc[n], 120), Source: src})
		kind := composerKinds[strings.ToLower(n)]
		if kind == "" {
			kind = KindForTaskName(n)
		}
		if kind != "" {
			add(kind, "composer "+n, "", decl, src)
		}
	}
	addPHPTools(r, f, dir, file, c, add)
}

// isComposerEvent reports composer lifecycle hooks, which are not runnable scripts.
func isComposerEvent(n string) bool {
	return strings.HasPrefix(n, "pre-") || strings.HasPrefix(n, "post-") || n == "command"
}

// composerBody renders a script value; arrays are joined only when every
// element is a plain string.
func composerBody(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return truncate(s, 120)
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		return truncate(strings.Join(arr, " && "), 120)
	}
	return ""
}

func hasPkg(c composerJSON, pkg string) bool {
	_, a := c.Require[pkg]
	_, b := c.RequireDev[pkg]
	return a || b
}

func addPHPTools(r *scan.Repo, f *Facts, dir, file string, c composerJSON, add func(kind, run, note string, ev []string, s model.Source)) {
	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	has := func(names ...string) string { return firstExisting(r, dir, names...) }
	laravel := r.Has(joinPath(dir, "artisan")) && hasPkg(c, "laravel/framework")

	phpunitCfg := has("phpunit.xml", "phpunit.xml.dist")
	switch {
	case laravel:
		add(model.KindTest, "php artisan test", "", conv, at(joinPath(dir, "artisan"), 1))
		add(model.KindTestOne, "php artisan test --filter <testName>", "replace <testName>", conv, at(joinPath(dir, "artisan"), 1))
	case hasPkg(c, "pestphp/pest"):
		add(model.KindTest, "vendor/bin/pest", "", conv, at(file, 1))
		add(model.KindTestOne, "vendor/bin/pest --filter <testName> <file>", "replace <testName> and <file>", conv, at(file, 1))
	case phpunitCfg != "" || hasPkg(c, "phpunit/phpunit"):
		src := at(file, 1)
		ev := conv
		if phpunitCfg != "" {
			src, ev = at(phpunitCfg, 1), decl
		}
		add(model.KindTest, "vendor/bin/phpunit", "", ev, src)
		add(model.KindTestOne, "vendor/bin/phpunit --filter <testName> <file>", "replace <testName> and <file>", ev, src)
	}
	if laravel {
		add(model.KindDev, "php artisan serve", "", conv, at(joinPath(dir, "artisan"), 1))
	}
	if p := has("phpstan.neon", "phpstan.neon.dist", "phpstan.dist.neon"); p != "" {
		add(model.KindTypecheck, "vendor/bin/phpstan analyse", "", decl, at(p, 1))
	}
	if p := has("psalm.xml", "psalm.xml.dist"); p != "" {
		add(model.KindTypecheck, "vendor/bin/psalm", "", decl, at(p, 1))
	}
	if p := has(".php-cs-fixer.php", ".php-cs-fixer.dist.php"); p != "" {
		add(model.KindFormat, "vendor/bin/php-cs-fixer fix", "", decl, at(p, 1))
		add(model.KindLint, "vendor/bin/php-cs-fixer fix --dry-run --diff", "", decl, at(p, 1))
	}
	if p := has("pint.json"); p != "" || hasPkg(c, "laravel/pint") {
		src := at(file, 1)
		if p != "" {
			src = at(p, 1)
		}
		add(model.KindFormat, "vendor/bin/pint", "", decl, src)
		add(model.KindLint, "vendor/bin/pint --test", "", decl, src)
	}
	if p := has("phpcs.xml", "phpcs.xml.dist", ".phpcs.xml", ".phpcs.xml.dist"); p != "" {
		add(model.KindLint, "vendor/bin/phpcs", "", decl, at(p, 1))
		add(model.KindFormat, "vendor/bin/phpcbf", "", decl, at(p, 1))
	}
}
