package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/stretchr/testify/require"
)

func TestBoxDrawsTitleMetaLegendAndExactWidth(t *testing.T) {
	view := ansi.Strip(Box{Title: "Escolher modelo", Meta: "Ollama · 7 de 8", Legend: "Enter usar · Esc fechar", Width: 60, PadX: 2, PadY: 1}.Render("linha\numa linha longa demais " + strings.Repeat("x", 80)))
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 6)
	for _, line := range lines {
		require.Equal(t, 60, ansi.StringWidth(line), line)
	}
	require.True(t, strings.HasPrefix(lines[0], " ▁ Escolher modelo "))
	require.True(t, strings.HasSuffix(lines[0], " Ollama · 7 de 8 ▁ "))
	require.True(t, strings.HasPrefix(lines[5], " ▔ Enter usar · Esc fechar "))
	require.Contains(t, lines[3], "…")
}

func TestBoxNeverWidensANarrowFrame(t *testing.T) {
	for _, line := range strings.Split(ansi.Strip(Box{Title: "Título bem longo", Meta: "meta", Legend: "legenda", Width: 8}.Render("x")), "\n") {
		require.Equal(t, 8, ansi.StringWidth(line), line)
	}
}

func TestSpreadAndMeter(t *testing.T) {
	row := ansi.Strip(Spread("esquerda", "direita", 30, P.Ground))
	require.Equal(t, 30, ansi.StringWidth(row))
	require.True(t, strings.HasSuffix(row, "direita"))
	require.Equal(t, "██░░░", ansi.Strip(Meter(0.45, P.Info, P.Ground)))
	require.Equal(t, "█████", ansi.Strip(Meter(2, P.Info, P.Ground)))
}

func TestListSearchRanksTitleMatchesFirst(t *testing.T) {
	l := List{Items: []Item{{Title: "Planejar", Desc: "prepara um plano"}, {Title: "Plano", Desc: "mostrar"}}}
	l.SetQuery("plano")
	require.Equal(t, "Plano", l.Visible()[0].Title)
	require.Len(t, l.Visible(), 2)
}

func TestKeyRowPushesTheLastPairRight(t *testing.T) {
	rows := KeyRow("Enter usar · Tab provedor · Esc fechar", 50, P.Surface)
	require.Len(t, rows, 2)
	row := ansi.Strip(rows[1])
	require.Equal(t, 50, ansi.StringWidth(row))
	require.True(t, strings.HasPrefix(row, "Enter usar   Tab provedor"))
	require.True(t, strings.HasSuffix(row, "Esc fechar"))
}

func TestThinEdgesHugTheFillAndButtonsKeepWidths(t *testing.T) {
	box := strings.Split(ansi.Strip(Box{Width: 10}.Render("x")), "\n")
	require.Equal(t, " ▁▁▁▁▁▁▁▁ ", box[0])
	require.Equal(t, "▕x       ▏", box[1])
	require.Equal(t, " ▔▔▔▔▔▔▔▔ ", box[2])
	buttons := OutlineButtons([]string{"Editar", "Testar"}, P.Border, P.Text, P.Card)
	require.Equal(t, "▕ Editar ▏ ▕ Testar ▏", ansi.Strip(buttons[1]))
	require.Equal(t, ansi.StringWidth(ansi.Strip(buttons[1])), ansi.StringWidth(ansi.Strip(buttons[0])))
}

func TestRecentCommandsKeepThreeNewestFirst(t *testing.T) {
	m := New(nil)
	for _, title := range []string{"A", "B", "C", "B", "D"} {
		m.remember(title)
	}
	require.Equal(t, []string{"D", "B", "C"}, m.recent)
}

func TestMentionInsertsTheChosenFile(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.composer.SetValue("veja @mod")
	m.files, m.filesAt = []string{"cmd/root.go", "internal/ui/model.go"}, time.Now()
	m.findMention()
	if len(m.mention) != 1 {
		t.Fatalf("mention = %v", m.mention)
	}
	m.mentionKey("tab")
	if got := m.composer.Value(); got != "veja @internal/ui/model.go " {
		t.Fatalf("composer = %q", got)
	}
	if m.findMention(); m.mention != nil {
		t.Fatalf("a finished mention should close the list, got %v", m.mention)
	}
}

func TestConversationArrowsWalkThePreviewButtons(t *testing.T) {
	d := &sessionsDialog{listDialog: &listDialog{}}
	press := func(code rune) { d.keys(nil, tea.KeyPressMsg{Code: code}) }
	press(tea.KeyRight)
	press(tea.KeyRight)
	if !d.acting || d.action != 1 {
		t.Fatalf("acting=%v action=%d, want Rename", d.acting, d.action)
	}
	press(tea.KeyLeft)
	press(tea.KeyLeft)
	if d.acting {
		t.Fatal("← from the first button should return to the list")
	}
}

func TestMentionSearchesTheWorkspaceWhenTheListingStoppedShort(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.files, m.filesAt = make([]string, fileutil.WorkspaceLimit), time.Now()
	for i := range m.files {
		m.files[i] = fmt.Sprintf("pasta/arquivo%d.go", i)
	}
	m.composer.SetValue("veja @alvo")
	if m.findMention() == nil {
		t.Fatal("a full listing should schedule a search for the query")
	}
	if len(m.mention) != 0 {
		t.Fatalf("mention = %v before the search", m.mention)
	}
	m.Update(foundMsg{"alvo", []string{"fundo/alvo.go"}})
	if len(m.mention) != 1 || m.mention[0] != "fundo/alvo.go" {
		t.Fatalf("mention = %v after the search", m.mention)
	}
	if m.findMention() != nil {
		t.Fatal("a query already searched should not be searched again")
	}
}

func TestModelPickerCtrlACyclesTheRoleItChoosesFor(t *testing.T) {
	m := &Model{}
	m.openModels()
	picker := m.dialog.(*modelPicker)
	require.Equal(t, config.AgentCoder, picker.role)
	picker.keys(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	require.Equal(t, config.AgentTask, picker.role)
	require.Contains(t, picker.title, roleName(config.AgentTask))
	for range 3 {
		picker.keys(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	}
	require.Equal(t, config.AgentCoder, picker.role)
	require.NotContains(t, picker.title, "·")
}
