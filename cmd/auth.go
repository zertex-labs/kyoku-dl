package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/zertex-labs/kyoku-dl/provider/deezer"
	"github.com/zertex-labs/kyoku-dl/provider/tidal"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for streaming services",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to a streaming service",
	Long: `Log in to a streaming service. Specify which service to authenticate with:

  kyoku-dl auth login tidal    Log in to Tidal via device authorization
  kyoku-dl auth login deezer   Log in to Deezer with your ARL cookie`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var authLoginTidalCmd = &cobra.Command{
	Use:   "tidal",
	Short: "Log in to Tidal via device authorization",
	RunE:  runAuthLoginTidal,
}

var authLoginDeezerCmd = &cobra.Command{
	Use:   "deezer",
	Short: "Log in to Deezer with your ARL cookie",
	RunE:  runAuthLoginDeezer,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status for all services",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove saved authentication credentials",
	Long: `Remove saved authentication credentials. Specify which service to log out of:

  kyoku-dl auth logout tidal    Remove Tidal credentials
  kyoku-dl auth logout deezer   Remove Deezer credentials`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var authLogoutTidalCmd = &cobra.Command{
	Use:   "tidal",
	Short: "Remove Tidal authentication credentials",
	RunE:  runAuthLogoutTidal,
}

var authLogoutDeezerCmd = &cobra.Command{
	Use:   "deezer",
	Short: "Remove Deezer authentication credentials",
	RunE:  runAuthLogoutDeezer,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)

	authLoginCmd.AddCommand(authLoginTidalCmd)
	authLoginCmd.AddCommand(authLoginDeezerCmd)

	authLogoutCmd.AddCommand(authLogoutTidalCmd)
	authLogoutCmd.AddCommand(authLogoutDeezerCmd)
}

func runAuthLoginTidal(cmd *cobra.Command, args []string) error {
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	// Create default config if it doesn't exist so users know it's there
	ensureDefaultConfig()

	auth, err := tidal.DeviceLogin()
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err := tidal.SaveAuth(authFile, *auth); err != nil {
		return fmt.Errorf("failed to save auth: %w", err)
	}

	fmt.Printf("Auth saved to %s\n", authFile)
	return nil
}

func runAuthLoginDeezer(cmd *cobra.Command, args []string) error {
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}
	ensureDefaultConfig()

	// Check if ARL was provided as an argument
	if len(args) > 0 {
		// If ARL provided directly, init session
		auth, err := deezer.InitSession(args[0])
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		if err := deezer.SaveAuth(deezerAuthFile, *auth); err != nil {
			return fmt.Errorf("failed to save auth: %w", err)
		}

		fmt.Printf("Auth saved to %s\n", deezerAuthFile)
		return nil
	}

	// Prompt for ARL (the EnsureAuth function handles the interactive prompt)
	auth, err := deezer.EnsureAuth(deezerAuthFile, deezer.Auth{})
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	_ = auth

	fmt.Printf("Auth saved to %s\n", deezerAuthFile)
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	// Tidal status
	tidalAuth, err := tidal.LoadAuth(authFile)
	if err != nil {
		fmt.Println("Tidal: Not logged in")
		fmt.Printf("  Auth file: %s\n", authFile)
		fmt.Println("  Run 'kyoku-dl auth login tidal' to log in.")
	} else {
		fmt.Println("Tidal Authentication Status")
		fmt.Println()
		fmt.Printf("  User ID:      %s\n", tidalAuth.UserID)
		fmt.Printf("  Country:      %s\n", tidalAuth.CountryCode)
		fmt.Printf("  Auth file:    %s\n", authFile)

		if tidalAuth.ExpiresAt > 0 {
			expiryTime := time.Unix(tidalAuth.ExpiresAt, 0)
			if time.Now().After(expiryTime) {
				fmt.Printf("  Token:        expired (at %s)\n", expiryTime.Format(time.RFC3339))
				if tidalAuth.RefreshToken != "" {
					fmt.Println("  Refresh:      available (will auto-refresh on next use)")
				} else {
					fmt.Println("  Refresh:      unavailable (re-login required)")
				}
			} else {
				remaining := time.Until(expiryTime).Round(time.Minute)
				fmt.Printf("  Token:        valid (expires in %s)\n", remaining)
			}
		} else {
			fmt.Println("  Token:        present (no expiry info)")
		}
	}

	fmt.Println()

	// Deezer status
	deezerAuth, err := deezer.LoadAuth(deezerAuthFile)
	if err != nil {
		fmt.Println("Deezer: Not logged in")
		fmt.Printf("  Auth file: %s\n", deezerAuthFile)
		fmt.Println("  Run 'kyoku-dl auth login deezer' to log in.")
	} else {
		fmt.Println("Deezer Authentication Status")
		fmt.Println()
		fmt.Printf("  User ID:      %s\n", deezerAuth.UserID)
		fmt.Printf("  Lossless:     %v\n", deezerAuth.CanStreamLossless)
		fmt.Printf("  HQ (320):     %v\n", deezerAuth.CanStreamHQ)
		fmt.Printf("  Auth file:    %s\n", deezerAuthFile)
		if deezerAuth.ARL != "" && len(deezerAuth.ARL) > 12 {
			fmt.Printf("  ARL:          %s...%s\n", deezerAuth.ARL[:8], deezerAuth.ARL[len(deezerAuth.ARL)-4:])
		}
	}

	return nil
}

func runAuthLogoutTidal(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(authFile); os.IsNotExist(err) {
		fmt.Println("Tidal: No auth file found. Already logged out.")
		return nil
	}

	if err := os.Remove(authFile); err != nil {
		return fmt.Errorf("failed to remove auth file: %w", err)
	}

	fmt.Println("Tidal: Logged out. Auth file removed.")
	return nil
}

func runAuthLogoutDeezer(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(deezerAuthFile); os.IsNotExist(err) {
		fmt.Println("Deezer: No auth file found. Already logged out.")
		return nil
	}

	if err := os.Remove(deezerAuthFile); err != nil {
		return fmt.Errorf("failed to remove auth file: %w", err)
	}

	fmt.Println("Deezer: Logged out. Auth file removed.")
	return nil
}
