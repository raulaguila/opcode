package logging

import (
	"fmt"
	"testing"

	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestLogKeepsOnlyRecentMessages(t *testing.T) {
	data := &LogData{Broker: pubsub.NewBroker[LogMessage]()}
	for i := range 2500 {
		data.Add(LogMessage{ID: fmt.Sprint(i)})
	}
	list := data.messages
	require.LessOrEqual(t, len(list), maxLogMessages)
	require.Equal(t, "2499", list[len(list)-1].ID)
}
