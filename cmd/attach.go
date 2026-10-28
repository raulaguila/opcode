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
func attachLoop(ctx context.Context, cancel context.CancelFunc, client *server.Client, id string, input io.Reader, out io.Writer) error {
	defer cancel()
	lines := make(chan string, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 32000)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	path := "/v1/sessions/" + id
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	results := make(chan attachResult, 8)
	prompts := make(chan map[string]string, 8)
	slots := make(chan struct{}, 8)
	var jobs sync.WaitGroup
	defer func() { cancel(); jobs.Wait() }()
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case body := <-prompts:
				err := client.Do(ctx, "POST", path+"/prompt", body, nil)
				select {
				case results <- attachResult{"Instrução aceita.", err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	send := func(label, endpoint string, body any) {
		select {
		case slots <- struct{}{}:
		default:
			fmt.Fprintln(out, "Aguarde as operações pendentes.")
			return
		}
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			defer func() { <-slots }()
			err := client.Do(ctx, "POST", path+endpoint, body, nil)
			select {
			case results <- attachResult{label, err}:
			case <-ctx.Done():
			}
		}()
	}
	seen := map[string]string{}
	permissionIDs := map[string]bool{}
	lastError := ""
	questionID := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-results:
			if result.err != nil {
				fmt.Fprintln(out, "Erro: "+safeTerminal(result.err.Error()))
			} else {
				fmt.Fprintln(out, result.label)
			}
		case line, open := <-lines:
			if !open {
				return nil
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			command, rest, _ := strings.Cut(line, " ")
			rest = strings.TrimSpace(rest)
			switch command {
			case "/quit", "/exit":
				return nil
			case "/help":
				fmt.Fprintln(out, "Texto: enviar instrução · /approve ID · /allow-session ID (mesmos argumentos) · /deny ID · /answer ID texto · /mode ask|plan|execute · /terminal comando · /input PROCESSO texto · /key PROCESSO enter|ctrl-c|tab · /stop PROCESSO · /cancel · /quit")
			case "/approve", "/allow-session", "/deny":
				decision := "once"
				if command == "/deny" {
					decision = "deny"
				}
				if command == "/allow-session" {
					decision = "session"
				}
				send("Permissão respondida.", "/permissions/"+rest, map[string]string{"decision": decision})
			case "/answer":
				qid, text, _ := strings.Cut(rest, " ")
				send("Resposta enviada.", "/answer", map[string]string{"id": qid, "text": text})
			case "/mode":
				send("Modo atualizado.", "/mode", map[string]string{"mode": rest})
			case "/cancel":
				send("Cancelamento solicitado.", "/cancel", map[string]any{})
			case "/terminal":
				send("Terminal iniciado.", "/processes", tools.ProcessParams{Command: rest, PTY: true, Timeout: 300000, Cols: 100, Rows: 24})
			case "/input", "/key":
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
						fmt.Fprintln(out, "Tecla desconhecida.")
						continue
					}
				}
				send("Entrada enviada.", "/processes/"+pid+"/input", map[string]string{"data": data})
			case "/stop":
				send("Processo encerrado.", "/processes/"+rest+"/stop", map[string]any{})
			default:
				select {
				case prompts <- map[string]string{"content": line, "request_id": uuid.NewString()}:
				default:
					fmt.Fprintln(out, "Fila de envio cheia; aguarde a confirmação das instruções pendentes.")
				}
			}
		case <-ticker.C:
			var snapshot server.Snapshot
			pollCtx, stopPoll := context.WithTimeout(ctx, 2*time.Second)
			err := client.Do(pollCtx, "GET", path, nil, &snapshot)
			stopPoll()
			if err != nil {
				if err.Error() != lastError {
					fmt.Fprintln(out, safeTerminal(err.Error()))
					lastError = err.Error()
				}
				continue
			}
			lastError = ""
			for _, m := range snapshot.Messages {
				if m.Role != "assistant" {
					continue
				}
				if m.Text != "" && seen[m.ID] != m.Text {
					previous := seen[m.ID]
					if strings.HasPrefix(m.Text, previous) {
						fmt.Fprint(out, safeTerminal(strings.TrimPrefix(m.Text, previous)))
					} else {
						fmt.Fprint(out, "\n"+safeTerminal(m.Text))
					}
					if m.Finished {
						fmt.Fprintln(out)
					}
					seen[m.ID] = m.Text
				}
			}
			for _, p := range snapshot.Permissions {
				if !permissionIDs[p.ID] {
					fmt.Fprintf(out, "\nPermissão %s: %s · %s · %v\n/approve %s ou /deny %s\n", p.ID, safeTerminal(p.ToolName), safeTerminal(p.Description), safeTerminal(fmt.Sprint(p.Params)), p.ID, p.ID)
					permissionIDs[p.ID] = true
				}
			}
			if snapshot.Question != nil && snapshot.Question.ID != questionID {
				q := snapshot.Question
				questionID = q.ID
				fmt.Fprintf(out, "\nPergunta %s: %s · %s\n/answer %s resposta\n", q.ID, safeTerminal(q.Question), safeTerminal(strings.Join(q.Choices, " | ")), q.ID)
			}
			for _, p := range snapshot.Processes {
				key := "process:" + p.ID
				if !p.Running && seen[key+":finished"] == "true" {
					continue
				}
				var screen tools.ProcessSnapshot
				pollCtx, stopPoll := context.WithTimeout(ctx, time.Second)
				err := client.Do(pollCtx, "GET", path+"/processes/"+p.ID, nil, &screen)
				stopPoll()
				if err != nil {
					continue
				}
				text := screen.Stdout + screen.Stderr
				if screen.PTY {
					text = screen.Screen
				}
				if seen[key] != text {
					fmt.Fprintf(out, "\nTerminal %s:\n%s\n", p.ID, safeTerminal(text))
					seen[key] = text
				}
				if !p.Running {
					seen[key+":finished"] = "true"
				}
			}
			if len(seen) > 512 {
				seen = map[string]string{}
			}
		}
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
