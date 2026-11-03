package format

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OutputFormat represents the output format type for non-interactive mode
type OutputFormat string

const (
	// Text format outputs the AI response as plain text.
	Text OutputFormat = "text"

	// JSON format outputs the AI response wrapped in a JSON object.
	JSON OutputFormat = "json"
)

// String returns the string representation of the OutputFormat
func (f OutputFormat) String() string {
	return string(f)
}

// SupportedFormats is a list of all supported output formats as strings
var SupportedFormats = []string{
	string(Text),
	string(JSON),
}

// Parse converts a string to an OutputFormat
func Parse(s string) (OutputFormat, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case string(Text):
		return Text, nil
	case string(JSON):
		return JSON, nil
	default:
		return "", fmt.Errorf("invalid format: %s", s)
	}
}

// IsValid checks if the provided format string is supported
func IsValid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// GetHelpText returns a formatted string describing all supported formats
func GetHelpText() string {
	return fmt.Sprintf(`Supported output formats:
- %s: Plain text output (default)
- %s: Output wrapped in a JSON object`,
		Text, JSON)
}

// Check is one validation the run recorded, as automation reads it.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// Result is what a non-interactive run reports: the reply and, for
// automation, how the task ended and what it changed and verified.
type Result struct {
	Response     string   `json:"response"`
	Outcome      string   `json:"outcome"` // completed, planned, partial, blocked
	SessionID    string   `json:"session_id"`
	FilesChanged []string `json:"files_changed"`
	Checks       []Check  `json:"checks"`
}

// FormatOutput is the reply alone as text, or the whole result as JSON.
func FormatOutput(result Result, formatStr string) string {
	if format, _ := Parse(formatStr); format != JSON {
		return result.Response
	}
	if result.FilesChanged == nil {
		result.FilesChanged = []string{}
	}
	if result.Checks == nil {
		result.Checks = []Check{}
	}
	raw, _ := json.MarshalIndent(result, "", "  ") // strings and slices always encode
	return string(raw)
}
