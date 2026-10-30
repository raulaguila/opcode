package logging

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logfmt/logfmt"
	"github.com/raulaguila/opcode/internal/pubsub"
)

const (
	persistKeyArg  = "$_persist"
	PersistTimeArg = "$_persist_time"
)

type LogData struct {
	messages []LogMessage
	*pubsub.Broker[LogMessage]
	lock sync.Mutex
}

func (l *LogData) Add(msg LogMessage) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.messages = append(l.messages, msg)
	l.Publish(pubsub.CreatedEvent, msg)
}

func (l *LogData) List() []LogMessage {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.messages
}

var defaultLogData = &LogData{
	messages: make([]LogMessage, 0),
	Broker:   pubsub.NewBroker[LogMessage](),
}

type writer struct{}

func (w *writer) Write(p []byte) (int, error) {
	d := logfmt.NewDecoder(bytes.NewReader(p))

	for d.ScanRecord() {
		msg := LogMessage{
			ID:   fmt.Sprintf("%d", time.Now().UnixNano()),
			Time: time.Now(),
		}
		for d.ScanKeyval() {
			if err := decodeLogField(&msg, string(d.Key()), string(d.Value())); err != nil {
				return 0, err
			}

		}
		defaultLogData.Add(msg)
	}
	if d.Err() != nil {
		return 0, d.Err()
	}
	return len(p), nil
}

func NewWriter() *writer {
	w := &writer{}
	return w
}

func Subscribe(ctx context.Context) <-chan pubsub.Event[LogMessage] {
	return defaultLogData.Subscribe(ctx)
}

func List() []LogMessage {
	return defaultLogData.List()
}

func decodeLogField(msg *LogMessage, key, value string) error {
	switch key {
	case "time":
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return fmt.Errorf("parsing time: %w", err)
		}
		msg.Time = parsed
	case "level":
		msg.Level = strings.ToLower(value)
	case "msg":
		msg.Message = value
	default:
		if key == persistKeyArg {
			msg.Persist = true
		} else if key == PersistTimeArg {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return nil
			}
			msg.PersistTime = parsed
		} else {
			msg.Attributes = append(msg.Attributes, Attr{
				Key:   key,
				Value: value,
			})
		}
	}
	return nil
}
