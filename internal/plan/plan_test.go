package plan

import (
	"bytes"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

func TestTruncatedScanCannotClaimCompleteMapping(t *testing.T) {
	repo := &model.Repo{Stats: model.ScanStats{Truncated: true}, Checks: []model.ValidationCheck{
		{ID: "test", Paths: []model.PathSelector{{Pattern: "**"}}},
	}}
	p, err := Build(repo, []string{"src/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	if p.MappingComplete || !p.ScanTruncated || len(p.UnmatchedPaths) != 0 || p.Executed || p.Coverage != "unknown" {
		t.Fatal(p)
	}
	if err := p.Write(failingWriter{}); err == nil {
		t.Fatal("ignored write failure")
	}
}

func TestMarkdownFencesUntrustedFilename(t *testing.T) {
	p := Plan{UnmatchedPaths: []string{"docs/`untrusted`.md"}}
	var out bytes.Buffer
	if err := p.Write(&out); err != nil || !bytes.Contains(out.Bytes(), []byte("`` docs/`untrusted`.md ``")) {
		t.Fatal(out.String(), err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, bytes.ErrTooLarge }

func TestMatchingBudgetRefusesExcessWorkBeforeMatching(t *testing.T) {
	b := matchBudget{remaining: 13, depths: map[string]int{}}
	if _, err := b.match("**/*.go", "a/b/c.go"); err == nil {
		t.Fatal("accepted a comparison exceeding the work budget")
	}
	if b.remaining != 13 {
		t.Fatal("failed budget check consumed work")
	}
	if matched, err := b.match("**", "a/b/c.go"); err != nil || !matched || b.remaining != 0 {
		t.Fatal(matched, err, b.remaining)
	}
	if _, err := b.match("*", "a"); err == nil {
		t.Fatal("accepted matching after budget exhaustion")
	}
}

func TestRepeatedLongNamesCannotExpandPlanWithoutBound(t *testing.T) {
	repo := &model.Repo{}
	for i := 0; i < 8; i++ {
		repo.Checks = append(repo.Checks, model.ValidationCheck{
			ID: strings.Repeat("x", i+1), Paths: []model.PathSelector{{Pattern: "**"}},
		})
	}
	p, err := Build(repo, []string{strings.Repeat("x", 1<<20)})
	if err == nil || p.Schema != "" {
		t.Fatal("returned a partial or unbounded plan", err)
	}
	// Rendering a backtick-heavy name is linear in its length.
	if got := code(strings.Repeat("`", 10000)); len(got) != 30004 {
		t.Fatal(len(got))
	}
}
