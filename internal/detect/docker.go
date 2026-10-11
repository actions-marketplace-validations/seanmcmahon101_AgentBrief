package detect

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("docker", detectDocker) }

const maxComposeServices = 12

func detectDocker(r *scan.Repo, f *Facts) {
	for _, p := range filesNamed(r, "compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml") {
		if depth(dirOf(p)) > 2 {
			continue
		}
		if note, line := composeServices(r.ReadString(p)); note != "" {
			f.AddNote(model.Note{Text: note, Source: at(p, line)})
		}
	}
}

// composeServices summarises service names and images only; environment,
// ports, volumes and anything else that may hold secrets is never read.
func composeServices(content string) (string, int) {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(content), &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", 0
	}
	svcs := yamlKey(doc.Content[0], "services")
	if svcs == nil || svcs.Kind != yaml.MappingNode {
		return "", 0
	}
	var parts []string
	for i := 0; i+1 < len(svcs.Content); i += 2 {
		part := svcs.Content[i].Value
		if img := yamlKey(svcs.Content[i+1], "image"); img != nil && img.Kind == yaml.ScalarNode && !strings.ContainsAny(img.Value, "@ ") {
			part += fmt.Sprintf(" (%s)", img.Value)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", 0
	}
	line := 1
	for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
		if doc.Content[0].Content[i].Value == "services" {
			line = doc.Content[0].Content[i].Line
		}
	}
	return "docker compose services: " + strings.Join(capList(parts, maxComposeServices), ", "), line
}
