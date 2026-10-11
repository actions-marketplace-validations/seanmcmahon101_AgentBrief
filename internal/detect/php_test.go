package detect

import "testing"

func TestComposerScripts(t *testing.T) {
	composer := `{
  "name": "acme/lib",
  "require": {"php": "^8.1"},
  "require-dev": {"phpunit/phpunit": "^10", "laravel/pint": "^1"},
  "scripts": {
    "test": "phpunit",
    "lint": ["@cs", "@stan"],
    "cs-fix": "php-cs-fixer fix",
    "stan": "phpstan analyse",
    "custom": "echo hi",
    "post-install-cmd": "echo hook",
    "weird": {"a": 1}
  },
  "scripts-descriptions": {"test": "Run tests"}
}`
	files := map[string]string{
		"composer.json": composer, "composer.lock": "{}",
		"phpunit.xml.dist": "<phpunit/>", "phpstan.neon": "", ".php-cs-fixer.dist.php": "",
		"vendor/acme/x/composer.json": "{}",
	}
	f := detectWith(files, detectPHP)
	needCmds(t, f,
		".|install|composer install", ".|test|composer test", ".|lint|composer lint", ".|format|composer cs-fix",
		".|typecheck|composer stan", ".|test|vendor/bin/phpunit", ".|test-single|vendor/bin/phpunit --filter <testName> <file>",
		".|typecheck|vendor/bin/phpstan analyse", ".|format|vendor/bin/php-cs-fixer fix", ".|format|vendor/bin/pint", ".|lint|vendor/bin/pint --test")
	forbidCmds(t, f, ".|install|composer post-install-cmd")
	if tk := findTask(f, "composer", "lint"); tk == nil || tk.Body != "@cs && @stan" {
		t.Errorf("lint: %+v", tk)
	}
	if tk := findTask(f, "composer", "test"); tk == nil || tk.Body != "phpunit" || tk.Desc != "Run tests" {
		t.Errorf("test: %+v", tk)
	}
	if findTask(f, "composer", "post-install-cmd") != nil || findTask(f, "composer", "custom") == nil {
		t.Errorf("tasks: %s", taskNames(f, "composer"))
	}
	if tk := findTask(f, "composer", "weird"); tk == nil || tk.Body != "" {
		t.Errorf("weird: %+v", tk)
	}
	if !hasToolchain(f, "php", "^8.1") || len(f.Projects) != 1 || f.Projects[0].Name != "acme/lib" {
		t.Errorf("toolchain/project: %+v %+v", f.Toolchains, f.Projects)
	}
	if f.PackageManagers[0].Source.Path != "composer.lock" {
		t.Errorf("pm: %+v", f.PackageManagers)
	}
}

func TestComposerLaravelAndPest(t *testing.T) {
	files := map[string]string{
		"composer.json": `{"require":{"laravel/framework":"^11"},"require-dev":{"pestphp/pest":"^2"}}`,
		"artisan":       "",
		"phpunit.xml":   "",
	}
	f := detectWith(files, detectPHP)
	needCmds(t, f, ".|test|php artisan test", ".|test-single|php artisan test --filter <testName>", ".|dev|php artisan serve")
	forbidCmds(t, f, ".|test|vendor/bin/phpunit", ".|test|vendor/bin/pest")

	f = detectWith(map[string]string{"composer.json": `{"require-dev":{"pestphp/pest":"^2"}}`, "phpunit.xml": ""}, detectPHP)
	needCmds(t, f, ".|test|vendor/bin/pest")
	forbidCmds(t, f, ".|test|vendor/bin/phpunit")
}

func TestComposerBad(t *testing.T) {
	f := detectWith(map[string]string{"composer.json": "{nope"}, detectPHP)
	if len(f.Projects) != 0 {
		t.Error("bad json")
	}
	f = detectWith(map[string]string{"composer.json": "{}"}, detectPHP)
	for _, c := range f.Commands {
		if c.Kind == "test" {
			t.Errorf("no test tooling declared: %+v", c)
		}
	}
}
