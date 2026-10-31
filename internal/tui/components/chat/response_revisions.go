package chat

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

type responseRevision struct {
	original, primary int
	attempts          []int
	details           []int
}
type responseRevisionPlan struct {
	roots  map[int]*responseRevision
	hidden map[int]int
}

// Links are local to a human turn; public tool narration is never a revision.
func planResponseRevisions(items []message.Message) responseRevisionPlan {
	p := responseRevisionPlan{}
	for i, item := range items {
		if item.RevisionOf() == "" || !publicResponse(item) || len(item.ToolCalls()) > 0 {
			continue
		}
		root := revisionOriginal(items, i)
		if root < 0 {
			continue
		}
		if p.roots == nil {
			p.roots = map[int]*responseRevision{}
			p.hidden = map[int]int{}
		}
		r := p.roots[root]
		if r == nil {
			r = &responseRevision{original: root, primary: root}
			p.roots[root] = r
		}
		r.attempts = append(r.attempts, i)
		p.hidden[i] = root
		if item.CitationValidation() == message.CitationValid && item.FinishReason() == message.FinishReasonEndTurn {
			r.primary = i
		}
	}
	p.foldRevisionDetails(items)
	return p
}

func (p responseRevisionPlan) foldRevisionDetails(items []message.Message) {
	for root, revision := range p.roots {
		end := revision.attempts[len(revision.attempts)-1]
		if !p.directRevision(items, root, end) {
			continue
		}
		for i := root + 1; i < end; i++ {
			if processEvent(items[i]) && !publicResponse(items[i]) {
				revision.details = append(revision.details, i)
				p.hidden[i] = root
			}
		}
	}
}

func (p responseRevisionPlan) directRevision(items []message.Message, root, end int) bool {
	for i := root + 1; i < end; i++ {
		if publicResponse(items[i]) {
			if owner, ok := p.hidden[i]; !ok || owner != root {
				return false
			}
		}
	}
	return true
}

func revisionOriginal(items []message.Message, index int) int {
	item := items[index]
	for i := index - 1; i >= 0; i-- {
		root := items[i]
		if root.Role == message.User && !root.IsApplication() {
			break
		}
		if root.ID == item.RevisionOf() && root.SessionID == item.SessionID && publicResponse(root) && len(root.ToolCalls()) == 0 && root.RevisionOf() == "" {
			return i
		}
	}
	return -1
}

func (p responseRevisionPlan) latest(items []message.Message) int {
	last := lastPublicIndex(items)
	if root, ok := p.hidden[last]; ok {
		return root
	}
	return last
}

func (r *turnRenderer) renderRevision(items []message.Message, revision *responseRevision, latest bool) {
	r.renderFinal(items[revision.primary], revision.primary, latest)
	root := items[revision.original]
	label := config.Text("Revisão · Ver tentativa de correção", "Revision · View correction attempt")
	if revision.primary != revision.original {
		label = config.Text("Revisão · Ver versão anterior", "Revision · View previous version")
	}
	expanded := r.renderRevisionToggle(root.ID, label)
	if !expanded {
		return
	}
	r.appendText(root.ID+":revision-reason", styles.HintOn(r.theme.Background()).Render(config.Text("Motivo: as referências da resposta precisaram de verificação.", "Reason: the response references needed verification.")))
	if revision.primary != revision.original {
		r.renderFinal(root, revision.original, false)
	}
	for _, index := range revision.attempts {
		if index != revision.primary {
			r.renderFinal(items[index], index, false)
		}
	}
	for _, index := range revision.details {
		r.renderProcessMessage(items[index], index)
	}
}

func (r *turnRenderer) renderRevisionToggle(id, label string) bool {
	expanded := r.messages.expanded[revisionKey(id)]
	mark := "›"
	if expanded {
		mark = "⌄"
	}
	start := r.position
	r.appendText(id+":versions", styles.HintOn(r.theme.Background()).Render(ansi.Truncate(mark+" "+label+" · Alt+V", r.messages.width, "…")))
	r.hits = append(r.hits, transcriptHit{start: start, end: start + 1, kind: "revision", id: id, expanded: expanded})
	return expanded
}

func revisionKey(id string) string { return id + "\x00revision" }

func (m *messagesCmp) toggleRevision(id string) {
	m.ClearSelection()
	if m.expanded == nil {
		m.expanded = map[string]bool{}
		m.toggled = map[string]bool{}
	}
	key := revisionKey(id)
	m.expanded[key] = !m.expanded[key]
	m.renderCache.versions[m.renderCache.messageTurn[id]]++
	m.renderCache.dirty = true
	offset := m.viewport.YOffset
	m.renderView()
	m.viewport.SetYOffset(offset)
}

func (m *messagesCmp) toggleLatestRevision() bool {
	for i := len(m.hits) - 1; i >= 0; i-- {
		if m.hits[i].kind == "revision" {
			m.toggleRevision(m.hits[i].id)
			return true
		}
	}
	return false
}

func latestRevisionResponse(groups []turnGroup) string {
	item := latestRevisionMessage(groups)
	return item.Content().String()
}

func (m *messagesCmp) ResponseCitationStatus(text string) string {
	item := latestRevisionMessage(m.groups())
	if item.Content().String() == text {
		return item.CitationValidation()
	}
	return ""
}

func latestRevisionMessage(groups []turnGroup) message.Message {
	for i := len(groups) - 1; i >= 0; i-- {
		items := groups[i].messages
		p := planResponseRevisions(items)
		for index := len(items) - 1; index >= 0; index-- {
			if root, ok := p.hidden[index]; ok {
				return items[p.roots[root].primary]
			}
			if publicResponse(items[index]) {
				return items[index]
			}
		}
	}
	return message.Message{}
}

func legacyPreviousVersion(items []message.Message, index int) bool {
	if items[index].CitationValidation() != message.CitationSuperseded {
		return false
	}
	for _, item := range items[index+1:] {
		if publicResponse(item) && item.CitationValidation() == message.CitationValid && item.FinishReason() == message.FinishReasonEndTurn {
			return true
		}
	}
	return false
}

func (r *turnRenderer) renderRevisingState(item message.Message) {
	if item.CitationValidation() != message.CitationRevising {
		return
	}
	state := message.CitationRevising
	if !r.active {
		state = message.CitationInvalid
	}
	r.appendText(item.ID+":revision-state", r.base.Foreground(r.theme.TextMuted()).Render(citationStatusLabel(state)))
}
