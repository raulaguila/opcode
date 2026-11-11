package agent

import "github.com/raulaguila/opcode/internal/message"

func (a *agent) budgetRequest(session string, history []message.Message, run *generationRun) ([]message.Message, error) {
	bounded, _, err := a.budgetPreparedRequest(session, history, run)
	return bounded, err
}

// lastSent is the last message a request carried.
func lastSent(history []message.Message) *message.Message {
	return &history[len(history)-1]
}
