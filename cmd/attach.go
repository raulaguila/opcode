package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/server"
	"github.com/spf13/cobra"
)

func newAttachCommand() *cobra.Command {
	var base string
	var once bool
	c := &cobra.Command{Use: "attach [SESSION]", Short: "Reconectar ao servidor local sem repetir comandos", Args: cobra.MaximumNArgs(1), RunE: func(c *cobra.Command, args []string) error {
		c.SilenceUsage = true
		client, err := server.NewClient(base, os.Getenv("OPCODE_SERVER_TOKEN"))
		if err != nil {
			return err
		}
		defer client.Close()
		ctx, cancel := signal.NotifyContext(c.Context(), os.Interrupt)
		defer cancel()
		request := map[string]string{"title": "Sessão remota"}
		if len(args) > 0 {
			request["resume"] = args[0]
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := client.Do(ctx, "POST", "/v1/sessions", request, &created); err != nil {
			return err
		}
		if once {
			var snapshot server.Snapshot
			if err := client.Do(ctx, "GET", "/v1/sessions/"+created.ID, nil, &snapshot); err != nil {
				return err
			}
			return printSnapshotJSON(c.OutOrStdout(), snapshot)
		}
		fmt.Fprintf(c.OutOrStdout(), "Sessão %s · /help mostra ações · /quit desconecta e mantém a execução\n", created.ID)
		return attachLoop(ctx, cancel, client, created.ID, c.InOrStdin(), c.OutOrStdout())
	}}
	c.Flags().StringVar(&base, "url", "http://127.0.0.1:4096", "Servidor local")
	c.Flags().BoolVar(&once, "once", false, "Mostrar snapshot JSON e desconectar")
	return c
}

type attachResult struct {
	label string
	err   error
}

func printSnapshotJSON(out io.Writer, snapshot server.Snapshot) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

type attachSession struct {
	ctx                   context.Context
	client                *server.Client
	path                  string
	out                   io.Writer
	results               chan attachResult
	prompts               chan map[string]string
	slots                 chan struct{}
	jobs                  sync.WaitGroup
	seen                  map[string]string
	permissionIDs         map[string]bool
	lastError, questionID string
}

type attachInput struct {
	line string
	err  error
}

func attachInputLines(ctx context.Context, input io.Reader) <-chan attachInput {
	lines := make(chan attachInput, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 32000)
		for scanner.Scan() {
			select {
			case lines <- attachInput{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- attachInput{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return lines
}
func attachLoop(ctx context.Context, cancel context.CancelFunc, client *server.Client, id string, input io.Reader, out io.Writer) error {
	defer cancel()
	lines := attachInputLines(ctx, input)
	a := &attachSession{ctx: ctx, client: client, path: "/v1/sessions/" + id, out: out, results: make(chan attachResult, 8), prompts: make(chan map[string]string, 8), slots: make(chan struct{}, 8), seen: map[string]string{}, permissionIDs: map[string]bool{}}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	defer func() { cancel(); a.jobs.Wait() }()
	a.jobs.Add(1)
	go a.sendPrompts()
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-a.results:
			if result.err != nil {
				fmt.Fprintln(out, "Erro: "+safeTerminal(result.err.Error()))
			} else {
				fmt.Fprintln(out, result.label)
			}
		case input, open := <-lines:
			if done, err := a.receiveInput(input, open); done {
				return err
			}
		case <-ticker.C:
			a.poll()
		}
	}
}
func (a *attachSession) receiveInput(input attachInput, open bool) (bool, error) {
	if !open {
		return true, nil
	}
	if input.err != nil {
		if a.ctx.Err() != nil {
			return true, nil
		}
		return true, fmt.Errorf("read attach input: %s", safeTerminal(tools.RedactCredentials(input.err.Error())))
	}
	return a.command(input.line), nil
}

func (a *attachSession) sendPrompts() {
	defer a.jobs.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case body := <-a.prompts:
			err := a.client.Do(a.ctx, "POST", a.path+"/prompt", body, nil)
			select {
			case a.results <- attachResult{"Instrução aceita.", err}:
			case <-a.ctx.Done():
				return
			}
		}
	}
}
func (a *attachSession) send(label, endpoint string, body any) {
	select {
	case a.slots <- struct{}{}:
	default:
		fmt.Fprintln(a.out, "Aguarde as operações pendentes.")
		return
	}
	a.jobs.Add(1)
	go func() {
		defer a.jobs.Done()
		defer func() { <-a.slots }()
		err := a.client.Do(a.ctx, "POST", a.path+endpoint, body, nil)
		select {
		case a.results <- attachResult{label, err}:
		case <-a.ctx.Done():
		}
	}()
}
func (a *attachSession) command(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	command, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch command {
	case "/quit", "/exit":
		return true
	case "/help":
		fmt.Fprintln(a.out, "Texto: enviar instrução · /approve ID · /allow-session ID (mesmos argumentos) · /deny ID · /answer ID texto · /mode ask|plan|execute · /terminal comando · /input PROCESSO texto · /key PROCESSO enter|ctrl-c|tab · /stop PROCESSO · /cancel · /quit")
	case "/approve", "/allow-session", "/deny":
		decision := "once"
		if command == "/deny" {
			decision = "deny"
		}
		if command == "/allow-session" {
			decision = "session"
		}
		a.send("Permissão respondida.", "/permissions/"+rest, map[string]string{"decision": decision})
	case "/answer":
		qid, text, _ := strings.Cut(rest, " ")
		a.send("Resposta enviada.", "/answer", map[string]string{"id": qid, "text": text})
	case "/mode":
		a.send("Modo atualizado.", "/mode", map[string]string{"mode": rest})
	case "/cancel":
		a.send("Cancelamento solicitado.", "/cancel", map[string]any{})
	case "/terminal":
		a.send("Terminal iniciado.", "/processes", tools.ProcessParams{Command: rest, PTY: true, Timeout: 300000, Cols: 100, Rows: 24})
	case "/input", "/key":
		a.writeInput(command, rest)
	case "/stop":
		a.send("Processo encerrado.", "/processes/"+rest+"/stop", map[string]any{})
	default:
		select {
		case a.prompts <- map[string]string{"content": line, "request_id": uuid.NewString()}:
		default:
			fmt.Fprintln(a.out, "Fila de envio cheia; aguarde a confirmação das instruções pendentes.")
		}
	}
	return false
}
func (a *attachSession) poll() {
	var snapshot server.Snapshot
	pollCtx, stopPoll := context.WithTimeout(a.ctx, 2*time.Second)
	err := a.client.Do(pollCtx, "GET", a.path, nil, &snapshot)
	stopPoll()
	if err != nil {
		if err.Error() != a.lastError {
			fmt.Fprintln(a.out, safeTerminal(err.Error()))
			a.lastError = err.Error()
		}
		return
	}
	a.lastError = ""
	a.renderMessages(snapshot)
	a.renderDecisions(snapshot)
	for _, p := range snapshot.Processes {
		a.renderProcess(p)
	}
	if len(a.seen) > 512 {
		a.seen = map[string]string{}
	}
}

func safeTerminal(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

func (a *attachSession) writeInput(command, rest string) {
	pid, data, _ := strings.Cut(rest, " ")
	if command == "/input" {
		data += "\n"
	} else {
		switch data {
		case "enter":
			data = "\r"
		case "ctrl-c":
			data = "\x03"
		case "tab":
			data = "\t"
		default:
			fmt.Fprintln(a.out, "Tecla desconhecida.")
			return
		}
	}
	a.send("Entrada enviada.", "/processes/"+pid+"/input", map[string]string{"data": data})
}

func (a *attachSession) renderMessages(snapshot server.Snapshot) {
	for _, m := range snapshot.Messages {
		if m.Role != "assistant" {
			continue
		}
		if m.Text != "" && a.seen[m.ID] != m.Text {
			previous := a.seen[m.ID]
			if after, ok := strings.CutPrefix(m.Text, previous); ok {
				fmt.Fprint(a.out, safeTerminal(after))
			} else {
				fmt.Fprint(a.out, "\n"+safeTerminal(m.Text))
			}
			if m.Finished {
				fmt.Fprintln(a.out)
			}
			a.seen[m.ID] = m.Text
		}
	}
}

func (a *attachSession) renderDecisions(snapshot server.Snapshot) {
	for _, p := range snapshot.Permissions {
		if !a.permissionIDs[p.ID] {
			fmt.Fprintf(a.out, "\nPermissão %s: %s · %s · %v\n/approve %s ou /deny %s\n", p.ID, safeTerminal(p.ToolName), safeTerminal(p.Description), safeTerminal(fmt.Sprint(p.Params)), p.ID, p.ID)
			a.permissionIDs[p.ID] = true
		}
	}
	if snapshot.Question != nil && snapshot.Question.ID != a.questionID {
		q := snapshot.Question
		a.questionID = q.ID
		fmt.Fprintf(a.out, "\nPergunta %s: %s · %s\n/answer %s resposta\n", q.ID, safeTerminal(q.Question), safeTerminal(strings.Join(q.Choices, " | ")), q.ID)
	}
}

func (a *attachSession) renderProcess(p tools.ProcessInfo) {
	key := "process:" + p.ID
	if !p.Running && a.seen[key+":finished"] == "true" {
		return
	}
	var screen tools.ProcessSnapshot
	pollCtx, stopPoll := context.WithTimeout(a.ctx, time.Second)
	err := a.client.Do(pollCtx, "GET", a.path+"/processes/"+p.ID, nil, &screen)
	stopPoll()
	if err != nil {
		return
	}
	text := screen.Stdout + screen.Stderr
	if screen.PTY {
		text = screen.Screen
	}
	if a.seen[key] != text {
		fmt.Fprintf(a.out, "\nTerminal %s:\n%s\n", p.ID, safeTerminal(text))
		a.seen[key] = text
	}
	if !p.Running {
		a.seen[key+":finished"] = "true"
	}
}
