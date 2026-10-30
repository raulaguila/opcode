package app

import (
	"context"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/spf13/viper"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

type delegateProbe struct {
	agent.Service
	*pubsub.Broker[agent.AgentEvent]
	busy, closed bool
	model        models.Model
}

func (p *delegateProbe) Model() models.Model { return p.model }
func (p *delegateProbe) IsBusy() bool        { return p.busy }
func (p *delegateProbe) Close() error        { p.closed = true; p.Broker.Shutdown(); return nil }
func TestDeferredCoderIsSafeBeforeSetupAndKeepsSubscriptionsAcrossReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := newConfigurableCoder(ctx, &App{})
	defer proxy.Close()
	require.Empty(t, proxy.Model().ID)
	require.False(t, proxy.IsBusy())
	require.False(t, proxy.IsSessionBusy("session"))
	_, _, err := proxy.Submit(ctx, "session", "hello")
	require.ErrorContains(t, err, "configure a provider")
	events := proxy.Subscribe(ctx)
	first := &delegateProbe{Broker: pubsub.NewBroker[agent.AgentEvent](), model: models.Model{ID: "first"}}
	require.NoError(t, proxy.bind(first))
	require.Equal(t, models.ModelID("first"), proxy.Model().ID)
	first.PublishEvent(pubsub.Event[agent.AgentEvent]{Type: pubsub.UpdatedEvent, Payload: agent.AgentEvent{Progress: "first"}, ReconcileRequired: true})
	select {
	case event := <-events:
		require.Equal(t, "first", event.Payload.Progress)
		require.True(t, event.ReconcileRequired, "forwarders must preserve the resynchronization signal")
	case <-time.After(time.Second):
		t.Fatal("initial delegate event was not forwarded")
	}
	next := &delegateProbe{Broker: pubsub.NewBroker[agent.AgentEvent](), model: models.Model{ID: "next"}}
	require.NoError(t, proxy.bind(next))
	require.True(t, first.closed)
	require.Equal(t, models.ModelID("next"), proxy.Model().ID)
	next.Publish(pubsub.UpdatedEvent, agent.AgentEvent{Progress: "next"})
	select {
	case event := <-events:
		require.Equal(t, "next", event.Payload.Progress)
	case <-time.After(time.Second):
		t.Fatal("replacement event was not forwarded through the existing subscription")
	}
	require.NoError(t, proxy.Close())
	require.True(t, next.closed)
}
func TestDeferredCoderRefusesToReplaceBusyDelegate(t *testing.T) {
	proxy := newConfigurableCoder(context.Background(), &App{})
	defer proxy.Close()
	first := &delegateProbe{Broker: pubsub.NewBroker[agent.AgentEvent](), model: models.Model{ID: "first"}}
	require.NoError(t, proxy.bind(first))
	first.busy = true
	next := &delegateProbe{Broker: pubsub.NewBroker[agent.AgentEvent](), model: models.Model{ID: "next"}}
	require.ErrorIs(t, proxy.bind(next), agent.ErrSessionBusy)
	require.False(t, first.closed)
	require.True(t, next.closed)
	require.Equal(t, models.ModelID("first"), proxy.Model().ID)
}

func (p *delegateProbe) Subscribe(ctx context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	return p.Broker.Subscribe(ctx)
}

func TestReconfiguringProviderKeepsSharedCommandsAliveUntilAppShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix shell")
	}
	if config.Get() == nil {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("LOCAL_ENDPOINT", "")
		file := filepath.Join(t.TempDir(), "settings.json")
		require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
		viper.SetConfigFile(file)
		_, err := config.Load(t.TempDir(), false)
		require.NoError(t, err)
	}
	cfg := config.Get()
	old := *cfg
	registered := maps.Clone(models.SupportedModels)
	t.Cleanup(func() { *cfg = old; models.SupportedModels = registered })
	root := t.TempDir()
	selected := models.Model{ID: "local.ownership", Name: "Ownership", Provider: models.ProviderLocal, APIModel: "ownership", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}
	models.SupportedModels[selected.ID] = selected
	*cfg = config.Config{WorkingDir: root, Data: config.Data{Directory: filepath.Join(root, "state")}, Runtime: (config.RuntimeConfig{}).Defaults(), Security: config.SecurityConfig{Sandbox: "none"}, Shell: config.ShellConfig{Path: "/bin/sh"}, Providers: map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://first.invalid/v1", API: "ollama"}}, Models: map[models.ModelID]models.Model{selected.ID: selected}, Agents: map[config.AgentName]config.Agent{}}
	for _, name := range []config.AgentName{config.AgentCoder, config.AgentTitle, config.AgentSummarizer, config.AgentTask} {
		cfg.Agents[name] = config.Agent{Model: selected.ID, MaxTokens: 2048}
	}
	conn, err := db.Connect()
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	application, err := New(ctx, conn)
	require.NoError(t, err)
	defer application.Shutdown()
	scope := "manager-owner"
	application.Permissions.AutoApproveSession(scope)
	processCtx := context.WithValue(ctx, tools.SessionIDContextKey, scope)
	background, err := application.Processes.Start(processCtx, application.Permissions, tools.ProcessParams{Command: "printf OWNER_READY; sleep 30", Timeout: 5000})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, err := application.Processes.Snapshot(scope, background.ID)
		return err == nil && snapshot.Running && strings.Contains(snapshot.Stdout, "OWNER_READY")
	}, time.Second, 10*time.Millisecond)
	profile := cfg.Providers[models.ProviderLocal]
	profile.BaseURL = "http://second.invalid/v1"
	cfg.Providers[models.ProviderLocal] = profile
	_, _, err = application.CoderAgent.(agent.ExtensionControl).InvokeSkill(ctx, "session", "missing", "")
	require.ErrorContains(t, err, "provider settings changed", "skills must not bypass a stale provider binding")
	require.ErrorContains(t, application.CoderAgent.Summarize(ctx, "session"), "provider settings changed", "summaries must not use stale auxiliary credentials")
	require.NoError(t, application.ConfigureCoderAgent())
	require.Equal(t, selected.ID, application.CoderAgent.Model().ID)
	snapshot, err := application.Processes.Snapshot(scope, background.ID)
	require.NoError(t, err)
	require.True(t, snapshot.Running, "replacing the provider must preserve a user-started terminal process")
	next, err := application.Processes.Start(processCtx, application.Permissions, tools.ProcessParams{Command: "printf COMMAND_AFTER_REBUILD", Timeout: 2000})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, err := application.Processes.Snapshot(scope, next.ID)
		return err == nil && !snapshot.Running && snapshot.ExitCode == 0 && strings.Contains(snapshot.Stdout, "COMMAND_AFTER_REBUILD")
	}, time.Second, 10*time.Millisecond)
	application.Shutdown()
	snapshot, err = application.Processes.Snapshot(scope, background.ID)
	require.NoError(t, err)
	require.False(t, snapshot.Running, "the application remains the owner responsible for process shutdown")
}
