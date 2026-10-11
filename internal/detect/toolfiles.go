package detect

import (
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("toolfiles", detectToolFiles) }

// toolAliases maps asdf/mise plugin names to the names used elsewhere.
var toolAliases = map[string]string{"nodejs": "node", "golang": "go"}

func canonicalTool(name string) string {
	if a, ok := toolAliases[name]; ok {
		return a
	}
	return name
}

func detectToolFiles(r *scan.Repo, f *Facts) {
	for _, dir := range projectDirs(f) {
		if p := joinPath(dir, ".tool-versions"); r.Has(p) {
			parseToolVersions(r.ReadString(p), p, f)
		}
		for _, rel := range miseConfigPaths {
			if p := joinPath(dir, rel); r.Has(p) {
				parseMiseTools(r.ReadString(p), p, f)
			}
		}
	}
}

func parseToolVersions(content, file string, f *Facts) {
	for i, line := range strings.Split(content, "\n") {
		if c := strings.Index(line, "#"); c >= 0 {
			line = line[:c]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] == "system" || strings.HasPrefix(fields[1], "ref:") || strings.HasPrefix(fields[1], "path:") {
			continue
		}
		f.AddToolchain(model.Toolchain{Name: canonicalTool(fields[0]), Version: fields[1], Source: at(file, i+1)})
	}
}

// parseMiseTools reads the [tools] table. Entries with a backend prefix
// (npm:, cargo:, ...) install packages rather than runtimes and are skipped.
func parseMiseTools(content, file string, f *Facts) {
	var cfg struct {
		Tools map[string]any `toml:"tools"`
	}
	if _, err := toml.Decode(content, &cfg); err != nil {
		return
	}
	for name, v := range cfg.Tools {
		if strings.Contains(name, ":") {
			continue
		}
		ver := miseToolVersion(v)
		if ver == "" {
			continue
		}
		f.AddToolchain(model.Toolchain{Name: canonicalTool(name), Version: ver, Source: at(file, toolLine(content, name))})
	}
}

func miseToolVersion(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			return miseToolVersion(t[0])
		}
	case map[string]any:
		if s, ok := t["version"].(string); ok {
			return s
		}
	}
	return ""
}

func toolLine(content, name string) int {
	for i, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, name+" ") || strings.HasPrefix(t, name+"=") || strings.HasPrefix(t, `"`+name+`"`) {
			return i + 1
		}
	}
	return 0
}
