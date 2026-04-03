package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// AgentConfig holds configuration for a specific agent
type AgentConfig struct {
	Model     string `mapstructure:"model"`
	Reasoning string `mapstructure:"reasoning"`
	Provider  string `mapstructure:"provider,omitempty"`
}

// OutputConfig holds output configuration
type OutputConfig struct {
	Directory string `mapstructure:"directory"`
	Overwrite bool   `mapstructure:"overwrite"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level          string `mapstructure:"level"`
	ChainOfThought bool   `mapstructure:"chain_of_thought"`
}

// Config is the top-level configuration struct
type Config struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
	Agents   struct {
		Orchestrator AgentConfig `mapstructure:"orchestrator"`
		Architect    AgentConfig `mapstructure:"architect"`
		Planner      AgentConfig `mapstructure:"planner"`
		Builder      AgentConfig `mapstructure:"builder"`
		Reviewer     AgentConfig `mapstructure:"reviewer"`
		Tester       AgentConfig `mapstructure:"tester"`
		Iterator     AgentConfig `mapstructure:"iterator"`
	} `mapstructure:"agents"`
	Output  OutputConfig  `mapstructure:"output"`
	Logging LoggingConfig `mapstructure:"logging"`
}

// Load loads configuration from file and environment variables
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Config file
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		// Search in home dir and current dir
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".vibe-agents"))
		}
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
		v.SetConfigName("default")
		v.SetConfigType("yaml")
	}

	// Env var bindings
	v.SetEnvPrefix("VIBE")
	v.AutomaticEnv()

	// Specific env var bindings
	_ = v.BindEnv("provider", "VIBE_PROVIDER")
	_ = v.BindEnv("model", "VIBE_MODEL")

	// Read config file (non-fatal if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("provider", "copilot")
	v.SetDefault("model", "gpt-4o")
	v.SetDefault("agents.architect.model", "gpt-4o")
	v.SetDefault("agents.architect.reasoning", "cot")
	v.SetDefault("agents.planner.model", "gpt-4o")
	v.SetDefault("agents.planner.reasoning", "cot")
	v.SetDefault("agents.builder.model", "gpt-4o")
	v.SetDefault("agents.builder.reasoning", "cot")
	v.SetDefault("agents.reviewer.model", "gpt-4o")
	v.SetDefault("agents.reviewer.reasoning", "tot")
	v.SetDefault("agents.tester.model", "gpt-4o")
	v.SetDefault("agents.tester.reasoning", "cot")
	v.SetDefault("agents.iterator.model", "gpt-4o")
	v.SetDefault("agents.iterator.reasoning", "react")
	v.SetDefault("output.directory", "./output")
	v.SetDefault("output.overwrite", false)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.chain_of_thought", true)
}

// GetGitHubToken returns the GitHub token from environment
func GetGitHubToken() string {
	return os.Getenv("GITHUB_TOKEN")
}

// GetGitHubAppID returns the GitHub App ID from environment (GITHUB_APP_ID).
func GetGitHubAppID() string {
	return os.Getenv("GITHUB_APP_ID")
}

// GetGitHubAppInstallationID returns the GitHub App Installation ID from
// environment (GITHUB_APP_INSTALLATION_ID).
// This is optional: when empty the installation ID is discovered automatically
// via the GitHub API. Set it explicitly only when the App is installed on
// multiple accounts or organisations and you need to choose a specific one.
func GetGitHubAppInstallationID() string {
	return os.Getenv("GITHUB_APP_INSTALLATION_ID")
}

// GetGitHubAppPrivateKey returns the PEM-encoded RSA private key for the
// GitHub App.  It checks two environment variables in order:
//
//  1. GITHUB_APP_PRIVATE_KEY — the PEM content directly.  Literal "\n"
//     sequences are treated as newlines so the value fits in a single-line
//     env var.
//  2. GITHUB_APP_PRIVATE_KEY_PATH — path to a PEM file on disk.
//
// Returns an empty string and nil error when neither variable is set. If
// GITHUB_APP_PRIVATE_KEY_PATH is set but the file cannot be read, the error
// is returned so callers can surface a clear diagnostic.
func GetGitHubAppPrivateKeyWithError() (string, error) {
	if v := os.Getenv("GITHUB_APP_PRIVATE_KEY"); v != "" {
		return v, nil
	}
	if path := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read GITHUB_APP_PRIVATE_KEY_PATH %q: %w", path, err)
		}
		return string(data), nil
	}
	return "", nil
}

// GetGitHubAppPrivateKey returns the GitHub App private key from environment.
// Prefer GetGitHubAppPrivateKeyWithError when callers need deterministic
// diagnostics for unreadable GITHUB_APP_PRIVATE_KEY_PATH values.
func GetGitHubAppPrivateKey() string {
	key, err := GetGitHubAppPrivateKeyWithError()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		return ""
	}
	return key
}
// GetOpenAIKey returns the OpenAI API key from environment
func GetOpenAIKey() string {
	return os.Getenv("OPENAI_API_KEY")
}

// GetAnthropicKey returns the Anthropic API key from environment
func GetAnthropicKey() string {
	return os.Getenv("ANTHROPIC_API_KEY")
}

// Save saves a key-value pair to the user's config file
func Save(key, value string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	configDir := filepath.Join(home, ".vibe-agents")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	v := viper.New()
	configFile := filepath.Join(configDir, "config.yaml")
	v.SetConfigFile(configFile)

	// Try to read existing config
	_ = v.ReadInConfig()

	v.Set(key, value)

	if err := v.WriteConfig(); err != nil {
		// Try WriteConfigAs if file doesn't exist
		if err := v.WriteConfigAs(configFile); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}

	return nil
}
