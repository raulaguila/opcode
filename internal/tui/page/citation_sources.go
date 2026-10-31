package page

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
)

type citedSource struct {
	path        string
	ranges      []string
	starts      []int
	unavailable bool
}

var responseFilePattern = regexp.MustCompile("`([^`\\n]+)`")

// These are references present in the response, not proof of validated reads.
func responseSources(text string) []citedSource {
	var sources []citedSource
	groups := map[string]int{}
	seen := map[string]bool{}
	// Bare file mentions remain navigable, without inventing a cited line.
	for _, match := range responseFilePattern.FindAllStringSubmatch(text, 64) {
		path := match[1]
		if strings.ContainsAny(path, ":#\n\x00") || strings.HasPrefix(path, "//") || sourceUnavailable(path) {
			continue
		}
		appendCitedSource(&sources, groups, seen, path, "", 1)
	}
	for _, ref := range agent.SourceCitations(text) {
		if ref.StartLine < 1 || ref.EndLine < ref.StartLine || strings.HasPrefix(ref.Path, "//") {
			continue
		}
		span := strconv.Itoa(ref.StartLine)
		if ref.EndLine != ref.StartLine {
			span += "-" + strconv.Itoa(ref.EndLine)
		}
		appendCitedSource(&sources, groups, seen, ref.Path, span, ref.StartLine)
	}
	return sources
}

func appendCitedSource(sources *[]citedSource, groups map[string]int, seen map[string]bool, path, span string, start int) {
	path = filepath.Clean(path)
	key := path + ":" + span
	if seen[key] {
		return
	}
	seen[key] = true
	group, exists := groups[path]
	if !exists {
		group = len(*sources)
		groups[path] = group
		*sources = append(*sources, citedSource{path: path, unavailable: sourceUnavailable(path)})
	}
	(*sources)[group].ranges = append((*sources)[group].ranges, span)
	(*sources)[group].starts = append((*sources)[group].starts, start)
}

func sourceReferences(sources []citedSource) string {
	var lines []string
	for _, source := range sources {
		for _, span := range source.ranges {
			reference := source.path
			if span != "" {
				reference += ":" + span
			}
			lines = append(lines, reference)
		}
	}
	return strings.Join(lines, "\n")
}

func appendCitationActions(p *chatPage, text, state string) {
	sources := responseSources(text)
	if len(sources) == 0 {
		return
	}
	p.workspace.actions = append(p.workspace.actions, workspaceAction{label: fmt.Sprintf(config.Text("Fontes citadas · %d arquivo(s)", "Cited sources · %d file(s)"), len(sources)), kind: "sources", text: text, citationStatus: state})
}

func (p *chatPage) openCitationSources(text string, states ...string) tea.Cmd {
	sources := responseSources(text)
	state := citationState(states)
	p.showWorkspace("sources", config.Text("Fontes citadas", "Cited sources"), sourceEvidenceLabel(state))
	basenames := map[string]int{}
	for _, source := range sources {
		basenames[filepath.Base(source.path)]++
	}
	for _, source := range sources {
		label := filepath.Base(source.path)
		if basenames[label] > 1 {
			label = source.path
		}
		label += fmt.Sprintf(config.Text(" · %d referência(s)", " · %d reference(s)"), len(source.ranges))
		if source.unavailable {
			label += config.Text(" · indisponível", " · unavailable")
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: label, kind: "source", path: source.path, text: text, citationStatus: state})
	}
	p.workspace.actions = append(p.workspace.actions, workspaceAction{label: config.Text("Copiar referências", "Copy references"), kind: "copy", text: sourceReferences(sources)})
	return nil
}

func (p *chatPage) openCitationSource(text, path string, states ...string) tea.Cmd {
	for _, source := range responseSources(text) {
		if source.path != path {
			continue
		}
		refs := sourceReferences([]citedSource{source})
		p.showWorkspace("source", config.Text("Fonte · ", "Source · ")+filepath.Base(path), source.path+"\n\n"+refs+"\n\n"+sourceEvidenceLabel(citationState(states)))
		if source.unavailable {
			p.workspace.body += config.Text("\nArquivo indisponível; a referência histórica foi preservada.", "\nFile unavailable; the historical reference was preserved.")
		}
		for i, span := range source.ranges {
			if source.unavailable {
				continue
			}
			label := config.Text("Abrir arquivo", "Open file")
			if span != "" {
				label = config.Text("Abrir intervalo ", "Open range ") + span
			}
			p.workspace.actions = append(p.workspace.actions, workspaceAction{label: label, kind: "file", path: source.path, line: source.starts[i]})
		}
		p.workspace.actions = append(p.workspace.actions, workspaceAction{label: config.Text("Copiar referências", "Copy references"), kind: "copy", text: refs})
		return nil
	}
	return nil
}

func sourceUnavailable(path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.WorkingDirectory(), path)
	}
	info, err := os.Lstat(path)
	return err != nil || !info.Mode().IsRegular()
}

func citationState(states []string) string {
	if len(states) > 0 {
		return states[0]
	}
	return ""
}

func sourceEvidenceLabel(state string) string {
	label := config.Text("Referências presentes na resposta; sem validação registrada para este texto.", "References present in the response; no recorded validation for this text.")
	switch state {
	case message.CitationValid:
		label = config.Text("Referências verificadas ao gerar a resposta: origem, intervalo lido e hash então atual.", "References checked when the answer was generated: origin, supplied line range and then-current hash.")
	case message.CitationInvalid:
		label = config.Text("Referências não validadas na resposta.", "References not validated in this answer.")
	case message.CitationPending, message.CitationRevising:
		label = config.Text("A validação das referências ainda não foi concluída.", "Reference validation has not completed.")
	}
	return label + config.Text("\nIsso não verifica a interpretação. Arquivos não foram revalidados agora; a prévia mantém as permissões normais.", "\nThis does not verify interpretation. Files have not been revalidated now; preview keeps normal permissions.")
}
