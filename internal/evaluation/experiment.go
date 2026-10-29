package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/localstate"
)

// ExperimentManifest contains references, not credentials. Every application
// receives the same suite and requested conditions in fresh workspaces.
type ExperimentManifest struct {
	Version        int                     `json:"version"`
	Name           string                  `json:"name"`
	Suite          string                  `json:"suite,omitempty"`
	SuiteSHA256    string                  `json:"suite_sha256"`
	ProviderConfig string                  `json:"provider_config"`
	Conditions     SharedConditions        `json:"conditions"`
	Repeats        int                     `json:"repeats"`
	Cases          []string                `json:"cases"`
	Applications   []ExperimentApplication `json:"applications"`
}

type SharedConditions struct {
	APIModel      string   `json:"api_model"`
	Protocol      string   `json:"protocol"`
	ContextWindow int64    `json:"context_window"`
	OutputReserve int64    `json:"output_reserve"`
	Temperature   *float64 `json:"temperature"`
}

type ExperimentApplication struct {
	Name    string `json:"name"`
	Adapter string `json:"adapter,omitempty"`
}

type Experiment struct {
	Manifest    ExperimentManifest
	Suite       Suite
	SuiteRaw    []byte
	SHA256      string
	adapters    []*Adapter
	providerRaw []byte
}

type ExperimentRun struct {
	Application string `json:"application"`
	Status      string `json:"status"`
	Report      string `json:"report"`
	Error       string `json:"error,omitempty"`
}

type ExperimentReport struct {
	Version        int              `json:"version"`
	Mode           string           `json:"mode"`
	Name           string           `json:"name"`
	ManifestSHA256 string           `json:"manifest_sha256"`
	SuiteSHA256    string           `json:"suite_sha256"`
	Conditions     SharedConditions `json:"conditions"`
	Repeats        int              `json:"repeats"`
	Cases          []string         `json:"cases"`
	Applications   []ExperimentRun  `json:"applications"`
	Comparison     *Comparison      `json:"comparison,omitempty"`
	Notice         string           `json:"notice"`
}

func criterionDigest(criteria []Criterion) string {
	raw, _ := json.Marshal(criteria)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func LoadExperiment(path string) (*Experiment, error) {
	raw, err := readBounded(path, 64<<10)
	if err != nil {
		return nil, fmt.Errorf("cannot read experiment manifest")
	}
	var m ExperimentManifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil {
		return nil, fmt.Errorf("invalid experiment manifest JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("experiment manifest requires exactly one document")
	}
	if m.Version != 1 || m.Name == "" || len(m.Name) > 64 || strings.Trim(m.Name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.") != "" {
		return nil, fmt.Errorf("experiment needs version 1 and an ASCII name")
	}
	c := m.Conditions
	if c.APIModel == "" || len(c.APIModel) > 256 || strings.ContainsAny(c.APIModel, "\r\n\x00") || !contains([]string{"ollama", "openai", "chat_completions", "responses", "anthropic", "gemini"}, c.Protocol) || c.ContextWindow < 4096 || c.ContextWindow > 2000000 || c.OutputReserve < 1 || c.OutputReserve > c.ContextWindow/2 {
		return nil, fmt.Errorf("explicit model/protocol/context and output reserve are required")
	}
	// Native sampling is controlled only at zero in the current evaluation
	// adapters. Never silently convert a requested nonzero experiment.
	if c.Temperature == nil || math.IsNaN(*c.Temperature) || math.IsInf(*c.Temperature, 0) || *c.Temperature != 0 {
		return nil, fmt.Errorf("experiment temperature must explicitly be 0; other sampling is not controlled by these adapters")
	}
	if m.Repeats < 1 || m.Repeats > 3 || len(m.Cases) == 0 || len(m.Cases) > 32 || len(m.Applications) < 2 || len(m.Applications) > 3 || m.ProviderConfig == "" {
		return nil, fmt.Errorf("experiment requires 1–3 repeats, explicit cases, a provider config and 2–3 applications")
	}
	base := filepath.Dir(path)
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	m.Suite, m.ProviderConfig = resolve(m.Suite), resolve(m.ProviderConfig)
	suite, suiteRaw, err := Load(m.Suite)
	if err != nil {
		return nil, fmt.Errorf("cannot load experiment suite: %w", err)
	}
	sum := sha256.Sum256(suiteRaw)
	if len(m.SuiteSHA256) != 64 || m.SuiteSHA256 != hex.EncodeToString(sum[:]) {
		return nil, fmt.Errorf("experiment suite_sha256 does not match the fixture input")
	}
	known, selected := map[string]bool{}, map[string]bool{}
	for _, v := range suite.Cases {
		known[v.ID] = true
	}
	for _, id := range m.Cases {
		if !known[id] || selected[id] {
			return nil, fmt.Errorf("experiment case IDs must exist and be unique")
		}
		selected[id] = true
	}
	providerRaw, err := readBounded(m.ProviderConfig, 2<<20)
	if err != nil {
		return nil, fmt.Errorf("cannot read experiment provider config")
	}
	if err := verifyExperimentProvider(providerRaw, c); err != nil {
		return nil, err
	}
	x := &Experiment{Manifest: m, Suite: suite, SuiteRaw: suiteRaw, providerRaw: providerRaw}
	digest := sha256.Sum256(raw)
	x.SHA256 = hex.EncodeToString(digest[:])
	apps := map[string]bool{}
	for i, app := range m.Applications {
		if !contains([]string{"opcode", "codaro", "crush"}, app.Name) || apps[app.Name] {
			return nil, fmt.Errorf("experiment applications must be distinct opcode/codaro/crush names")
		}
		apps[app.Name] = true
		if app.Adapter == "" {
			if app.Name != "opcode" {
				return nil, fmt.Errorf("external experiment application requires an explicit adapter")
			}
			x.adapters = append(x.adapters, nil)
			continue
		}
		x.Manifest.Applications[i].Adapter = resolve(app.Adapter)
		a, err := LoadAdapter(x.Manifest.Applications[i].Adapter)
		if err != nil || a.Name != app.Name {
			return nil, fmt.Errorf("experiment adapter must match its application")
		}
		x.adapters = append(x.adapters, a)
	}
	return x, nil
}

func verifyExperimentProvider(raw []byte, expected SharedConditions) error {
	var c struct {
		Agents map[string]struct {
			Model string `json:"model"`
		} `json:"agents"`
		Models map[string]struct {
			APIModel string `json:"api_model"`
			API      string `json:"api"`
			Provider string `json:"provider"`
			Profile  string `json:"provider_profile"`
		} `json:"models"`
		Providers map[string]struct {
			API string `json:"api"`
		} `json:"providers"`
		Profiles map[string]struct {
			API string `json:"api"`
		} `json:"providerProfiles"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return fmt.Errorf("invalid experiment provider config")
	}
	model, ok := c.Models[c.Agents["coder"].Model]
	if !ok || model.APIModel != expected.APIModel {
		return fmt.Errorf("experiment API model differs from the provider's explicit coder model")
	}
	protocol := model.API
	if protocol == "" {
		if model.Profile != "" {
			protocol = c.Profiles[model.Profile].API
		} else {
			protocol = c.Providers[model.Provider].API
		}
	}
	if protocol != expected.Protocol {
		return fmt.Errorf("experiment protocol differs from the explicit provider/model protocol")
	}
	return nil
}

func (x *Experiment) Run(ctx context.Context, executable, outputDirectory string, live, allowExecution bool) (ExperimentReport, error) {
	m := x.Manifest
	r := ExperimentReport{Version: 1, Mode: "experiment_plan", Name: m.Name, ManifestSHA256: x.SHA256, SuiteSHA256: m.SuiteSHA256, Conditions: m.Conditions, Repeats: m.Repeats, Cases: m.Cases, Notice: "Requested conditions are shared; unknown observations stay unavailable. This is not a general application ranking."}
	for _, app := range m.Applications {
		r.Applications = append(r.Applications, ExperimentRun{Application: app.Name, Status: "planned", Report: app.Name + ".json"})
	}
	if !live {
		return r, nil
	}
	if outputDirectory == "" || executable == "" {
		return r, fmt.Errorf("live experiment needs an output directory and executable")
	}
	for _, c := range x.Suite.Cases {
		if contains(m.Cases, c.ID) && c.Mode == "execute" && !allowExecution {
			return r, fmt.Errorf("experiment execute cases require --allow-execution before any request")
		}
	}
	// Exclusive creation prevents reports from replacing prior evidence or an input.
	parent, err := localstate.OpenPrivateRoot(filepath.Dir(outputDirectory), false)
	if err != nil {
		return r, fmt.Errorf("experiment output parent must exist without symbolic links")
	}
	err = parent.Mkdir(filepath.Base(outputDirectory), 0700)
	parent.Close()
	if err != nil {
		return r, fmt.Errorf("experiment output directory must not already exist; its parent must exist")
	}
	// Keep every case/application on the exact provider configuration validated
	// at load time. This temporary private copy is never part of saved evidence.
	private, err := os.MkdirTemp("", "opcode-experiment-")
	if err != nil {
		return r, fmt.Errorf("cannot create private experiment configuration")
	}
	defer os.RemoveAll(private)
	providerPath := filepath.Join(private, "provider.json")
	if err := localstate.WriteBytes(providerPath, x.providerRaw); err != nil {
		return r, fmt.Errorf("cannot snapshot experiment provider configuration")
	}
	r.Mode = "live_experiment"
	if err := localstate.Write(filepath.Join(outputDirectory, "experiment.json"), r, 2<<20); err != nil {
		return r, err
	}
	paths := []string{}
	for i, app := range m.Applications {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		opts := Options{Shared: &m.Conditions, ManifestSHA256: x.SHA256, Experiment: m.Name, Adapter: x.adapters[i], ContextWindow: m.Conditions.ContextWindow, Repeats: m.Repeats, CaseIDs: m.Cases, Live: true, AllowExecution: allowExecution, ProviderConfig: providerPath, Executable: executable}
		var report Report
		var err error
		if a := x.adapters[i]; a != nil {
			if _, lookup := exec.LookPath(a.Command); lookup != nil {
				report = x.unavailableReport(app.Name)
			} else {
				report, err = Run(ctx, x.Suite, x.SuiteRaw, opts)
			}
		} else {
			report, err = Run(ctx, x.Suite, x.SuiteRaw, opts)
		}
		r.Applications[i].Status = "completed"
		if err != nil {
			r.Applications[i].Status = "failed"
			r.Applications[i].Error = "evaluation stopped; partial results are preserved"
		}
		if Failed(report) {
			r.Applications[i].Status = "failed_or_unavailable"
		}
		path := filepath.Join(outputDirectory, app.Name+".json")
		if err := localstate.Write(path, report, 2<<20); err != nil {
			return r, err
		}
		if len(report.Cases) > 0 {
			paths = append(paths, path)
		}
		if err := localstate.Write(filepath.Join(outputDirectory, "experiment.json"), r, 2<<20); err != nil {
			return r, err
		}
	}
	if len(paths) >= 2 {
		comparison, err := Compare(paths)
		if err != nil {
			comparison = Comparison{Version: 1, Differences: []string{err.Error()}, RankingUnavailable: []string{"incomplete or invalid application report"}}
		}
		r.Comparison = &comparison
		if len(paths) != len(m.Applications) {
			comparison.Comparable = false
			comparison.Rankable = false
			comparison.Differences = append(comparison.Differences, "application report is incomplete")
		}
	}
	if err := localstate.Write(filepath.Join(outputDirectory, "experiment.json"), r, 2<<20); err != nil {
		return r, err
	}
	return r, nil
}

func (x *Experiment) unavailableReport(app string) Report {
	m := x.Manifest
	r := Report{RequestedConditions: &m.Conditions, Version: 1, Mode: "live_provider", ManifestSHA256: x.SHA256, SuiteSHA256: m.SuiteSHA256, Repeats: m.Repeats, SelectedCases: m.Cases, Notice: "Application executable unavailable; no inference or observations fabricated."}
	for trial := 1; trial <= m.Repeats; trial++ {
		for _, c := range x.Suite.Cases {
			if !contains(m.Cases, c.ID) {
				continue
			}
			v := unavailableResult(c, "adapter executable unavailable")
			v.Application = app
			v.Trial = trial
			v.CriteriaSHA256 = criterionDigest(c.Criteria)
			r.Cases = append(r.Cases, v)
		}
	}
	return r
}
