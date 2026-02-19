package config

import (
	"testing"
	"time"
)

func baseConfig() Config {
	return Config{
		ServerName:      "gox",
		LogLevel:        "info",
		ShutdownTimeout: 2 * time.Second,
		ActionTimeout:   2 * time.Second,
		BridgeTimeout:   2 * time.Second,
		BridgeEnabled:   false,
		BridgeNetwork:   "tcp",
		BridgeAddress:   "127.0.0.1:1",
		BridgeRelayAddr: "127.0.0.1:2",
		BridgeAuthToken: "token",
		BridgeAuth:      true,
	}
}

func TestValidate(t *testing.T) {
	cfg := baseConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateRejectsBadLogLevel(t *testing.T) {
	cfg := baseConfig()
	cfg.LogLevel = "trace"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsEnabledBridgeWithoutAddress(t *testing.T) {
	cfg := baseConfig()
	cfg.BridgeEnabled = true
	cfg.BridgeNetwork = "tcp"
	cfg.BridgeAddress = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsEnabledAuthWithoutToken(t *testing.T) {
	cfg := baseConfig()
	cfg.BridgeEnabled = true
	cfg.BridgeAuth = true
	cfg.BridgeAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsUnsupportedBridgeNetwork(t *testing.T) {
	cfg := baseConfig()
	cfg.BridgeEnabled = true
	cfg.BridgeNetwork = "udp"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsRelayWithoutAddress(t *testing.T) {
	cfg := baseConfig()
	cfg.BridgeEnabled = true
	cfg.BridgeNetwork = "relay-http"
	cfg.BridgeRelayAddr = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsWorkflowDefaultTimeoutGreaterThanMax(t *testing.T) {
	cfg := baseConfig()
	cfg.WorkflowDefaultStepTimeout = 60 * time.Second
	cfg.WorkflowMaxStepTimeout = 30 * time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadFromEnvWorkflowConfig(t *testing.T) {
	t.Setenv("GOX_SERVER_NAME", "gox")
	t.Setenv("GOX_LOG_LEVEL", "info")
	t.Setenv("GOX_SHUTDOWN_TIMEOUT_SECONDS", "10")
	t.Setenv("GOX_ACTION_TIMEOUT_SECONDS", "30")
	t.Setenv("GOX_BRIDGE_TIMEOUT_SECONDS", "5")
	t.Setenv("GOX_BRIDGE_ENABLED", "false")
	t.Setenv("GOX_WORKFLOW_MAX_CONCURRENT_RUNS", "3")
	t.Setenv("GOX_WORKFLOW_MAX_STEPS", "22")
	t.Setenv("GOX_WORKFLOW_MAX_LOGS_PER_RUN", "99")
	t.Setenv("GOX_WORKFLOW_DEFAULT_STEP_TIMEOUT_SECONDS", "12")
	t.Setenv("GOX_WORKFLOW_MAX_STEP_TIMEOUT_SECONDS", "45")
	t.Setenv("GOX_WORKFLOW_MAX_ATTEMPTS", "4")
	t.Setenv("GOX_WORKFLOW_MAX_BACKOFF_SECONDS", "8")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.WorkflowMaxConcurrentRuns != 3 {
		t.Fatalf("unexpected max concurrent runs: %d", cfg.WorkflowMaxConcurrentRuns)
	}
	if cfg.WorkflowMaxSteps != 22 {
		t.Fatalf("unexpected max steps: %d", cfg.WorkflowMaxSteps)
	}
	if cfg.WorkflowMaxLogsPerRun != 99 {
		t.Fatalf("unexpected max logs per run: %d", cfg.WorkflowMaxLogsPerRun)
	}
	if cfg.WorkflowDefaultStepTimeout != 12*time.Second {
		t.Fatalf("unexpected default step timeout: %v", cfg.WorkflowDefaultStepTimeout)
	}
	if cfg.WorkflowMaxStepTimeout != 45*time.Second {
		t.Fatalf("unexpected max step timeout: %v", cfg.WorkflowMaxStepTimeout)
	}
	if cfg.WorkflowMaxAttempts != 4 {
		t.Fatalf("unexpected max attempts: %d", cfg.WorkflowMaxAttempts)
	}
	if cfg.WorkflowMaxBackoff != 8*time.Second {
		t.Fatalf("unexpected max backoff: %v", cfg.WorkflowMaxBackoff)
	}
}

func TestLoadFromEnvRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("GOX_BRIDGE_ENABLED", "not-bool")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected load error")
	}
}

func TestLoadFromEnvRejectsInvalidDuration(t *testing.T) {
	t.Setenv("GOX_ACTION_TIMEOUT_SECONDS", "NaN")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected load error")
	}
}

func TestLoadFromEnvRejectsInvalidInt(t *testing.T) {
	t.Setenv("GOX_WORKFLOW_MAX_STEPS", "oops")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected load error")
	}
}

func TestLoadFromEnvDefaultsToDenyByDefaultPolicyMode(t *testing.T) {
	t.Setenv("GOX_POLICY_MODE", "")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.PolicyMode != "deny-by-default" {
		t.Fatalf("unexpected default policy mode: %q", cfg.PolicyMode)
	}
}
