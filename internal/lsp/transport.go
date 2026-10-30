package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"
)

// Write writes an LSP message to the given writer
func WriteMessage(w io.Writer, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	cnf := config.Get()

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Sending message to server", "method", msg.Method, "id", msg.ID)
	}

	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
	if err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// ReadMessage reads a single LSP message from the given reader
func ReadMessage(r *bufio.Reader) (*Message, error) {
	cnf := config.Get()
	// Read headers
	var contentLength int
	headerBytes := 0
	if result0, result1, handled := readMessageState(r, cnf, &contentLength, &headerBytes); handled {
		return result0, result1
	}

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Content-Length", "length", contentLength)
	}

	if contentLength <= 0 || contentLength > 4<<20 {
		return nil, fmt.Errorf("LSP message exceeds size limit or has invalid length")
	}
	// Read content
	content := make([]byte, contentLength)
	_, err := io.ReadFull(r, content)
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Received content", "bytes", len(content))
	}

	// Parse message
	var msg Message
	if err := json.Unmarshal(content, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

// handleMessages reads and dispatches messages in a loop
func (c *Client) handleMessages() {
	if c.transportDone != nil {
		defer close(c.transportDone)
	}
	cnf := config.Get()
	if handled := handleMessagesState(c, cnf); handled {
		return
	}
}

// Call makes a request and waits for the response
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	cnf := config.Get()
	id := c.nextID.Add(1)

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Making call", "method", method, "id", id)
	}

	msg, err := NewRequest(id, method, params)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Create response channel
	ch := make(chan *Message, 1)
	c.handlersMu.Lock()
	c.handlers[id] = ch
	c.handlersMu.Unlock()

	defer func() {
		c.handlersMu.Lock()
		delete(c.handlers, id)
		c.handlersMu.Unlock()
	}()

	// Send request
	if err := c.writeMessage(ctx, msg); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Request sent", "method", method, "id", id)
	}

	// Wait for response
	var resp *Message
	select {
	case resp = <-ch:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.transportDone:
		return fmt.Errorf("LSP transport closed")
	}
	if resp == nil {
		return fmt.Errorf("LSP transport closed")
	}

	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Received response", "id", id)
	}

	if resp.Error != nil {
		return fmt.Errorf("request failed: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	if result0, handled := callResult(result, resp); handled {
		return result0
	}

	return nil
}

// Notify sends a notification (a request without an ID that doesn't expect a response)
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	cnf := config.Get()
	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Sending notification", "method", method)
	}

	msg, err := NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	if err := c.writeMessage(ctx, msg); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	return nil
}

type (
	NotificationHandler  func(params json.RawMessage)
	ServerRequestHandler func(params json.RawMessage) (any, error)
)

func (c *Client) writeMessage(ctx context.Context, msg *Message) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() { c.writeMu.Lock(); defer c.writeMu.Unlock(); done <- WriteMessage(c.stdin, msg) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		c.stdin.Close()
		return ctx.Err()
	}
}

func handleMessagesState(c *Client, cnf *config.Config) (handled bool) {
	for {
		msg, err := ReadMessage(c.stdout)
		if result0, handled := handleMessagesStateStateCase(cnf, err); handled {
			return result0
		}

		// Handle server->client request (has both Method and ID)
		if msg.Method != "" && msg.ID != 0 {
			handleMessagesStateCnfCnfDebugLSP(cnf, msg)

			response := &Message{
				JSONRPC: "2.0",
				ID:      msg.ID,
			}

			// Look up handler for this method
			c.serverHandlersMu.RLock()
			handler, ok := c.serverRequestHandlers[msg.Method]
			c.serverHandlersMu.RUnlock()

			handleMessagesStateStateCaseCase(msg, response, handler, ok)

			// Send response back to server
			if err := c.writeMessage(context.Background(), response); err != nil {
				logging.Error("Error sending response to server", "error", err)
			}

			continue
		}

		// Handle notification (has Method but no ID)
		if msg.Method != "" && msg.ID == 0 {
			c.notificationMu.RLock()
			handler, ok := c.notificationHandlers[msg.Method]
			c.notificationMu.RUnlock()

			handleMessagesStateState(cnf, msg, handler, ok)
			continue
		}

		// Handle response to our request (has ID but no Method)
		handleMessagesStateIDMethod(c, cnf, msg)
	}
}

func handleMessagesStateIDMethod(c *Client, cnf *config.Config, msg *Message) {
	if msg.ID != 0 && msg.Method == "" {
		c.handlersMu.RLock()
		ch, ok := c.handlers[msg.ID]
		c.handlersMu.RUnlock()

		if ok {
			if cnf != nil && cnf.DebugLSP {
				logging.Debug("Received response for request", "id", msg.ID)
			}
			ch <- msg
			close(ch)
		} else if cnf != nil && cnf.DebugLSP {
			logging.Debug("No handler for response", "id", msg.ID)
		}
	}
}

func handleMessagesStateState(cnf *config.Config, msg *Message, handler NotificationHandler, ok bool) {
	if ok {
		if cnf != nil && cnf.DebugLSP {
			logging.Debug("Handling notification", "method", msg.Method)
		}
		go handler(msg.Params)
	} else if cnf != nil && cnf.DebugLSP {
		logging.Debug("No handler for notification", "method", msg.Method)
	}
}

func handleMessagesStateStateCase(cnf *config.Config, err error) (extractedResult0 bool, handled bool) {
	if err != nil {
		if cnf != nil && cnf.DebugLSP {
			logging.Error("Error reading message", "error", err)
		}
		return true, true
	}
	return extractedResult0, false
}

func handleMessagesStateStateCaseCase(msg *Message, response *Message, handler ServerRequestHandler, ok bool) {
	if ok {
		result, err := handler(msg.Params)
		if err != nil {
			response.Error = &ResponseError{
				Code:    -32603,
				Message: err.Error(),
			}
		} else {
			rawJSON, err := json.Marshal(result)
			if err != nil {
				response.Error = &ResponseError{
					Code:    -32603,
					Message: fmt.Sprintf("failed to marshal response: %v", err),
				}
			} else {
				response.Result = rawJSON
			}
		}
	} else {
		response.Error = &ResponseError{
			Code:    -32601,
			Message: fmt.Sprintf("method not found: %s", msg.Method),
		}
	}
}

func readMessageState(r *bufio.Reader, cnf *config.Config, contentLength *int, headerBytes *int) (extractedResult0 *Message, extractedResult1 error, handled bool) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read header: %w", err), true
		}
		(*headerBytes) += len(line)
		if (*headerBytes) > 8192 {
			return nil, fmt.Errorf("LSP header exceeds limit"), true
		}
		line = strings.TrimSpace(line)

		if cnf != nil && cnf.DebugLSP {
			logging.Debug("Received header", "line", line)
		}

		if line == "" {
			break // End of headers
		}

		if strings.HasPrefix(line, "Content-Length: ") {
			_, err := fmt.Sscanf(line, "Content-Length: %d", contentLength)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err), true
			}
		}
	}
	return extractedResult0, extractedResult1, false
}

func callResult(result any, resp *Message) (extractedResult0 error, handled bool) {
	if result != nil {
		// If result is a json.RawMessage, just copy the raw bytes
		if rawMsg, ok := result.(*json.RawMessage); ok {
			*rawMsg = resp.Result
			return nil, true
		}
		// Otherwise unmarshal into the provided type
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err), true
		}
	}
	return extractedResult0, false
}

func handleMessagesStateCnfCnfDebugLSP(cnf *config.Config, msg *Message) {
	if cnf != nil && cnf.DebugLSP {
		logging.Debug("Received request from server", "method", msg.Method, "id", msg.ID)
	}
}
