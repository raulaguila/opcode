package agent

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
)

// What the conversation shows of the report; the file holds all of it.
const (
	auditChatGroups    = 20
	auditChatLocations = 5
)

var auditWords = regexp.MustCompile("[`'\"]|\\d+")

func (r *auditRun) progress(ctx context.Context, text string) {
	r.reply.Parts = []message.ContentPart{message.TextContent{Text: text}}
	_ = r.agent.messages.Update(context.WithoutCancel(ctx), *r.reply)
}

func (r *auditRun) finish(ctx context.Context, events chan AgentEvent) {
	a, session := r.agent, r.session
	a.activeRequests.Delete(session + "-summarize")
	r.report.drop(auditDismissed(filepath.Join(auditDirectory(), "ignored.txt")))
	r.report.rank()
	if err := r.cache.save(); err != nil {
		logging.Warn("audit cache not saved", "error", err)
	}
	path := filepath.Join(auditDirectory(), "report.md")
	if err := writeAuditFile(path, []byte(r.report.render(false, ""))); err != nil {
		logging.Warn("audit report not saved", "error", err)
		path = ""
	}
	if data, err := json.MarshalIndent(r.report.findings, "", "  "); err == nil {
		_ = writeAuditFile(filepath.Join(auditDirectory(), "findings.json"), data)
	}
	if data, err := json.MarshalIndent(r.report.refused, "", "  "); err == nil {
		_ = writeAuditFile(filepath.Join(auditDirectory(), "refused.json"), data)
	}
	r.progress(ctx, r.report.render(true, path))
	r.reply.AddFinish(message.FinishReasonEndTurn)
	result := AgentEvent{Type: AgentEventTypeResponse, Message: *r.reply, SessionID: session, Done: true, Outcome: "completed"}
	if err := a.messages.Update(context.WithoutCancel(ctx), *r.reply); err != nil {
		result.Type, result.Error = AgentEventTypeError, err
	}
	if len(r.report.missed) > 0 || r.report.unreviewed > 0 {
		result.Outcome = "partial"
	}
	if ctx.Err() != nil {
		result.Error = ErrRequestCancelled
	}
	a.Publish(pubsub.CreatedEvent, result)
	events <- result
}

// auditDismissed reads the findings someone dismissed, one "path: code" a line,
// the way the report prints them, so a dismissed finding survives moved lines.
func auditDismissed(path string) map[string]bool {
	ignored := map[string]bool{}
	file, err := os.Open(path)
	if err != nil {
		return ignored
	}
	defer file.Close()
	for lines := bufio.NewScanner(file); lines.Scan(); {
		ignored[strings.TrimSpace(lines.Text())] = true
	}
	return ignored
}

func (f auditFinding) key() string {
	return f.Path + ": " + strings.Join(strings.Fields(f.Code), " ")
}

func (r *auditReport) drop(ignored map[string]bool) {
	r.findings = slices.DeleteFunc(r.findings, func(finding auditFinding) bool {
		if ignored[finding.key()] {
			r.ignored++
			return true
		}
		return false
	})
}

// rank sorts by severity, proof first, and leaves one finding per line,
// an analyzer's when there is one.
func (r *auditReport) rank() {
	slices.SortStableFunc(r.findings, func(a, b auditFinding) int {
		severities := []string{"high", "medium", "low"}
		return cmp.Or(cmp.Compare(slices.Index(severities, a.Severity), slices.Index(severities, b.Severity)),
			cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), -cmp.Compare(a.Source, b.Source))
	})
	r.findings = slices.CompactFunc(r.findings, func(a, b auditFinding) bool { return a.Path == b.Path && a.Line == b.Line })
}

type auditGroup struct {
	label    string
	findings []auditFinding
}

// groups joins what repeats: forty ignored errors are one thing to decide.
func (r *auditReport) groups(severity string) []auditGroup {
	var groups []auditGroup
	for _, finding := range r.findings {
		if finding.Severity != severity {
			continue
		}
		label := strings.ToLower(auditWords.ReplaceAllString(finding.Problem, ""))
		if linter := auditLinter.FindStringSubmatch(finding.Problem); linter != nil {
			label = linter[1]
		}
		index := slices.IndexFunc(groups, func(group auditGroup) bool { return group.label == label })
		if index < 0 {
			groups = append(groups, auditGroup{label: label})
			index = len(groups) - 1
		}
		groups[index].findings = append(groups[index].findings, finding)
	}
	slices.SortStableFunc(groups, func(a, b auditGroup) int { return cmp.Compare(len(b.findings), len(a.findings)) })
	return groups
}

func (f auditFinding) mark() string {
	mark := ""
	switch f.State {
	case auditProved:
		mark = config.Text("✓ provado por teste · ", "✓ proved by a test · ")
	case auditReviewed:
		mark = config.Text("revisado · ", "reviewed · ")
	}
	if f.Source != "" {
		mark += f.Source + " · "
	}
	return mark
}

func (r *auditReport) render(brief bool, file string) string {
	r.rank()
	slices.Sort(r.missed)
	r.missed = slices.Compact(r.missed)
	slices.Sort(r.read)
	read := slices.DeleteFunc(slices.Compact(slices.Clone(r.read)), func(path string) bool { return slices.Contains(r.missed, path) })
	var text strings.Builder
	fmt.Fprintf(&text, config.Text("## Auditoria\n\n%d partes, %d arquivos lidos, %d achados, %d descartados sem evidência, %d reprovados na revisão, em %s.\n",
		"## Audit\n\n%d parts, %d files read, %d findings, %d dropped without evidence, %d refuted in review, in %s.\n"),
		r.parts, len(read), len(r.findings), r.rejected, r.refuted, time.Since(r.started).Round(time.Second))
	if r.cached > 0 {
		fmt.Fprintf(&text, config.Text("%d respostas vieram do cache.\n", "%d replies came from the cache.\n"), r.cached)
	}
	if r.ignored > 0 {
		fmt.Fprintf(&text, config.Text("%d achados dispensados.\n", "%d findings dismissed.\n"), r.ignored)
	}
	if len(r.tools) > 0 {
		fmt.Fprintf(&text, config.Text("Analisadores: %s.\n", "Analyzers: %s.\n"), strings.Join(r.tools, ", "))
	}
	if file != "" {
		fmt.Fprintf(&text, config.Text("Relatório completo: `%s`\n", "Full report: `%s`\n"), file)
	}
	if r.checks != "" {
		fmt.Fprintf(&text, "\n### %s\n\n%s\n", config.Text("Verificações", "Checks"), strings.TrimSpace(r.checks))
	}
	headings := map[string]string{"high": config.Text("Alta", "High"), "medium": config.Text("Média", "Medium"), "low": config.Text("Baixa", "Low")}
	number, hidden := 0, 0
	for _, severity := range []string{"high", "medium", "low"} {
		groups := r.groups(severity)
		if len(groups) > 0 {
			fmt.Fprintf(&text, "\n### %s\n\n", headings[severity])
		}
		for g, group := range groups {
			if brief && severity != "high" && g >= auditChatGroups {
				hidden += len(group.findings)
				continue
			}
			number++
			if len(group.findings) == 1 {
				finding := group.findings[0]
				fmt.Fprintf(&text, "%d. %s`%s:%d`: %s\n   `%s`\n", number, finding.mark(), finding.Path, finding.Line, finding.Problem, finding.Code)
				continue
			}
			fmt.Fprintf(&text, "%d. %s (×%d)\n", number, group.findings[0].Problem, len(group.findings))
			for i, finding := range group.findings {
				if brief && i == auditChatLocations {
					fmt.Fprintf(&text, config.Text("   mais %d\n", "   %d more\n"), len(group.findings)-i)
					break
				}
				fmt.Fprintf(&text, "   - %s`%s:%d`\n", finding.mark(), finding.Path, finding.Line)
			}
		}
	}
	if len(r.findings) == 0 {
		text.WriteString(config.Text("\nNenhum achado com evidência.\n", "\nNo finding with evidence.\n"))
	}
	if hidden > 0 {
		fmt.Fprintf(&text, config.Text("\n%d achados de média e baixa severidade só no relatório completo.\n", "\n%d medium and low findings only in the full report.\n"), hidden)
	}
	fmt.Fprintf(&text, "\n### %s\n\n%s\n", config.Text("Não coberto", "Not covered"), config.Text("Testes e código gerado não entram nas partes. Só achados de severidade alta passam pela revisão.", "Tests and generated code are left out of the parts. Only high findings go through review."))
	if r.unread > 0 {
		fmt.Fprintf(&text, config.Text("%d arquivos ficaram de fora por isso.\n", "%d files stayed out for that.\n"), r.unread)
	}
	if r.unreviewed > 0 {
		fmt.Fprintf(&text, config.Text("%d achados de severidade alta ficaram sem revisão.\n", "%d high findings stayed unreviewed.\n"), r.unreviewed)
	}
	if len(r.missed) > 0 {
		fmt.Fprintf(&text, config.Text("Sem resposta do modelo: %s\n", "No answer from the model: %s\n"), strings.Join(r.missed, ", "))
	}
	if !brief {
		text.WriteString(config.Text("\nPara dispensar um achado, copie a linha abaixo dele para `ignored.txt`, nesta pasta.\n", "\nTo dismiss a finding, copy its key below to `ignored.txt`, in this folder.\n"))
		for _, finding := range r.findings {
			fmt.Fprintf(&text, "    %s\n", finding.key())
		}
	}
	return text.String()
}
