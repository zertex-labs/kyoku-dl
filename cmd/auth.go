package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/zertex-labs/kyoku-dl/provider/tidal"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for streaming services",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Tidal via device authorization",
	RunE:  runAuthLogin,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove saved authentication credentials",
	RunE:  runAuthLogout,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
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

func runAuthStatus(cmd *cobra.Command, args []string) error {
	auth, err := tidal.LoadAuth(authFile)
	if err != nil {
		fmt.Println("Not logged in. No auth file found.")
		fmt.Printf("  Auth file: %s\n", authFile)
		fmt.Println("\nRun 'kyoku-dl auth login' to log in.")
		return nil
	}

	fmt.Println("Tidal Authentication Status")
	fmt.Println()
	fmt.Printf("  User ID:      %s\n", auth.UserID)
	fmt.Printf("  Country:      %s\n", auth.CountryCode)
	fmt.Printf("  Auth file:    %s\n", authFile)

	if auth.ExpiresAt > 0 {
		expiryTime := time.Unix(auth.ExpiresAt, 0)
		if time.Now().After(expiryTime) {
			fmt.Printf("  Token:        expired (at %s)\n", expiryTime.Format(time.RFC3339))
			if auth.RefreshToken != "" {
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

	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(authFile); os.IsNotExist(err) {
		fmt.Println("No auth file found. Already logged out.")
		return nil
	}

	if err := os.Remove(authFile); err != nil {
		return fmt.Errorf("failed to remove auth file: %w", err)
	}

	fmt.Println("Logged out. Auth file removed.")
	return nil
}
