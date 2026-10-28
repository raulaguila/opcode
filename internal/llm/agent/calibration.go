package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/logging"
)

const calibrationTTL = 24 * time.Hour
const calibrationLimit = 65536

type contextCalibration struct {
	Window  int64 `json:"window"`
	Expires int64 `json:"expires"`
}
type ContextCalibrationControl interface {
	RecalibrateContext(context.Context, string) (ContextStatus, error)
}

func calibrationLocation(model models.Model) (string, string) {
	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		return "", ""
	}
	connection, err := config.ProviderForModel(model)
	if err != nil {
		return "", ""
	}
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", config.WorkingDirectory(), model.Provider, model.ProviderProfile, connection.BaseURL, model.APIModel)
	return filepath.Join(cfg.Data.Directory, "context-calibration.json"), fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}
func calibrationRecords(path string) (map[string]contextCalibration, error) {
	entries := map[string]contextCalibration{}
	err := localstate.Read(path, calibrationLimit, &entries)
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > 128 {
		return nil, fmt.Errorf("context calibration exceeds 128 entries")
	}
	now := time.Now().Unix()
	for k, v := range entries {
		if len(k) != 64 || v.Expires <= now || v.Expires > now+int64(calibrationTTL/time.Second) || v.Window < 4096 {
			delete(entries, k)
		}
	}
	return entries, nil
}
func learnedContext(model models.Model) int64 {
	path, key := calibrationLocation(model)
	if path == "" {
		return 0
	}
	unlock := localstate.Lock(path)
	defer unlock()
	entries, err := calibrationRecords(path)
	if err != nil {
		logging.Warn("Context calibration unavailable", "error", err)
		return 0
	}
	return entries[key].Window
}
func saveContextCalibration(model models.Model, window int64) error {
	path, key := calibrationLocation(model)
	if path == "" {
		return fmt.Errorf("context calibration storage unavailable")
	}
	unlock := localstate.Lock(path)
	defer unlock()
	entries, err := calibrationRecords(path)
	if err != nil {
		return err
	}
	if window == 0 {
		delete(entries, key)
	} else {
		if len(entries) >= 128 {
			oldest := ""
			for k, v := range entries {
				if oldest == "" || v.Expires < entries[oldest].Expires {
					oldest = k
				}
			}
			delete(entries, oldest)
		}
		entries[key] = contextCalibration{Window: window, Expires: time.Now().Add(calibrationTTL).Unix()}
	}
	return localstate.Write(path, entries, calibrationLimit)
}
func contextFailure(err error, model models.Model) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrContextLimit) {
		return false
	}
	text := strings.ToLower(err.Error())
	var httpError *provider.HTTPError
	if errors.As(err, &httpError) && httpError.StatusCode != 400 && httpError.StatusCode != 413 && httpError.StatusCode != 500 {
		return false
	}
	for _, term := range []string{"context_length_exceeded", "maximum context length", "context window", "context length exceeded", "input is too long", "prompt too long", "too many tokens"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	if model.Provider == models.ProviderLocal {
		for _, term := range []string{"out of memory", "requires more system memory", "unable to allocate", "cuda error: out of memory"} {
			if strings.Contains(text, term) {
				return true
			}
		}
	}
	return false
}
func (a *agent) adaptContext(run *generationRun, err error) bool {
	model := run.provider.Model()
	if !contextFailure(err, model) || model.ContextWindow <= 4096 {
		return false
	}
	controller, ok := run.provider.(provider.ContextWindowController)
	if !ok {
		return false
	}
	window := max(int64(4096), model.ContextWindow/2)
	next, resizeErr := controller.WithContextWindow(window)
	if resizeErr != nil {
		return false
	}
	run.provider = next
	a.contextMu.Lock()
	a.contextStatuses = nil
	a.contextMu.Unlock()
	if saveErr := saveContextCalibration(model, window); saveErr != nil {
		logging.Warn("Could not persist learned context limit", "error", saveErr)
	}
	return true
}
func (a *agent) RecalibrateContext(ctx context.Context, session string) (ContextStatus, error) {
	if err := ctx.Err(); err != nil {
		return ContextStatus{}, err
	}
	a.runMu.Lock()
	if a.IsBusy() {
		a.runMu.Unlock()
		return ContextStatus{}, ErrSessionBusy
	}
	model := a.provider.Model()
	if err := saveContextCalibration(model, 0); err != nil {
		a.runMu.Unlock()
		return ContextStatus{}, err
	}
	cfg := config.Get()
	if cfg == nil {
		a.runMu.Unlock()
		return ContextStatus{}, fmt.Errorf("configuration unavailable")
	}
	agentConfig, ok := cfg.Agents[a.agentName]
	if !ok {
		a.runMu.Unlock()
		return ContextStatus{}, fmt.Errorf("configured agent unavailable")
	}
	configured, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		a.runMu.Unlock()
		return ContextStatus{}, fmt.Errorf("configured model unavailable")
	}
	next, err := createAgentProviderFor(a.agentName, agentConfig, configured)
	if err == nil {
		a.provider = next
		a.contextMu.Lock()
		a.contextStatuses = nil
		a.contextMu.Unlock()
	}
	a.runMu.Unlock()
	if err != nil {
		return ContextStatus{}, err
	}
	return a.ContextStatus(ctx, session)
}
