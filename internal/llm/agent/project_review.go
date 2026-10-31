package agent

import (
	"context"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// Reuse the bounded index and native read path: two implementations, two tests.
func (a *agent) bootstrapReview(ctx context.Context, session, question, query string, history []message.Message, run *generationRun, view, index tools.BaseTool) ([]message.Message, error) {
	if len(tools.ProjectSourcePaths(question)) > 0 {
		var err error
		history, err = a.bootstrapBasic(ctx, session, question, history, run, view)
		if err != nil {
			return history, err
		}
	}
	b := evidenceBootstrap{agent: a, ctx: ctx, session: session, run: run, history: history, view: view, review: true}
	read, err := b.readIndex(query, index, "overview")
	if err != nil {
		return b.history, err
	}
	if !read {
		b.history, err = a.bootstrapBasic(ctx, session, question, b.history, run, view)
		if err != nil {
			return b.history, err
		}
	}
	_, err = b.readIndex(query, index, "tests")
	return b.history, err
}
