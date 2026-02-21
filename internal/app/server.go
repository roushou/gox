package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/roushou/gox/internal/adapter/mcp"
	"github.com/roushou/gox/internal/adapter/roblox"
	"github.com/roushou/gox/internal/app/actions"
	"github.com/roushou/gox/internal/domain/action"
	"github.com/roushou/gox/internal/domain/policy"
	"github.com/roushou/gox/internal/domain/runlog"
	"github.com/roushou/gox/internal/platform/config"
	"github.com/roushou/gox/internal/platform/identity"
	"github.com/roushou/gox/internal/platform/logging"
	"github.com/roushou/gox/internal/usecase"
)

type App struct {
	cfg          config.Config
	logger       *slog.Logger
	mcpServer    *mcp.Server
	robloxClient *roblox.Client
	relayServer  *roblox.RelayHTTPServer
	runlogs      runlog.Store
	cleanup      []func()
}

func New(cfg config.Config, reader io.Reader, writer io.Writer) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}

	logger := logging.New(cfg.LogLevel)
	transport, relayServer := buildRobloxTransport(cfg, logger)
	robloxClient := roblox.NewClient(transport, logger, cfg.BridgeTimeout)
	registry := action.NewInMemoryRegistry()
	if err := registerActions(cfg, registry, robloxClient); err != nil {
		return nil, err
	}

	runlogs, runlogCleanup := buildRunlogStore()
	policyEvaluator, err := buildPolicyEvaluator(cfg)
	if err != nil {
		return nil, err
	}
	executor := usecase.NewExecuteActionService(
		registry,
		policyEvaluator,
		runlogs,
		logger,
		cfg.ActionTimeout,
		identity.NewID,
	)

	mcpServer := mcp.NewServerWithRuntimeConfig(
		cfg.ServerName,
		reader,
		writer,
		logger,
		registry,
		executor,
		runlogs,
		buildWorkflowRuntimeConfig(cfg),
		buildMCPServerRuntimeConfig(cfg),
	)
	return &App{
		cfg:          cfg,
		logger:       logger,
		mcpServer:    mcpServer,
		robloxClient: robloxClient,
		relayServer:  relayServer,
		runlogs:      runlogs,
		cleanup:      []func(){runlogCleanup},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("gox starting", "server_name", a.cfg.ServerName)
	defer func() {
		_ = a.robloxClient.Close()
		for _, cleanup := range a.cleanup {
			cleanup()
		}
	}()

	var relayErrCh chan error
	if a.relayServer != nil {
		relayErrCh = make(chan error, 1)
		go func() {
			relayErrCh <- a.relayServer.Start()
		}()
		// Fail fast if the relay cannot start (for example, port already bound).
		select {
		case err := <-relayErrCh:
			if err != nil {
				return fmt.Errorf("start bridge relay server: %w", err)
			}
			return fmt.Errorf("start bridge relay server: exited unexpectedly")
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.cfg.BridgeEnabled {
		go a.robloxClient.StartHealthChecks(ctx, 15*time.Second)
	}
	if a.cfg.RunlogRetentionMaxAge > 0 {
		go a.pruneRunlogs(ctx, a.cfg.RunlogRetentionMaxAge)
	}
	serveErr := a.mcpServer.Serve(ctx)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer drainCancel()
	if err := a.mcpServer.WorkflowRuntime().Close(drainCtx); err != nil {
		a.logger.Warn("workflow drain incomplete", "error", err)
	}
	if a.relayServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.relayServer.Shutdown(shutdownCtx)
		if relayErrCh != nil {
			select {
			case err := <-relayErrCh:
				if err != nil {
					a.logger.Error("bridge relay server failed", "error", err)
				}
			default:
			}
		}
	}
	if serveErr != nil {
		return fmt.Errorf("mcp serve: %w", serveErr)
	}
	a.logger.Info("gox stopped")
	return nil
}

func (a *App) pruneRunlogs(ctx context.Context, maxAge time.Duration) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().UTC().Add(-maxAge)
			n, err := a.runlogs.DeleteOlderThan(ctx, cutoff)
			if err != nil {
				a.logger.Error("runlog pruning failed", "error", err)
				continue
			}
			if n > 0 {
				a.logger.Info("pruned old runlog records", "deleted", n)
			}
		}
	}
}

func registerActions(cfg config.Config, registry action.Registry, robloxClient *roblox.Client) error {
	if err := registry.Register(actions.NewPingAction()); err != nil {
		return fmt.Errorf("register core.ping: %w", err)
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := registry.Register(actions.NewWorkspaceStatsAction(root)); err != nil {
		return fmt.Errorf("register project.workspace_stats: %w", err)
	}
	if cfg.BridgeEnabled {
		if err := registry.Register(actions.NewBridgePingAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.bridge_ping: %w", err)
		}
		if err := registry.Register(actions.NewScriptCreateAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.script_create: %w", err)
		}
		if err := registry.Register(actions.NewScriptUpdateAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.script_update: %w", err)
		}
		if err := registry.Register(actions.NewScriptDeleteAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.script_delete: %w", err)
		}
		if err := registry.Register(actions.NewScriptGetSourceAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.script_get_source: %w", err)
		}
		if err := registry.Register(actions.NewScriptExecuteAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.script_execute: %w", err)
		}
		if err := registry.Register(actions.NewInstanceCreateAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_create: %w", err)
		}
		if err := registry.Register(actions.NewInstanceSetPropertyAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_set_property: %w", err)
		}
		if err := registry.Register(actions.NewInstanceDeleteAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_delete: %w", err)
		}
		if err := registry.Register(actions.NewInstanceGetAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_get: %w", err)
		}
		if err := registry.Register(actions.NewInstanceListChildrenAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_list_children: %w", err)
		}
		if err := registry.Register(actions.NewInstanceFindAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.instance_find: %w", err)
		}
		if err := registry.Register(actions.NewSceneSnapshotAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.scene_snapshot: %w", err)
		}
		if err := registry.Register(actions.NewScenePlanAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.scene_plan: %w", err)
		}
		if err := registry.Register(actions.NewSceneApplyAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.scene_apply: %w", err)
		}
		if err := registry.Register(actions.NewSceneValidateAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.scene_validate: %w", err)
		}
		if err := registry.Register(actions.NewSceneCaptureAction(robloxClient)); err != nil {
			return fmt.Errorf("register roblox.scene_capture: %w", err)
		}
	}
	return nil
}

func buildRobloxTransport(cfg config.Config, logger *slog.Logger) (roblox.Transport, *roblox.RelayHTTPServer) {
	if !cfg.BridgeEnabled {
		return nil, nil
	}
	opts := roblox.TCPTransportOptions{
		AuthToken:   cfg.BridgeAuthToken,
		RequireAuth: cfg.BridgeAuth,
		ClientName:  cfg.ServerName,
	}

	switch cfg.BridgeNetwork {
	case "relay-http":
		hub := roblox.NewRelayHub(cfg.BridgeAuthToken)
		transport := roblox.NewRelayTransport(hub, opts)
		relayServer := roblox.NewRelayHTTPServer(cfg.BridgeRelayAddr, hub, cfg.BridgeAuthToken, logger)
		return transport, relayServer
	default:
		return roblox.NewTCPTransport(cfg.BridgeNetwork, cfg.BridgeAddress, cfg.BridgeTimeout, opts), nil
	}
}

func buildRunlogStore() (runlog.Store, func()) {
	return runlog.NewInMemoryStore(), func() {}
}

func buildWorkflowRuntimeConfig(cfg config.Config) mcp.WorkflowRuntimeConfig {
	return mcp.WorkflowRuntimeConfig{
		MaxConcurrentRuns:  cfg.WorkflowMaxConcurrentRuns,
		MaxSteps:           cfg.WorkflowMaxSteps,
		MaxLogsPerRun:      cfg.WorkflowMaxLogsPerRun,
		DefaultStepTimeout: cfg.WorkflowDefaultStepTimeout,
		MaxStepTimeout:     cfg.WorkflowMaxStepTimeout,
		MaxAttempts:        cfg.WorkflowMaxAttempts,
		MaxBackoff:         cfg.WorkflowMaxBackoff,
		RetentionMaxRuns:   cfg.WorkflowRetentionMaxRuns,
		RetentionTTL:       cfg.WorkflowRetentionTTL,
	}
}

func buildMCPServerRuntimeConfig(cfg config.Config) mcp.ServerRuntimeConfig {
	return mcp.ServerRuntimeConfig{
		MaxPayloadBytes: cfg.MCPMaxPayloadBytes,
	}
}

func buildPolicyEvaluator(cfg config.Config) (policy.Evaluator, error) {
	fallbackMode := policy.Mode(strings.TrimSpace(cfg.PolicyMode))
	ruleSet := policy.RuleSet{Mode: fallbackMode}
	if cfg.PolicyRulesPath != "" {
		loadedRuleSet, err := policy.LoadRuleSetFile(cfg.PolicyRulesPath, fallbackMode)
		if err != nil {
			return nil, err
		}
		ruleSet = loadedRuleSet
	} else if fallbackMode == policy.ModeDenyByDefault {
		// Secure defaults with minimal diagnostics access when no policy file is supplied.
		ruleSet.Rules = []policy.Rule{
			{
				Name:     "allow core ping",
				ActionID: "core.ping",
				Effect:   policy.EffectAllow,
				Priority: 1000,
			},
			{
				Name:     "allow workspace stats",
				ActionID: "project.workspace_stats",
				Effect:   policy.EffectAllow,
				Priority: 1000,
			},
		}
		ruleSet.DefaultDenyReason = "action not explicitly allowed by policy"
	}
	return policy.NewRuleEvaluator(ruleSet)
}
