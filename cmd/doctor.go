package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diagnostic"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/version"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type doctorReport struct {
	Version   string                       `json:"version"`
	Checks    []doctorCheck                `json:"checks"`
	Languages *tools.LanguageSupportReport `json:"languages,omitempty"`
}

type doctorOptions struct {
	jsonOutput, checkProvider, probeModel, ollamaStatus, languageSupport bool
	profile, modelID                                                     string
}

func newDoctorCommand() *cobra.Command {
	o := &doctorOptions{}
	command := &cobra.Command{Use: "doctor", Short: "Diagnose configuration and execution readiness without starting an agent", Args: cobra.NoArgs, RunE: o.run}
	command.Flags().BoolVar(&o.ollamaStatus, "ollama-status", false, "Opt in to GET /api/ps for actual model context and CPU/GPU allocation; no inference or model load")
	command.Flags().BoolVar(&o.jsonOutput, "json", false, "Print a structured report without credentials")
	command.Flags().StringP("cwd", "c", "", "Workspace to diagnose")
	command.Flags().BoolVar(&o.languageSupport, "languages", false, "Discover project languages and report parser/LSP/check readiness without installing or starting language servers")
	command.Flags().BoolVar(&o.checkProvider, "check-provider", false, "Opt in to a bounded metadata/catalog request; does not generate tokens")
	command.Flags().StringVar(&o.profile, "profile", "", "Provider profile ID for the optional catalog request")
	command.Flags().BoolVar(&o.probeModel, "probe-model", false, "Opt in to one real model/tool inference request (may incur costs; no tools execute)")
	command.Flags().StringVar(&o.modelID, "model", "", "Explicit configured model ID used only by --probe-model")
	return command
}

func (o *doctorOptions) run(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	if o.probeModel && o.modelID == "" {
		return fmt.Errorf("--probe-model requires --model with an explicit configured model ID; this sends one real inference request and may incur costs")
	}
	cwd, err := doctorWorkingDirectory(cmd)
	if err != nil {
		return err
	}
	report := doctorReport{Version: version.Version, Checks: []doctorCheck{}}
	cfg, err := config.LoadOffline(cwd)
	if err != nil {
		// Parsing errors can contain unregistered literal keys; never echo them.
		report.add("configuration", "error", "Configuration could not be loaded. Check JSON syntax, environment references and provider settings; values are omitted.")
	} else {
		o.inspect(cmd, cwd, cfg, &report)
	}
	if err := report.write(cmd, o.jsonOutput); err != nil {
		return err
	}
	if cmd.Context().Err() != nil {
		return cmd.Context().Err()
	}
	for _, check := range report.Checks {
		if check.Status == "error" {
			return fmt.Errorf("diagnostics found errors; see the report")
		}
	}
	return nil
}

func doctorWorkingDirectory(cmd *cobra.Command) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(cwd)
}

func (r *doctorReport) add(name, status, detail string) {
	r.Checks = append(r.Checks, doctorCheck{name, status, tools.RedactCredentials(detail)})
}

func (r doctorReport) write(cmd *cobra.Command, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(r)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Opcode %s — diagnostics\n", r.Version)
	for _, check := range r.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	return nil
}

func (o *doctorOptions) inspect(cmd *cobra.Command, cwd string, cfg *config.Config, report *doctorReport) {
	report.add("configuration", "ok", "Loaded; provider credentials and environment values are omitted.")
	if cfg.ValidateStartup() != nil {
		report.add("model", "warning", "No usable coder model configured; open provider settings to select one.")
	} else {
		report.add("model", "ok", "A coder model is configured; inference was not tested.")
	}
	report.inspectBinaries()
	report.inspectSandbox(cmd.Context(), cwd, cfg)
	report.inspectLSP(cfg)
	if o.languageSupport {
		report.inspectLanguages(cmd.Context())
	}
	if o.checkProvider {
		report.inspectCatalog(cmd.Context(), cfg, o.profile)
	}
	if o.ollamaStatus {
		report.inspectOllama(cmd.Context(), cfg, o.modelID)
	}
	if o.probeModel {
		report.probeModel(cmd, o.modelID)
	}
}

func (r *doctorReport) inspectBinaries() {
	for _, binary := range []string{"git", "rg", "go", "gofmt", "ruff", "prettier", "bwrap"} {
		if _, e := exec.LookPath(binary); e == nil {
			r.add("binary:"+binary, "ok", "Available on PATH.")
		} else {
			r.add("binary:"+binary, "warning", "Not found on PATH; dependent features are unavailable. Nothing was installed.")
		}
	}
}

func (r *doctorReport) inspectSandbox(ctx context.Context, cwd string, cfg *config.Config) {
	sandboxCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	sandbox, err := shell.Command(sandboxCtx, cwd, "true")
	if err == nil {
		sandbox.Stdout = nil
		sandbox.Stderr = nil
		err = sandbox.Run()
	}
	cancel()
	switch {
	case err != nil:
		r.add("sandbox", "error", "Execution policy cannot start a command. Check Bubblewrap installation, user namespaces and security settings.")
	case cfg.Security.Sandbox == "none":
		r.add("sandbox", "warning", "Commands run without OS isolation, as explicitly configured globally.")
	default:
		r.add("sandbox", "ok", "A bounded command successfully started under the configured sandbox.")
	}
	if runtime.GOOS == "windows" {
		r.add("pty", "warning", "Interactive PTY is unavailable in this build.")
	} else {
		r.add("pty", "ok", "PTY implementation is included; terminal interaction is not started by doctor.")
	}
}

func (r *doctorReport) inspectLSP(cfg *config.Config) {
	keys := make([]string, 0, len(cfg.LSP))
	for key := range cfg.LSP {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, e := exec.LookPath(cfg.LSP[key].Command); e != nil {
			r.add("lsp:"+key, "warning", "Configured executable is unavailable; the server was not started.")
		} else {
			r.add("lsp:"+key, "ok", "Executable available; server not started.")
		}
	}
}

func (r *doctorReport) inspectLanguages(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	inventory, err := tools.InspectLanguageSupport(ctx, true)
	cancel()
	if err != nil {
		r.add("languages", "warning", "Language discovery could not finish; source content was not inspected. Retry with an accessible workspace.")
		return
	}
	projectLanguages := inventory.Languages[:0]
	for _, language := range inventory.Languages {
		if language.ProjectFiles == 0 {
			continue
		}
		projectLanguages = append(projectLanguages, language)
		status, parser := "ok", "tested local syntax parser"
		if !language.LocalParser {
			status, parser = "warning", "text search; no local syntax parser"
		}
		serverNote := doctorLanguageServers(language.ConfiguredServers)
		r.add("language:"+language.Language, status, fmt.Sprintf("%d files; %s. LSP: %s. %d configured project checks; none executed.", language.ProjectFiles, parser, serverNote, len(language.ConfiguredChecks)))
	}
	inventory.Languages = projectLanguages
	r.Languages = &inventory
	if inventory.Truncated {
		r.add("languages", "warning", "Language discovery stopped at 2000 files; counts are partial. Nothing was installed or started.")
	}
}

func (r *doctorReport) inspectCatalog(ctx context.Context, cfg *config.Config, profile string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	catalog, err := doctorCatalog(ctx, cfg, profile)
	cancel()
	if err != nil {
		r.add("provider-catalog", "error", "Catalog request failed; check the selected connection, authentication and availability. No inference was requested.")
	} else {
		r.add("provider-catalog", "ok", fmt.Sprintf("Catalog returned %d models. No inference was requested.", len(catalog)))
	}
}

func doctorCatalog(ctx context.Context, cfg *config.Config, profile string) ([]models.Model, error) {
	if profile != "" {
		return config.ProfileModelCatalog(profile)(ctx)
	}
	selected, exists := models.SupportedModels[cfg.Agents[config.AgentCoder].Model]
	if !exists {
		return nil, fmt.Errorf("select a model or specify --profile")
	}
	if selected.ProviderProfile != "" {
		return config.ProfileModelCatalog(selected.ProviderProfile)(ctx)
	}
	return config.ModelCatalog(selected.Provider)(ctx)
}

func (r *doctorReport) inspectOllama(ctx context.Context, cfg *config.Config, modelID string) {
	id := models.ModelID(modelID)
	if id == "" {
		id = cfg.Agents[config.AgentCoder].Model
	}
	selected, exists := models.SupportedModels[id]
	selected = config.ModelWithProviderDefaults(selected)
	connection, err := config.ProviderForModel(selected)
	if !exists || err != nil || selected.Provider != models.ProviderLocal || selected.API != "ollama" {
		r.add("ollama-allocation", "error", "Select a configured model using the native Ollama API.")
		return
	}
	running, err := models.InspectOllama(ctx, connection.BaseURL, connection.APIKey)
	if err != nil {
		r.add("ollama-allocation", "error", err.Error())
		return
	}
	found := false
	for _, entry := range running {
		if entry.Name != selected.APIModel && entry.Name != selected.APIModel+":latest" {
			continue
		}
		found = true
		status := "ok"
		if entry.Context > 0 && entry.Context != selected.ContextWindow {
			status = "warning"
		}
		r.add("ollama-allocation", status, fmt.Sprintf("Configured context: %d; allocated: %d; placement: %s; model VRAM: %d bytes. No inference or model load requested. Free VRAM is unknown.", selected.ContextWindow, entry.Context, entry.Placement(), entry.VRAM))
	}
	if !found {
		r.add("ollama-allocation", "warning", "Selected model is not loaded; nothing was loaded or downloaded.")
	}
}

func (r *doctorReport) probeModel(cmd *cobra.Command, modelID string) {
	fmt.Fprintln(cmd.ErrOrStderr(), "Sending one real inference request with an inert tool schema to the explicitly selected model; provider charges may apply. No tool will execute.")
	ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
	err := runModelProbe(ctx, models.ModelID(modelID))
	cancel()
	if err != nil {
		r.add("model-tool-probe", "error", "The model did not return the requested structured tool call, or the request failed. No tool was executed and no raw response was recorded.")
	} else {
		r.add("model-tool-probe", "ok", "The model returned the requested tool name and challenge arguments. No tool was executed; this is a capability check, not a quality benchmark.")
	}
}

func runModelProbe(ctx context.Context, id models.ModelID) error {
	model, ok := models.SupportedModels[id]
	if !ok {
		return fmt.Errorf("configured model not found")
	}
	model = config.ModelWithProviderDefaults(model)
	settings, err := config.ProviderForModel(model)
	if err != nil {
		return err
	}
	return diagnostic.ProbeModel(ctx, model, settings)
}
func init() { rootCmd.AddCommand(newDoctorCommand()) }

func doctorLanguageServers(serversState []tools.LanguageServerState) string {
	servers := []string{}
	for _, server := range serversState {
		state := "missing executable"
		if server.ExecutableAvailable {
			state = "executable available"
		}
		if server.Disabled {
			state = "disabled"
		}
		servers = append(servers, server.Name+" ("+state+")")
	}
	serverNote := strings.Join(servers, ", ")
	if serverNote == "" {
		serverNote = "not configured; consult languages.suggested_servers in --json and configure explicitly"
	}
	return serverNote
}
