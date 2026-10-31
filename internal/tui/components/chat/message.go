package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

type uiMessageType int

const (
	userMessageType uiMessageType = iota
	assistantMessageType
	toolMessageType

	maxResultHeight = 10
)

type uiMessage struct {
	ID          string
	messageType uiMessageType
	position    int
	height      int
	content     string
}

func toMarkdown(content string, _ bool, width int) string {
	return styles.RenderMarkdownBlocks(content, width, width)
}

func renderMessage(msg string, isUser bool, _ bool, width int, info ...string) string {
	return renderMessageWithCache(msg, isUser, width, nil, info...)
}

func renderMessageWithCache(msg string, isUser bool, width int, cache *styles.MarkdownCache, info ...string) string {
	t := theme.CurrentTheme()
	pipe := t.Primary()
	if isUser {
		pipe = t.Secondary()
	}
	return renderMessageWithPipe(msg, width, cache, pipe, info...)
}

func renderMessageWithPipe(msg string, width int, cache *styles.MarkdownCache, pipe color.Color, info ...string) string {
	t := theme.CurrentTheme()

	style := styles.BaseStyle().
		Width(max(1, width)).
		BorderLeft(true).
		Foreground(t.TextMuted()).
		BorderForeground(pipe).
		BorderStyle(lipgloss.NormalBorder())

	contentWidth := max(1, width-style.GetHorizontalFrameSize())
	measure := contentWidth
	if c := config.Get(); c != nil && c.TUI.ProseWidth > 0 {
		measure = c.TUI.ProseWidth
	}
	markdown := styles.RenderMarkdownBlocks
	if cache != nil {
		markdown = cache.RenderStreaming
	}
	parts := []string{markdown(msg, contentWidth, measure)}

	// Remove newline at the end
	parts[0] = strings.TrimSuffix(parts[0], "\n")
	if len(info) > 0 {
		parts = append(parts, info...)
	}
	if len(parts) == 1 {
		if framed, ok := renderLaidOutMessage(parts[0], width, pipe); ok {
			return framed
		}
	}

	rendered := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)

	return rendered
}

// The single Markdown fragment already owns its layout. Keep native ANSI/link
// wrapping, but reuse fixed frame tags instead of rebuilding them for every row.
func renderLaidOutMessage(content string, width int, pipe color.Color) (string, bool) {
	if width < 2 || strings.IndexFunc(content, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\x1b' }) >= 0 {
		return "", false
	}
	palette := theme.CurrentTheme()
	inner := width - 1
	content = lipgloss.Wrap(content, inner, "")
	border := ansi.Style{}.ForegroundColor(pipe).Styled("│")
	text := ansi.Style{}.ForegroundColor(palette.TextMuted()).BackgroundColor(palette.Background()).String()
	pad := ansi.Style{}.BackgroundColor(palette.Background()).String()
	spaces := strings.Repeat(" ", inner)
	var out strings.Builder
	out.Grow(len(content) + (strings.Count(content, "\n")+1)*(len(border)+len(text)+len(pad)+inner+8))
	first := true
	for line := range strings.SplitSeq(content, "\n") {
		remaining := inner - ansi.StringWidth(line)
		if remaining < 0 {
			return "", false
		}
		if !first {
			out.WriteByte('\n')
		}
		first = false
		out.WriteString(border)
		out.WriteString(text)
		out.WriteString(line)
		out.WriteString(ansi.ResetStyle)
		if remaining > 0 {
			out.WriteString(pad)
			out.WriteString(spaces[:remaining])
			out.WriteString(ansi.ResetStyle)
		}
	}
	return out.String(), true
}

func renderUserMessage(msg message.Message, isFocused bool, width int, position int) uiMessage {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.TextMuted()).
		Foreground(t.Text())
	for _, attachment := range msg.BinaryContent() {
		file := filepath.Base(attachment.Path)
		var filename string
		if len(file) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, file[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, file)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := ""
	if len(styledAttachments) > 0 {
		attachmentContent := styles.BaseStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...))
		content = renderMessage(msg.Content().String(), true, isFocused, width, attachmentContent)
	} else {
		content = renderMessage(msg.Content().String(), true, isFocused, width)
	}
	userMsg := uiMessage{
		ID:          msg.ID,
		messageType: userMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return userMsg
}

// Rendering receives only an already loaded task projection. Storage access
// belongs to update commands and never to a frame or a Markdown conversion.
type taskHistoryReader interface {
	List(context.Context, string) ([]message.Message, error)
}

// Returns multiple uiMessages because of the tool calls
func renderAssistantMessage(
	msg message.Message,
	_ int,
	allMessages []message.Message, // we need this to get tool results and the user message
	messagesService taskHistoryReader, // We need this to get the task tool messages
	focusedUIMessageId string,
	isSummary bool,
	width int,
	position int,
	markdown ...*styles.MarkdownCache,
) []uiMessage {
	messages := []uiMessage{}
	content := msg.Content().String()
	finished := msg.IsFinished()
	finishData := msg.FinishPart()
	info := []string{}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	if label := citationStatusLabel(msg.CitationValidation()); label != "" {
		info = append(info, baseStyle.Foreground(t.Warning()).Render(" "+label))
	}

	// Add finish info if available
	if finished {
		switch finishData.Reason {
		case message.FinishReasonEndTurn:
			took := formatTimestampDiff(msg.CreatedAt, finishData.Time)
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, took)),
			)
		case message.FinishReasonCanceled:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "canceled")),
			)
		case message.FinishReasonError:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "error")),
			)
		case message.FinishReasonPermissionDenied:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "permission denied")),
			)
		}
	}
	if content != "" || (finished && finishData.Reason == message.FinishReasonEndTurn) {
		if content == "" {
			content = "*Finished without output*"
		}
		if isSummary {
			info = append(info, baseStyle.Width(width-1).Foreground(t.TextMuted()).Render(config.Text(" (resumo)", " (summary)")))
		}

		var cache *styles.MarkdownCache
		if len(markdown) > 0 {
			cache = markdown[0]
		}
		content = renderMessageWithCache(content, false, width, cache, info...)
		messages = append(messages, uiMessage{
			ID:          msg.ID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		})
		position += messages[0].height
		position++ // for the space
	}

	for i, toolCall := range msg.ToolCalls() {
		toolCallContent := renderToolMessage(
			toolCall,
			allMessages,
			messagesService,
			focusedUIMessageId,
			false,
			width,
			i+1,
		)
		messages = append(messages, toolCallContent)
		position += toolCallContent.height
		position++ // for the space
	}
	return messages
}

func findToolResponse(toolCallID string, futureMessages []message.Message) *message.ToolResult {
	for _, msg := range futureMessages {
		for _, result := range msg.ToolResults() {
			if result.ToolCallID == toolCallID {
				return &result
			}
		}
	}
	return nil
}

func toolName(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Task"
	case tools.BashToolName:
		return "Bash"
	case tools.EditToolName:
		return "Edit"
	case tools.FetchToolName:
		return "Fetch"
	case tools.GlobToolName:
		return "Glob"
	case tools.GrepToolName:
		return "Grep"
	case tools.LSToolName:
		return "List"
	case tools.SourcegraphToolName:
		return "Sourcegraph"
	case tools.ViewToolName:
		return "View"
	case tools.WriteToolName:
		return "Write"
	case tools.PatchToolName:
		return "Patch"
	}
	return name
}

func getToolAction(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Preparing prompt..."
	case tools.BashToolName:
		return "Building command..."
	case tools.EditToolName:
		return "Preparing edit..."
	case tools.FetchToolName:
		return "Writing fetch..."
	case tools.GlobToolName:
		return "Finding files..."
	case tools.GrepToolName:
		return "Searching content..."
	case tools.LSToolName:
		return "Listing directory..."
	case tools.SourcegraphToolName:
		return "Searching code..."
	case tools.ViewToolName:
		return "Reading file..."
	case tools.WriteToolName:
		return "Preparing write..."
	case tools.PatchToolName:
		return "Preparing patch..."
	}
	return "Working..."
}

// renders params, params[0] (params[1]=params[2] ....)
func renderParams(paramsWidth int, params ...string) string {
	if len(params) == 0 {
		return ""
	}
	mainParam := params[0]
	if len(mainParam) > paramsWidth {
		mainParam = mainParam[:paramsWidth-3] + "..."
	}

	if len(params) == 1 {
		return mainParam
	}
	otherParams := params[1:]
	// create pairs of key/value
	// if odd number of params, the last one is a key without value
	if len(otherParams)%2 != 0 {
		otherParams = append(otherParams, "")
	}
	parts := make([]string, 0, len(otherParams)/2)
	for i := 0; i < len(otherParams); i += 2 {
		key := otherParams[i]
		value := otherParams[i+1]
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	partsRendered := strings.Join(parts, ", ")
	remainingWidth := paramsWidth - lipgloss.Width(partsRendered) - 5 // for the space
	if remainingWidth < 30 {
		// No space for the params, just show the main
		return mainParam
	}

	if len(parts) > 0 {
		mainParam = fmt.Sprintf("%s (%s)", mainParam, strings.Join(parts, ", "))
	}

	return ansi.Truncate(mainParam, paramsWidth, "...")
}

func removeWorkingDirPrefix(path string) string {
	wd := config.WorkingDirectory()
	path = strings.TrimPrefix(path, wd)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "../")
	return path
}

func renderToolParams(paramWidth int, toolCall message.ToolCall) string {
	params := ""
	switch toolCall.Name {
	case agent.AgentToolName:
		var params agent.AgentParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		prompt := strings.ReplaceAll(params.Prompt, "\n", " ")
		return renderParams(paramWidth, prompt)
	case tools.BashToolName:
		var params tools.BashParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		command := strings.ReplaceAll(params.Command, "\n", " ")
		return renderParams(paramWidth, command)
	case tools.EditToolName:
		var params tools.EditParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, filePath)
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		url := params.URL
		toolParams := []string{
			url,
		}
		if params.Format != "" {
			toolParams = append(toolParams, "format", params.Format)
		}
		if params.Timeout != 0 {
			toolParams = append(toolParams, "timeout", (time.Duration(params.Timeout) * time.Second).String())
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GlobToolName:
		var params tools.GlobParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		pattern := params.Pattern
		toolParams := []string{
			pattern,
		}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GrepToolName:
		return renderToolParamsCaseToolsGrepToolName(paramWidth, &toolCall)
	case tools.LSToolName:
		var params tools.LSParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		path := params.Path
		if path == "" {
			path = "."
		}
		return renderParams(paramWidth, path)
	case tools.SourcegraphToolName:
		var params tools.SourcegraphParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, params.Query)
	case tools.ViewToolName:
		var params tools.ViewParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		toolParams := []string{
			filePath,
		}
		if params.Limit != 0 {
			toolParams = append(toolParams, "limit", fmt.Sprintf("%d", params.Limit))
		}
		if params.Offset != 0 {
			toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
		}
		return renderParams(paramWidth, toolParams...)
	case tools.WriteToolName:
		var params tools.WriteParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, filePath)
	default:
		input := strings.ReplaceAll(toolCall.Input, "\n", " ")
		params = renderParams(paramWidth, input)
	}
	return params
}

func truncateHeight(content string, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return strings.Join(lines[:height], "\n")
	}
	return content
}

func renderToolResponse(toolCall message.ToolCall, response message.ToolResult, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if response.IsError {
		errContent := fmt.Sprintf("Error: %s", strings.ReplaceAll(response.Content, "\n", " "))
		errContent = ansi.Truncate(errContent, width-1, "...")
		return baseStyle.
			Width(width).
			Foreground(t.Error()).
			Render(errContent)
	}

	resultContent := truncateHeight(response.Content, maxResultHeight)
	switch toolCall.Name {
	case agent.AgentToolName:
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, false, width),
			t.Background(),
		)
	case tools.BashToolName:
		resultContent = fmt.Sprintf("```bash\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.EditToolName:
		metadata := tools.EditResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		truncDiff := truncateHeight(metadata.Diff, maxResultHeight)
		formattedDiff, _ := diff.FormatDiff(truncDiff, diff.WithTotalWidth(width))
		return formattedDiff
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		mdFormat := "markdown"
		switch params.Format {
		case "text":
			mdFormat = "text"
		case "html":
			mdFormat = "html"
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", mdFormat, resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.GlobToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.GrepToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.LSToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.SourcegraphToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.ViewToolName:
		metadata := tools.ViewResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(metadata.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, truncateHeight(metadata.Content, maxResultHeight))
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.WriteToolName:
		params := tools.WriteParams{}
		json.Unmarshal([]byte(toolCall.Input), &params)
		metadata := tools.WriteResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(params.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, truncateHeight(params.Content, maxResultHeight))
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	default:
		resultContent = fmt.Sprintf("```text\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	}
}

func renderToolMessage(
	toolCall message.ToolCall,
	allMessages []message.Message,
	messagesService taskHistoryReader,
	focusedUIMessageId string,
	nested bool,
	width int,
	position int,
) uiMessage {
	if nested {
		width = width - 3
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		PaddingLeft(1).
		BorderForeground(t.TextMuted())

	response := findToolResponse(toolCall.ID, allMessages)
	toolNameText := baseStyle.Foreground(t.TextMuted()).
		Render(fmt.Sprintf("%s: ", toolName(toolCall.Name)))

	if !toolCall.Finished {
		// Get a brief description of what the tool is doing
		toolAction := getToolAction(toolCall.Name)

		progressText := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(toolAction)

		content := style.Render(lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, progressText))
		toolMsg := uiMessage{
			messageType: toolMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		}
		return toolMsg
	}

	params := renderToolParams(width-2-lipgloss.Width(toolNameText), toolCall)
	responseContent := ""
	if response != nil {
		responseContent = renderToolResponse(toolCall, *response, width-2)
		responseContent = strings.TrimSuffix(responseContent, "\n")
	} else {
		responseContent = baseStyle.
			Italic(true).
			Width(width - 2).
			Foreground(t.TextMuted()).
			Render(config.Text("Aguardando resposta…", "Waiting for response..."))
	}

	parts := []string{}
	if !nested {
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)

		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, formattedParams))
	} else {
		prefix := baseStyle.
			Foreground(t.TextMuted()).
			Render(" └ ")
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, prefix, toolNameText, formattedParams))
	}

	if toolCall.Name == agent.AgentToolName {
		taskMessages := projectedTaskHistory(messagesService, toolCall.ID)
		toolCalls := []message.ToolCall{}
		for _, v := range taskMessages {
			toolCalls = append(toolCalls, v.ToolCalls()...)
		}
		for _, call := range toolCalls {
			rendered := renderToolMessage(call, []message.Message{}, messagesService, focusedUIMessageId, true, width, 0)
			parts = append(parts, rendered.content)
		}
	}
	if responseContent != "" && !nested {
		parts = append(parts, responseContent)
	}

	content := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)
	if nested {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		)
	}
	toolMsg := uiMessage{
		messageType: toolMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return toolMsg
}

// Helper function to format the time difference between two Unix timestamps
func formatTimestampDiff(start, end int64) string {
	// Stored message/finish timestamps use Unix seconds, not milliseconds.
	if start <= 0 || end < start {
		return "—"
	}
	seconds := end - start
	if seconds == 0 {
		return "<1s"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}

func renderToolParamsCaseToolsGrepToolName(paramWidth int, toolCall *message.ToolCall) (extractedResult0 string) {
	var params tools.GrepParams
	json.Unmarshal([]byte(toolCall.Input), &params)
	pattern := params.Pattern
	toolParams := []string{
		pattern,
	}
	if params.Path != "" {
		toolParams = append(toolParams, "path", params.Path)
	}
	if params.Include != "" {
		toolParams = append(toolParams, "include", params.Include)
	}
	if params.LiteralText {
		toolParams = append(toolParams, "literal", "true")
	}
	return renderParams(paramWidth, toolParams...)

}

func projectedTaskHistory(reader taskHistoryReader, id string) []message.Message {
	if reader == nil {
		return nil
	}
	history, _ := reader.List(context.Background(), id)
	return history
}
