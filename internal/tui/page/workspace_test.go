package page

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

type workspaceAgent struct {
	agent.Service
	items   []agent.QueuedInstruction
	taken   int
	compact int
}

func (*workspaceAgent) IsBusy() bool                                          { return false }
func (*workspaceAgent) IsSessionBusy(string) bool                             { return false }
func (a *workspaceAgent) QueuedInstructions(string) []agent.QueuedInstruction { return a.items }
func (a *workspaceAgent) TakeInstruction(_, id string) (agent.QueuedInstruction, error) {
	for i, item := range a.items {
		if item.ID == id {
			a.items = append(a.items[:i], a.items[i+1:]...)
			a.taken++
			return item, nil
		}
	}
	return agent.QueuedInstruction{}, errors.New("already consumed")
}
func (*workspaceAgent) ContextStatus(context.Context, string) (agent.ContextStatus, error) {
	return agent.ContextStatus{ContextWindow: 16384, EstimatedInputTokens: 2000, ReservedOutputTokens: 2048, SafetyTokens: 512, Fits: true, Method: "estimated"}, nil
}
func (a *workspaceAgent) CompactContext(ctx context.Context, s string) (agent.ContextStatus, error) {
	a.compact++
	return a.ContextStatus(ctx, s)
}
func workspacePage(t *testing.T) (*chatPage, *workspaceAgent) {
	t.Helper()
	a := &workspaceAgent{}
	p := NewChatPage(&app.App{CoderAgent: a}).(*chatPage)
	p.session = session.Session{ID: "s"}
	p.SetSize(90, 30)
	return p, a
}
func TestQueueEditorDoesNotRemoveInstructionUntilDraftCanBeReplaced(t *testing.T) {
	p, a := workspacePage(t)
	a.items = []agent.QueuedInstruction{{ID: "queued", Content: "next instruction", Attachments: []message.Attachment{{FileName: "note.txt", Content: []byte("attached")}}}}
	p.editor.Update(chat.RestoreDraftMsg{Text: "current draft"})
	p.openQueue()
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Zero(t, a.taken)
	require.Contains(t, p.workspace.note, config.Text("rascunho", "draft"))
	// Removing is explicit and leaves the current draft intact.
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyDelete})
	require.Equal(t, 1, a.taken)
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "current draft", draft)
}
func TestQueueEditRestoresAttachmentsAndConsumptionRaceReportsError(t *testing.T) {
	p, a := workspacePage(t)
	a.items = []agent.QueuedInstruction{{ID: "q", Content: "edit this", Attachments: []message.Attachment{{FileName: "note.txt", Content: []byte("data")}}}}
	p.openQueue()
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEnter})
	draft, attachments := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "edit this", draft)
	require.Equal(t, "data", string(attachments[0].Content))
	require.False(t, p.WorkspaceFocused())
	p, a = workspacePage(t)
	a.items = []agent.QueuedInstruction{{ID: "q", Content: "gone"}}
	p.openQueue()
	a.items = nil
	require.NotNil(t, p.workspaceKey(tea.KeyMsg{Type: tea.KeyEnter}))
	require.True(t, p.WorkspaceFocused())
	require.Empty(t, p.workspace.actions)
}
func TestContextPanelIgnoresStaleResultsAndPreventsRepeatedOperations(t *testing.T) {
	p, a := workspacePage(t)
	cmd := p.openContext(true)
	require.True(t, p.workspace.loading)
	require.Nil(t, p.workspaceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}))
	result := cmd().(WorkspaceResultMsg)
	require.Equal(t, 1, a.compact)
	p.workspaceResult(result)
	require.Contains(t, p.workspace.body, "16384")
	cmd = p.openContext(false)
	p.closeWorkspace()
	p.workspaceResult(cmd().(WorkspaceResultMsg))
	require.False(t, p.WorkspaceFocused())
	require.Empty(t, p.workspace.body)
}
func TestClipboardFailureKeepsSelectableTextAndRecoveryPersists(t *testing.T) {
	p, _ := workspacePage(t)
	p.openResponseActions("answer\n```go\nfmt.Println(1)\n```\n")
	require.Len(t, p.workspace.actions, 3)
	cmd := p.workspaceChoose()
	request := cmd().(util.CopyTextMsg)
	p.workspaceResult(request.Result(false, errors.New("unavailable")).(WorkspaceResultMsg))
	require.Contains(t, p.workspace.note, config.Text("Selecionar texto", "Select text"))
	p.workspace.selected = 1
	p.workspaceChoose()
	require.Equal(t, "text", p.workspace.kind)
	require.Contains(t, p.workspace.body, "answer")
	p.closeWorkspace()
	p.Update(AgentOutcomeMsg{SessionID: "s", Error: errors.New("connection failed")})
	require.Contains(t, p.composerPanel(), "/retry")
	p.Update(AgentOutcomeMsg{SessionID: "s", Done: true})
	require.Empty(t, p.composerPanel(), "resolved errors no longer occupy the composer")
	require.NotNil(t, p.recovery.err, "technical evidence remains available with /error")
	// The plan/recovery panel is above the editor, below the conversation.
	require.Equal(t, "conversation\ncomposer", p.insertComposerPanel("conversation\ncomposer"))
}

func TestRetryProducesOneFailureAcknowledgement(t *testing.T) {
	for _, previous := range []string{"", "original prompt"} {
		p, _ := workspacePage(t)
		probe := &submittingAgent{err: errors.New("queue full")}
		p.app.CoderAgent = probe
		p.lastRequest = chat.SendMsg{Text: previous}
		msgs := flattenCommands(p.sendMessage("/retry", nil))
		acknowledgements := 0
		for _, msg := range msgs {
			if ack, ok := msg.(chat.SubmissionResultMsg); ok {
				acknowledgements++
				require.Equal(t, "/retry", ack.Text)
				require.Error(t, ack.Error)
			}
		}
		require.Equal(t, 1, acknowledgements)
		require.Equal(t, previous, p.lastRequest.Text)
	}
}
func TestQueueEditPreservesAttachmentOnlyDraft(t *testing.T) {
	p, a := workspacePage(t)
	a.items = []agent.QueuedInstruction{{ID: "q", Content: "pending"}}
	p.editor.Update(chat.RestoreDraftMsg{Attachments: []message.Attachment{{FileName: "unsent", Content: []byte("draft")}}})
	p.openQueue()
	p.takeQueue(true)
	require.Zero(t, a.taken)
	require.Len(t, a.items, 1)
	_, attachments := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "draft", string(attachments[0].Content))
}

func TestPanelRenderingStripsTerminalControls(t *testing.T) {
	p, _ := workspacePage(t)
	p.showWorkspace("file", "title\x1b]52;c;aW5qZWN0\a", "body\n\x1b[2Jsafe\x00")
	require.Equal(t, "body\nsafe", p.workspace.body)
	require.NotContains(t, p.workspace.title, "\x1b")
	result := WorkspaceResultMsg{session: "s", seq: p.workspace.seq, body: "new\x1b]52;c;secret\a\ntext"}
	p.workspaceResult(result)
	require.Equal(t, "new\ntext", p.workspace.body)
}
func TestValidationPanelDistinguishesRecordedPassFromCurrentApproval(t *testing.T) {
	for _, revision := range []int{1, 2} {
		raw := fmt.Sprintf(`{"task_revision":%d,"task_digest":"digest","configured":{"test":"go test"},"receipts":[{"name":"test","passed":true,"revision":1,"digest":"digest","command_hash":"%x"}]}`, revision, sha256.Sum256([]byte("go test")))
		text, err := formatValidationReport(raw)
		require.NoError(t, err)
		require.Contains(t, text, fmt.Sprintf(config.Text("aprovação atual: %t", "current approval: %t"), revision == 1))
		require.Contains(t, text, config.Text("resultado registrado: true", "recorded result: true"))
	}
}
