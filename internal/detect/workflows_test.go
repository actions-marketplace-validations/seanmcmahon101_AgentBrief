package detect

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func TestWorkflowConfigIsAtomicAndRedacted(t *testing.T) {
	config := "version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: 'TOKEN=super-secret-value npm test'\ngenerated: [skills/**]\n"
	repo := Run(scan.FromMap(map[string]string{WorkflowFile: config}))
	if len(repo.Workflows) != 1 || len(repo.Generated) != 1 || len(repo.ConfigFindings) != 0 {
		t.Fatalf("%+v", repo)
	}
	data, _ := json.Marshal(repo)
	if strings.Contains(string(data), "super-secret-value") {
		t.Fatal(string(data))
	}
	repo = Run(scan.FromMap(map[string]string{WorkflowFile: config + "unknown: value\n"}))
	if len(repo.Workflows) != 0 || len(repo.Generated) != 0 || len(repo.ConfigFindings) != 1 {
		t.Fatalf("%+v", repo)
	}
}

func TestWorkflowOutputDoesNotDependOnYAMLOrder(t *testing.T) {
	a := "version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: make test\n      - kind: build\n        run: make build\n"
	b := "version: 1\nworkflows:\n  - commands:\n      - kind: build\n        run: make build\n      - kind: test\n        run: make test\n"
	one, two := Run(scan.FromMap(map[string]string{WorkflowFile: a})), Run(scan.FromMap(map[string]string{WorkflowFile: b}))
	for i := range one.Workflows[0].Commands {
		c, d := one.Workflows[0].Commands[i], two.Workflows[0].Commands[i]
		if c.Kind != d.Kind || c.Run != d.Run {
			t.Fatalf("%+v %+v", c, d)
		}
	}
}
