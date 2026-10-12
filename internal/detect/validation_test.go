package detect

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func TestValidationPolicyIsAtomicAndRedacted(t *testing.T) {
	config := `version: 1
workflows:
  - commands:
      - kind: test
        run: go test ./...
checks:
  - id: tests
    kind: test
    run: TOKEN=super-secret-value go test ./...
    paths: ["**/*.go"]
generated: [generated/**]
`
	repo := Run(scan.FromMap(map[string]string{WorkflowFile: config}))
	if len(repo.Checks) != 1 || len(repo.Checks[0].Paths) != 1 || len(repo.Workflows) != 1 ||
		len(repo.ConfigFindings) != 0 {
		t.Fatalf("%+v", repo)
	}
	data, _ := json.Marshal(repo)
	if strings.Contains(string(data), "super-secret-value") || repo.Checks[0].Paths[0].Source.Line == 0 {
		t.Fatal(string(data))
	}
	// A bad graph must not leave the earlier preferences or generated rules.
	repo = Run(scan.FromMap(map[string]string{WorkflowFile: strings.Replace(config,
		"    paths:", "    requires: [absent]\n    paths:", 1)}))
	if len(repo.Checks) != 0 || len(repo.Workflows) != 0 || len(repo.Generated) != 0 ||
		len(repo.ConfigFindings) != 1 {
		t.Fatalf("%+v", repo)
	}
}
