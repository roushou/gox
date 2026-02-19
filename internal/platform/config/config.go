package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultServerName                 = "gox"
	defaultLogLevel                   = "info"
	defaultShutdownTimeout            = 10 * time.Second
	defaultActionTimeout              = 30 * time.Second
	defaultBridgeTimeout              = 5 * time.Second
	defaultBridgeNetwork              = "tcp"
	defaultBridgeAddress              = "127.0.0.1:47011"
	defaultBridgeRelayAddr            = "127.0.0.1:47012"
	defaultBridgeAuth                 = true
	defaultWorkflowMaxConcurrentRuns  = 8
	defaultWorkflowMaxSteps           = 64
	defaultWorkflowMaxLogsPerRun      = 256
	defaultWorkflowDefaultStepTimeout = 30 * time.Second
	defaultWorkflowMaxStepTimeout     = 5 * time.Minute
	defaultWorkflowMaxAttempts        = 5
	defaultWorkflowMaxBackoff         = 30 * time.Second
	defaultPolicyMode                 = "deny-by-default"
	defaultPolicyRulesPath            = ""
	defaultWorkflowRetentionMaxRuns   = 1000
	defaultWorkflowRetentionTTL       = 24 * time.Hour
	defaultRunlogRetentionMaxAge      = 72 * time.Hour
	defaultMCPMaxPayloadBytes         = 1 << 20
)

type Config struct {
	ServerName                 string
	LogLevel                   string
	ShutdownTimeout            time.Duration
	ActionTimeout              time.Duration
	BridgeTimeout              time.Duration
	BridgeEnabled              bool
	BridgeNetwork              string
	BridgeAddress              string
	BridgeRelayAddr            string
	BridgeAuthToken            string
	BridgeAuth                 bool
	RunlogRetentionMaxAge      time.Duration
	WorkflowMaxConcurrentRuns  int
	WorkflowMaxSteps           int
	WorkflowMaxLogsPerRun      int
	WorkflowDefaultStepTimeout time.Duration
	WorkflowMaxStepTimeout     time.Duration
	WorkflowMaxAttempts        int
	WorkflowMaxBackoff         time.Duration
	PolicyMode                 string
	PolicyRulesPath            string
	WorkflowRetentionMaxRuns   int
	WorkflowRetentionTTL       time.Duration
	MCPMaxPayloadBytes         int
}

func LoadFromEnv() (Config, error) {
	shutdownTimeout, err := parseEnvDuration("GOX_SHUTDOWN_TIMEOUT_SECONDS", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	actionTimeout, err := parseEnvDuration("GOX_ACTION_TIMEOUT_SECONDS", defaultActionTimeout)
	if err != nil {
		return Config{}, err
	}
	bridgeTimeout, err := parseEnvDuration("GOX_BRIDGE_TIMEOUT_SECONDS", defaultBridgeTimeout)
	if err != nil {
		return Config{}, err
	}
	bridgeEnabled, err := parseEnvBool("GOX_BRIDGE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	bridgeAuth, err := parseEnvBool("GOX_BRIDGE_AUTH", defaultBridgeAuth)
	if err != nil {
		return Config{}, err
	}
	maxConcurrentRuns, err := parseEnvInt("GOX_WORKFLOW_MAX_CONCURRENT_RUNS", defaultWorkflowMaxConcurrentRuns)
	if err != nil {
		return Config{}, err
	}
	maxSteps, err := parseEnvInt("GOX_WORKFLOW_MAX_STEPS", defaultWorkflowMaxSteps)
	if err != nil {
		return Config{}, err
	}
	maxLogsPerRun, err := parseEnvInt("GOX_WORKFLOW_MAX_LOGS_PER_RUN", defaultWorkflowMaxLogsPerRun)
	if err != nil {
		return Config{}, err
	}
	defaultStepTimeout, err := parseEnvDuration("GOX_WORKFLOW_DEFAULT_STEP_TIMEOUT_SECONDS", defaultWorkflowDefaultStepTimeout)
	if err != nil {
		return Config{}, err
	}
	maxStepTimeout, err := parseEnvDuration("GOX_WORKFLOW_MAX_STEP_TIMEOUT_SECONDS", defaultWorkflowMaxStepTimeout)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := parseEnvInt("GOX_WORKFLOW_MAX_ATTEMPTS", defaultWorkflowMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	maxBackoff, err := parseEnvDuration("GOX_WORKFLOW_MAX_BACKOFF_SECONDS", defaultWorkflowMaxBackoff)
	if err != nil {
		return Config{}, err
	}
	runlogRetentionMaxAge, err := parseEnvDurationAllowZero("GOX_RUNLOG_RETENTION_MAX_AGE_SECONDS", defaultRunlogRetentionMaxAge)
	if err != nil {
		return Config{}, err
	}
	retentionMaxRuns, err := parseEnvInt("GOX_WORKFLOW_RETENTION_MAX_RUNS", defaultWorkflowRetentionMaxRuns)
	if err != nil {
		return Config{}, err
	}
	retentionTTL, err := parseEnvDurationAllowZero("GOX_WORKFLOW_RETENTION_TTL_SECONDS", defaultWorkflowRetentionTTL)
	if err != nil {
		return Config{}, err
	}
	maxPayloadBytes, err := parseEnvInt("GOX_MCP_MAX_PAYLOAD_BYTES", defaultMCPMaxPayloadBytes)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		ServerName:                 getEnv("GOX_SERVER_NAME", defaultServerName),
		LogLevel:                   getEnv("GOX_LOG_LEVEL", defaultLogLevel),
		ShutdownTimeout:            shutdownTimeout,
		ActionTimeout:              actionTimeout,
		BridgeTimeout:              bridgeTimeout,
		BridgeEnabled:              bridgeEnabled,
		BridgeNetwork:              getEnv("GOX_BRIDGE_NETWORK", defaultBridgeNetwork),
		BridgeAddress:              getEnv("GOX_BRIDGE_ADDRESS", defaultBridgeAddress),
		BridgeRelayAddr:            getEnv("GOX_BRIDGE_RELAY_ADDRESS", defaultBridgeRelayAddr),
		BridgeAuthToken:            getEnv("GOX_BRIDGE_AUTH_TOKEN", ""),
		BridgeAuth:                 bridgeAuth,
		RunlogRetentionMaxAge:      runlogRetentionMaxAge,
		WorkflowMaxConcurrentRuns:  maxConcurrentRuns,
		WorkflowMaxSteps:           maxSteps,
		WorkflowMaxLogsPerRun:      maxLogsPerRun,
		WorkflowDefaultStepTimeout: defaultStepTimeout,
		WorkflowMaxStepTimeout:     maxStepTimeout,
		WorkflowMaxAttempts:        maxAttempts,
		WorkflowMaxBackoff:         maxBackoff,
		PolicyMode:                 getEnv("GOX_POLICY_MODE", defaultPolicyMode),
		PolicyRulesPath:            getEnv("GOX_POLICY_RULES_PATH", defaultPolicyRulesPath),
		WorkflowRetentionMaxRuns:   retentionMaxRuns,
		WorkflowRetentionTTL:       retentionTTL,
		MCPMaxPayloadBytes:         maxPayloadBytes,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.ServerName == "" {
		return errors.New("server name cannot be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log level %q", c.LogLevel)
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if c.ActionTimeout <= 0 {
		return errors.New("action timeout must be positive")
	}
	if c.BridgeTimeout <= 0 {
		return errors.New("bridge timeout must be positive")
	}
	if c.BridgeEnabled {
		if c.BridgeNetwork == "" {
			return errors.New("bridge network cannot be empty when bridge is enabled")
		}
		switch c.BridgeNetwork {
		case "tcp":
			if c.BridgeAddress == "" {
				return errors.New("bridge address cannot be empty when bridge network is tcp")
			}
		case "relay-http":
			if c.BridgeRelayAddr == "" {
				return errors.New("bridge relay address cannot be empty when bridge network is relay-http")
			}
		default:
			return fmt.Errorf("unsupported bridge network %q", c.BridgeNetwork)
		}
		if c.BridgeAuth && c.BridgeAuthToken == "" {
			return errors.New("bridge auth token cannot be empty when bridge auth is enabled")
		}
	}

	if c.RunlogRetentionMaxAge < 0 {
		return errors.New("runlog retention max age must be >= 0")
	}
	if c.WorkflowMaxConcurrentRuns < 0 {
		return errors.New("workflow max concurrent runs must be >= 0")
	}
	if c.WorkflowMaxSteps < 0 {
		return errors.New("workflow max steps must be >= 0")
	}
	if c.WorkflowMaxLogsPerRun < 0 {
		return errors.New("workflow max logs per run must be >= 0")
	}
	if c.WorkflowDefaultStepTimeout < 0 {
		return errors.New("workflow default step timeout must be >= 0")
	}
	if c.WorkflowMaxStepTimeout < 0 {
		return errors.New("workflow max step timeout must be >= 0")
	}
	if c.WorkflowMaxAttempts < 0 {
		return errors.New("workflow max attempts must be >= 0")
	}
	if c.WorkflowMaxBackoff < 0 {
		return errors.New("workflow max backoff must be >= 0")
	}
	if c.WorkflowDefaultStepTimeout > 0 && c.WorkflowMaxStepTimeout > 0 && c.WorkflowDefaultStepTimeout > c.WorkflowMaxStepTimeout {
		return errors.New("workflow default step timeout cannot exceed workflow max step timeout")
	}
	mode := c.PolicyMode
	if mode == "" {
		mode = defaultPolicyMode
	}
	switch mode {
	case "allow-all", "deny-by-default":
	default:
		return fmt.Errorf("unsupported policy mode %q", mode)
	}
	if c.WorkflowRetentionMaxRuns < 0 {
		return errors.New("workflow retention max runs must be >= 0")
	}
	if c.WorkflowRetentionTTL < 0 {
		return errors.New("workflow retention ttl must be >= 0")
	}
	if c.MCPMaxPayloadBytes < 0 {
		return errors.New("mcp max payload bytes must be >= 0")
	}
	return nil
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func parseEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of seconds: %w", key, err)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be > 0 seconds", key)
	}
	return time.Duration(seconds) * time.Second, nil
}

func parseEnvDurationAllowZero(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of seconds: %w", key, err)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("%s must be >= 0 seconds", key)
	}
	return time.Duration(seconds) * time.Second, nil
}

func parseEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	out, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return out, nil
}

func parseEnvBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}
