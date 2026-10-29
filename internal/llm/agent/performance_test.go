package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type titleProbe struct {
	provider.Provider
	model models.Model
	calls int
}

func (p *titleProbe) Model() models.Model { return p.model }
func (p *titleProbe) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	p.calls++
	return &provider.ProviderResponse{Content: "Generated title"}, nil
}

type titleSessions struct {
	memorySessions
	saved session.Session
}

func (s *titleSessions) Save(_ context.Context, value session.Session) (session.Session, error) {
	s.saved = value
	return value, nil
}
func TestLocalTitleAvoidsSameModelInferenceAndBoundsText(t *testing.T) {
	p := &titleProbe{model: models.Model{ID: "local.llama", Provider: models.ProviderLocal}}
	s := &titleSessions{}
	a := &agent{provider: p, titleProvider: p, sessions: s}
	require.NoError(t, a.generateTitle(context.Background(), "s", "  Olá\n"+strings.Repeat("mundo ", 40)))
	require.Zero(t, p.calls)
	require.Len(t, []rune(s.saved.Title), 72)
	require.True(t, strings.HasPrefix(s.saved.Title, "Olá mundo"))
	u, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 1, u.Performance.LocalTitles)
}

func TestSeparatelySelectedTitleModelStillUsesProvider(t *testing.T) {
	main := &titleProbe{model: models.Model{ID: "local.llama", Provider: models.ProviderLocal}}
	title := &titleProbe{model: models.Model{ID: "local.title", Provider: models.ProviderLocal, ContextWindow: 16384, DefaultMaxTokens: 128}}
	s := &titleSessions{}
	a := &agent{provider: main, titleProvider: title, sessions: s}
	require.NoError(t, a.generateTitle(context.Background(), "s", "Explique o projeto"))
	require.Zero(t, main.calls)
	require.Equal(t, 1, title.calls)
	require.Equal(t, "Generated title", s.saved.Title)
}

func TestPerformanceWindowAndDetachedSnapshots(t *testing.T) {
	a := &agent{}
	for i := 1; i <= 100; i++ {
		a.observePhase("s", "first_token", time.Duration(i)*time.Millisecond)
	}
	a.observePreparation("s", tools.ViewToolName, false, 12)
	a.observePreparation("s", tools.ViewToolName, true, 8)
	u, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, 64, u.Performance.FirstTokenSamples)
	require.EqualValues(t, 100, u.Performance.FirstTokenLastMS)
	require.EqualValues(t, 68, u.Performance.FirstTokenP50MS)
	require.EqualValues(t, 97, u.Performance.FirstTokenP95MS)
	require.EqualValues(t, 1, u.Performance.PreparationSourceReads)
	require.EqualValues(t, 1, u.Performance.PreparationReuses)
	require.EqualValues(t, 20, u.Performance.PreparationBytes)
	u.Performance.PreparationBytes = -1
	v, err := a.Usage(context.Background(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 20, v.Performance.PreparationBytes)
	empty, err := a.Usage(context.Background(), "other")
	require.NoError(t, err)
	require.Nil(t, empty.Performance)
}
