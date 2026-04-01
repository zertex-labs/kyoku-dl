package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zertex-labs/kyoku-dl/provider"
)

var (
	// Global flags
	cfgFile  string
	authFile string
	debug    bool

	// Loaded at runtime
	appConfig provider.Config
)

// rootCmd is the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "kyoku-dl",
	Short: "A command-line tool to download music from various streaming services",
	Long: `kyoku-dl is a command-line tool to download music from various streaming
services. Currently supports Tidal (tracks, albums, and artists).

If a URL is provided without a subcommand, it is treated as a download:
  kyoku-dl https://tidal.com/browse/track/123456789
  kyoku-dl https://tidal.com/artist/13427410`,

	// When called with args but no subcommand, delegate to download.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		// Delegate to the download command
		downloadCmd.SetArgs(args)
		return downloadCmd.Execute()
	},
	// Silence Cobra's default error/usage printing so we control it.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the main entry point for the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/kyoku-dl/config.json)")
	rootCmd.PersistentFlags().StringVar(&authFile, "auth-file", "", "auth file (default: ~/.config/kyoku-dl/tidal-auth.json)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug output")

	// Register subcommands
	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(versionCmd)
}

// initConfig reads in config file and sets defaults.
func initConfig() {
	if cfgFile == "" {
		cfgFile = filepath.Join(DefaultConfigDir(), "config.json")
	}
	if authFile == "" {
		authFile = filepath.Join(DefaultConfigDir(), "tidal-auth.json")
	}

	appConfig = loadConfig(cfgFile)

	if debug {
		appConfig.Debug = true
	}
}

// DefaultConfigDir returns the default configuration directory.
// Uses $XDG_CONFIG_HOME/kyoku-dl if set, otherwise ~/.config/kyoku-dl.
func DefaultConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kyoku-dl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		return "."
	}
	return filepath.Join(home, ".config", "kyoku-dl")
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	dir := DefaultConfigDir()
	if cfgFile != "" {
		dir = filepath.Dir(cfgFile)
	}
	return os.MkdirAll(dir, 0755)
}

// loadConfig reads the config file and returns the configuration.
// If the file doesn't exist, creates it with default values.
// Returns default config if the file is invalid.
func loadConfig(path string) provider.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create config file with defaults
			if err := createDefaultConfig(path); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not create default config: %v\n", err)
			}
		}
		return provider.DefaultConfig()
	}

	var config provider.Config
	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse config: %v\n", err)
		return provider.DefaultConfig()
	}

	// Ensure format has a value
	if config.Format == "" {
		config.Format = provider.DefaultConfig().Format
	}

	return config
}

// createDefaultConfig creates a config file with default values.
func createDefaultConfig(path string) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	config := provider.DefaultConfig()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Created default config at %s\n", path)
	return nil
}

// ensureDefaultConfig creates the config file with defaults if it doesn't exist.
func ensureDefaultConfig() {
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		if err := createDefaultConfig(cfgFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create default config: %v\n", err)
		}
	}
}
