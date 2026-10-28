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
	Version string        `json:"version"`
	Checks  []doctorCheck `json:"checks"`
}

func newDoctorCommand() *cobra.Command {
	var jsonOutput, checkProvider, probeModel, ollamaStatus bool
	var profile, modelID string
	command := &cobra.Command{Use: "doctor", Short: "Diagnose configuration and execution readiness without starting an agent", Args: cobra.NoArgs}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		if probeModel && modelID == "" {
			return fmt.Errorf("--probe-model requires --model with an explicit configured model ID; this sends one real inference request and may incur costs")
		}
		cwd, _ := cmd.Flags().GetString("cwd")
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		cwd, err := filepath.Abs(cwd)
		if err != nil {
			return err
		}
		report := doctorReport{Version: version.Version, Checks: []doctorCheck{}}
		add := func(name, status, detail string) {
			report.Checks = append(report.Checks, doctorCheck{name, status, tools.RedactCredentials(detail)})
		}
		cfg, err := config.LoadOffline(cwd)
		if err != nil {
			// Config parsing errors can include unregistered literal keys; never echo them.
			add("configuration", "error", "Configuration could not be loaded. Check JSON syntax, environment references and provider settings; values are omitted.")
		} else {
			add("configuration", "ok", "Loaded; provider credentials and environment values are omitted.")
			if cfg.ValidateStartup() != nil {
				add("model", "warning", "No usable coder model configured; open provider settings to select one.")
			} else {
				add("model", "ok", "A coder model is configured; inference was not tested.")
			}
			for _, binary := range []string{"git", "rg", "go", "gofmt", "ruff", "prettier", "bwrap"} {
				if _, e := exec.LookPath(binary); e == nil {
					add("binary:"+binary, "ok", "Available on PATH.")
				} else {
					add("binary:"+binary, "warning", "Not found on PATH; dependent features are unavailable. Nothing was installed.")
				}
			}
			sandboxCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			sandbox, sandboxErr := shell.Command(sandboxCtx, cwd, "true")
			if sandboxErr == nil {
				sandbox.Stdout = nil
				sandbox.Stderr = nil
				sandboxErr = sandbox.Run()
			}
			cancel()
			if sandboxErr != nil {
				add("sandbox", "error", "Execution policy cannot start a command. Check Bubblewrap installation, user namespaces and security settings.")
			} else if cfg.Security.Sandbox == "none" {
				add("sandbox", "warning", "Commands run without OS isolation, as explicitly configured globally.")
			} else {
				add("sandbox", "ok", "A bounded command successfully started under the configured sandbox.")
			}
			if runtime.GOOS == "windows" {
				add("pty", "warning", "Interactive PTY is unavailable in this build.")
			} else {
				add("pty", "ok", "PTY implementation is included; terminal interaction is not started by doctor.")
			}
			keys := make([]string, 0, len(cfg.LSP))
			for key := range cfg.LSP {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				entry := cfg.LSP[key]
				if _, e := exec.LookPath(entry.Command); e != nil {
					add("lsp:"+key, "warning", "Configured executable is unavailable; the server was not started.")
				} else {
					add("lsp:"+key, "ok", "Executable available; server not started.")
				}
			}
			if checkProvider {
				ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
				var catalog []models.Model
				if profile != "" {
					catalog, err = config.ProfileModelCatalog(profile)(ctx)
				} else {
					selected, exists := models.SupportedModels[cfg.Agents[config.AgentCoder].Model]
					if !exists {
						err = fmt.Errorf("select a model or specify --profile")
					} else if selected.ProviderProfile != "" {
						catalog, err = config.ProfileModelCatalog(selected.ProviderProfile)(ctx)
					} else {
						catalog, err = config.ModelCatalog(selected.Provider)(ctx)
					}
				}
				cancel()
				if err != nil {
					add("provider-catalog", "error", "Catalog request failed; check the selected connection, authentication and availability. No inference was requested.")
				} else {
					add("provider-catalog", "ok", fmt.Sprintf("Catalog returned %d models. No inference was requested.", len(catalog)))
				}
			}
			if ollamaStatus {
				id := models.ModelID(modelID)
				if id == "" {
					id = cfg.Agents[config.AgentCoder].Model
				}
				selected, exists := models.SupportedModels[id]
				selected = config.ModelWithProviderDefaults(selected)
				connection, e := config.ProviderForModel(selected)
				if !exists || e != nil || selected.Provider != models.ProviderLocal || selected.API != "ollama" {
					add("ollama-allocation", "error", "Select a configured model using the native Ollama API.")
				} else {
					running, e := models.InspectOllama(cmd.Context(), connection.BaseURL, connection.APIKey)
					if e != nil {
						add("ollama-allocation", "error", e.Error())
					} else {
						found := false
						for _, entry := range running {
							if entry.Name == selected.APIModel || entry.Name == selected.APIModel+":latest" {
								found = true
								status := "ok"
								if entry.Context > 0 && entry.Context != selected.ContextWindow {
									status = "warning"
								}
								add("ollama-allocation", status, fmt.Sprintf("Configured context: %d; allocated: %d; placement: %s; model VRAM: %d bytes. No inference or model load requested. Free VRAM is unknown.", selected.ContextWindow, entry.Context, entry.Placement(), entry.VRAM))
							}
						}
						if !found {
							add("ollama-allocation", "warning", "Selected model is not loaded; nothing was loaded or downloaded.")
						}
					}
				}
			}
			if probeModel {
				fmt.Fprintln(cmd.ErrOrStderr(), "Sending one real inference request with an inert tool schema to the explicitly selected model; provider charges may apply. No tool will execute.")
				ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
				err := runModelProbe(ctx, models.ModelID(modelID))
				cancel()
				if err != nil {
					add("model-tool-probe", "error", "The model did not return the requested structured tool call, or the request failed. No tool was executed and no raw response was recorded.")
				} else {
					add("model-tool-probe", "ok", "The model returned the requested tool name and challenge arguments. No tool was executed; this is a capability check, not a quality benchmark.")
				}
			}
		}
		if jsonOutput {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(report); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Opcode %s — diagnostics\n", report.Version)
			for _, check := range report.Checks {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
			}
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
	command.Flags().BoolVar(&ollamaStatus, "ollama-status", false, "Opt in to GET /api/ps for actual model context and CPU/GPU allocation; no inference or model load")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Print a structured report without credentials")
	command.Flags().StringP("cwd", "c", "", "Workspace to diagnose")
	command.Flags().BoolVar(&checkProvider, "check-provider", false, "Opt in to a bounded metadata/catalog request; does not generate tokens")
	command.Flags().StringVar(&profile, "profile", "", "Provider profile ID for the optional catalog request")
	command.Flags().BoolVar(&probeModel, "probe-model", false, "Opt in to one real model/tool inference request (may incur costs; no tools execute)")
	command.Flags().StringVar(&modelID, "model", "", "Explicit configured model ID used only by --probe-model")
	return command
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
