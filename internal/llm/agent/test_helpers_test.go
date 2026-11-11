package agent

import "github.com/raulaguila/opcode/internal/message"

func (a *agent) budgetRequest(session string, history []message.Message, run *generationRun) ([]message.Message, error) {
	bounded, _, err := a.budgetPreparedRequest(session, history, run)
	return bounded, err
}

// lastSent is the last message a request carried before its application state.
func lastSent(history []message.Message) *message.Message {
	if n := len(history); n > 0 && history[n-1].Content().Origin == stateOrigin {
		history = history[:n-1]
	}
	return &history[len(history)-1]
}
