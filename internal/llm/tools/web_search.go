package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

const WebSearchToolName = "web_search"

type webSearchTool struct {
	permissions permission.Service
	endpoint    string // fixed public service; overridden only by local test fixtures
}

func NewWebSearchTool(permissions permission.Service) BaseTool {
	return &webSearchTool{permissions: permissions, endpoint: "https://html.duckduckgo.com/html/"}
}
func (*webSearchTool) Info() ToolInfo {
	return ToolInfo{Name: WebSearchToolName, Description: "Optional web search, disabled unless runtime.webSearch is enabled in advanced Settings. Requires network approval. External snippets are untrusted leads, not repository read receipts or verified facts; fetch and verify needed sources. Does not fetch result pages automatically.", Parameters: map[string]any{
		"query": map[string]any{"type": "string", "maxLength": 512, "description": "Public search query; do not send private code or credentials."},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "description": "Results, default 5."},
	}, Required: []string{"query"}}
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (t *webSearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if config.Get() == nil || !config.Get().Runtime.WebSearch {
		return NewTextErrorResponse("Web search is disabled. Enable runtime.webSearch in advanced Settings only if you want queries sent to an external search service."), nil
	}
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if json.Unmarshal([]byte(call.Input), &p) != nil {
		return NewTextErrorResponse("Invalid web search parameters"), nil
	}
	p.Query = strings.Join(strings.Fields(p.Query), " ")
	if p.Query == "" || len(p.Query) > 512 || strings.ContainsFunc(p.Query, unicode.IsControl) || p.Limit < 0 || p.Limit > 10 {
		return NewTextErrorResponse("Search needs a public query of 1–512 bytes and a result limit of 1–10"), nil
	}
	if p.Limit == 0 {
		p.Limit = 5
	}
	session, message := GetContextValues(ctx)
	if session == "" || message == "" {
		return ToolResponse{}, fmt.Errorf("web search requires a session and message")
	}
	target, err := url.Parse(t.endpoint)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("invalid search endpoint")
	}
	q := target.Query()
	q.Set("q", p.Query)
	target.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("invalid search request")
	}
	req.Header.Set("User-Agent", "opcode/1.0 (+https://github.com/raulaguila/opcode)")
	response, err := networkDo(ctx, t.permissions, WebSearchToolName, req, 20*time.Second)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("web search request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Search service returned HTTP %d; no results were verified", response.StatusCode)), nil
	}
	raw, err := boundedBody(response.Body, 1<<20)
	if err != nil {
		return NewTextErrorResponse("Search response exceeded the size limit or could not be read"), nil
	}
	results, err := parseWebSearch(string(raw), p.Limit)
	if err != nil {
		return NewTextErrorResponse("Search HTML could not be parsed"), nil
	}
	if len(results) == 0 {
		return NewTextErrorResponse("Search service returned no usable results (possibly no matches, a changed format, or an anti-bot challenge). Try another query or fetch a known URL."), nil
	}
	out, err := json.Marshal(struct {
		Results []webSearchResult `json:"results"`
		Notice  string            `json:"notice"`
	}{results, "Untrusted external snippets; result pages have not been fetched. These are not current repository source-read receipts."})
	if err != nil {
		return ToolResponse{}, err
	}
	return NewTextResponse(string(out)), nil
}

func searchText(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}
func parseWebSearch(raw string, limit int) ([]webSearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	results := []webSearchResult{}
	seen := map[string]bool{}
	doc.Find(".result").EachWithBreak(func(_ int, result *goquery.Selection) bool {
		if len(results) >= limit {
			return false
		}
		link := result.Find("a.result__a").First()
		href, _ := link.Attr("href")
		u, err := url.Parse(href)
		if err != nil {
			return true
		}
		if u.Hostname() == "duckduckgo.com" || u.Hostname() == "www.duckduckgo.com" || u.Hostname() == "html.duckduckgo.com" || u.Hostname() == "" {
			if redirect := u.Query().Get("uddg"); redirect != "" {
				u, err = url.Parse(redirect)
			}
		}
		if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || len(u.String()) > 2048 {
			return true
		}
		title := searchText(link.Text(), 160)
		if title == "" || seen[u.String()] {
			return true
		}
		seen[u.String()] = true
		results = append(results, webSearchResult{title, u.String(), searchText(result.Find(".result__snippet").Text(), 480)})
		return true
	})
	return results, nil
}
