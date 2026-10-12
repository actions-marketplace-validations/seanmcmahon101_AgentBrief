package brief

import (
	"sort"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

func (b *Brief) applyWorkflows(repo *model.Repo, scope, pdir string) {
	var matches []model.Workflow
	for _, w := range repo.Workflows {
		if within(scope, w.Scope) {
			matches = append(matches, w)
		}
	}
	if len(matches) == 0 {
		return
	}
	sort.Slice(matches, func(i, j int) bool { return len(matches[i].Scope) < len(matches[j].Scope) })
	for _, w := range matches {
		if len(w.Scope) > len(b.Scope) || b.Scope == "." {
			b.Scope = w.Scope
		}
		for _, c := range w.Commands {
			index := -1
			for i, existing := range b.Commands {
				if existing.Kind == c.Kind {
					index = i
					break
				}
			}
			// Root policy does not replace a nested package's own command. A workflow
			// explicitly scoped to that package or a deeper subtree can replace it.
			if pdir != "." && w.Scope != pdir && within(pdir, w.Scope) && index >= 0 && within(b.Commands[index].Dir, pdir) {
				continue
			}
			src := c.Source
			cmd := Command{Kind: c.Kind, Run: c.Run, Dir: c.Dir,
				Label:    ".agentbrief.yaml, maintainer-selected; execution unverified",
				Evidence: c.Evidence, Preferred: true, Source: &src}
			if index >= 0 {
				b.Commands[index] = cmd
			} else {
				b.Commands = append(b.Commands, cmd)
			}
		}
	}
	order := append(append([]string(nil), kindOrder...), model.KindConfigure, model.KindRun)
	rank := map[string]int{}
	for i, kind := range order {
		rank[kind] = i
	}
	sort.SliceStable(b.Commands, func(i, j int) bool { return rank[b.Commands[i].Kind] < rank[b.Commands[j].Kind] })
	b.DoNotEdit = buildDoNotEdit(repo, b.Scope)
	b.InstructionFiles = buildInstructionList(repo, b.Scope)
}
