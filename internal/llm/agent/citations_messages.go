package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/message"
)

// ErrSourceCitation distinguishes verification failure from provider capacity.
var ErrSourceCitation = errors.New("source citation remains missing after repair; read evidence preserved")

// Only closed conversational clauses are exempt: adding a claim, path, or any
// other prose requires normal citation validation, regardless of answer length.
var citationAcknowledgment = regexp.MustCompile(`(?i)^(?:perfeito|ok|okay|certo|entendido|combinado|obrigad[oa]|fica o diagn[oó]stico|deixamos (?:as mudan[çc]as|a implementa[çc][aã]o) para depois|n[aã]o farei altera[çc][oõ]es agora|nenhuma altera[çc][aã]o ser[aá] feita agora|sem altera[çc][oõ]es por enquanto|understood|thanks|thank you|we can leave the changes for later|i (?:will not|won't) make changes now|no changes will be made now)(?:[\s,.;:!—–-]+(?:perfeito|ok|okay|certo|entendido|combinado|obrigad[oa]|fica o diagn[oó]stico|deixamos (?:as mudan[çc]as|a implementa[çc][aã]o) para depois|n[aã]o farei altera[çc][oõ]es agora|nenhuma altera[çc][aã]o ser[aá] feita agora|sem altera[çc][oõ]es por enquanto|understood|thanks|thank you|we can leave the changes for later|i (?:will not|won't) make changes now|no changes will be made now))*[\s.!]*$`)

func questionCall(call message.ToolCall) bool {
	return call.Name == "ask_user_question" || call.Name == "ask_user_questions"
}

func (a *agent) markAssistantCitation(ctx context.Context, history []message.Message, run *generationRun, item *message.Message) error {
	if !run.requiresCitation || strings.TrimSpace(item.Content().Text) == "" {
		return nil
	}
	state := message.CitationPending
	for _, call := range item.ToolCalls() {
		if questionCall(call) {
			state = message.CitationValid
			if missingSourceCitation(run, history, item.Content().Text) != "" {
				state = message.CitationInvalid
			}
			break
		}
	}
	if item.SetCitationValidation(state) {
		return a.messages.Update(ctx, *item)
	}
	return nil
}

func (g *generationState) setCitationState(item *message.Message, state string) error {
	if !item.SetCitationValidation(state) {
		return nil
	}
	ctx := g.ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}
	if err := g.agent.messages.Update(ctx, *item); err != nil {
		return err
	}
	for i := range g.history {
		if g.history[i].ID == item.ID {
			g.history[i] = *item
			break
		}
	}
	return nil
}

// Workflow repair may finish an answer candidate before the citation stage.
// Resolve its badge independently so a completed attempt never stays pending.
func (g *generationState) resolvePendingCitation(item message.Message) error {
	if item.CitationValidation() != message.CitationPending {
		return nil
	}
	state := message.CitationValid
	if missingSourceCitation(g.run, g.history, item.Content().Text) != "" {
		state = message.CitationInvalid
	}
	return g.setCitationState(&item, state)
}

func (g *generationState) recordPublicCitation(item *message.Message, results *message.Message) error {
	if !g.run.requiresCitation || len(item.ToolCalls()) == 0 {
		return nil
	}
	question := false
	for _, call := range item.ToolCalls() {
		if !questionCall(call) {
			continue
		}
		question = true
		g.recordAnsweredQuestion(call.ID, results)
	}
	if strings.TrimSpace(item.Content().Text) == "" {
		return nil
	}
	// Ordinary tool progress is not an explanation. A diagnosis accompanying a
	// question, or explicit source references, retains independent verification.
	if !question && len(sourceReferences(item.Content().Text)) == 0 {
		return g.setCitationState(item, "")
	}
	state := message.CitationValid
	if missingSourceCitation(g.run, g.history, item.Content().Text) != "" {
		state = message.CitationInvalid
	}
	g.citationMessages = append(g.citationMessages, item.ID)
	return g.setCitationState(item, state)
}

func (g *generationState) recordAnsweredQuestion(id string, results *message.Message) {
	if results == nil {
		return
	}
	for _, result := range results.ToolResults() {
		if result.ToolCallID == id && !result.IsError {
			g.citationQuestionAnswered = true
			return
		}
	}
}

func (g *generationState) intermediateCitationProblem() (string, error) {
	for _, id := range g.citationMessages {
		for _, item := range g.history {
			if item.ID != id || item.CitationValidation() == message.CitationSuperseded {
				continue
			}
			if problem := missingSourceCitation(g.run, g.history, item.Content().Text); problem != "" {
				if err := g.setCitationState(&item, message.CitationInvalid); err != nil {
					return "", err
				}
				return problem, nil
			}
		}
	}
	return "", nil
}

type citationAssessment struct {
	problem string
	state   string
}

func (g *generationState) citationProblem(item message.Message) (citationAssessment, error) {
	priorProblem, err := g.intermediateCitationProblem()
	if err != nil {
		return citationAssessment{}, err
	}
	assessment := citationAssessment{}
	neutral := len(g.citationMessages) > 0 && g.citationQuestionAnswered && !g.run.citationRepair && citationAcknowledgment.MatchString(strings.TrimSpace(item.Content().Text))
	if g.run.requiresCitation && strings.TrimSpace(item.Content().Text) != "" && !neutral {
		assessment.problem = missingSourceCitation(g.run, g.history, item.Content().Text)
		assessment.state = message.CitationValid
		if assessment.problem != "" {
			assessment.state = message.CitationInvalid
		}
	}
	// The earlier diagnosis remains unresolved even when the current message is
	// merely an acknowledgment or has valid citations of its own.
	if priorProblem != "" && !g.run.citationRepair {
		assessment.problem = priorProblem
	}
	return assessment, nil
}

func (g *generationState) supersedeInvalidCitations() error {
	if !g.run.citationRepair {
		return nil
	}
	for _, id := range g.citationFailures {
		if err := g.supersedeCitation(id); err != nil {
			return err
		}
	}
	return nil
}

func (g *generationState) supersedeCitation(id string) error {
	for _, item := range g.history {
		if item.ID == id && item.CitationValidation() == message.CitationInvalid {
			return g.setCitationState(&item, message.CitationSuperseded)
		}
	}
	return nil
}
