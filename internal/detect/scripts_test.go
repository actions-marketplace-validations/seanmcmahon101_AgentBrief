package detect

import "testing"

func TestScriptsDir(t *testing.T) {
	f := runOnly(map[string]string{
		"scripts/test":        "#!/bin/sh\n",
		"scripts/check":       "#!/bin/sh\n",
		"scripts/install":     "#!/bin/sh\n",
		"scripts/build":       "no shebang\n",
		"scripts/lint.sh":     "echo",
		"scripts/docs.py":     "#!/usr/bin/env python\n",
		"scripts/test-cov.sh": "x",
		"script/cibuild":      "#!/bin/bash\n",
		"script/bootstrap":    "#!/bin/bash\n",
		"web/scripts/test":    "#!/bin/sh\n",
		"a/b/scripts/test":    "#!/bin/sh\n",
		"other/test":          "#!/bin/sh\n",
	}, detectScriptsDir)
	wantCmds(t, f, ".|test|scripts/test", ".|check|scripts/check", ".|install|scripts/install", ".|lint|bash scripts/lint.sh",
		".|check|script/cibuild", ".|install|script/bootstrap", "web|test|scripts/test")
	noCmds(t, f, ".|build|scripts/build", ".|test|bash scripts/docs.py")
	if len(f.Commands) != 7 || len(f.Tasks) != 7 {
		t.Errorf("commands=%d tasks=%d", len(f.Commands), len(f.Tasks))
	}
	if f.Tasks[0].Runner != "script" {
		t.Errorf("runner %q", f.Tasks[0].Runner)
	}
}

func TestScriptsDirNegative(t *testing.T) {
	f := runOnly(map[string]string{"main.go": "", "scripts/release.sh": "x", "scripts/data.json": "{}"}, detectScriptsDir)
	if len(f.Commands) != 0 {
		t.Errorf("%+v", f.Commands)
	}
}
