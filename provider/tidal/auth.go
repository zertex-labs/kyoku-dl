package tidal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	AuthBaseURL     = "https://auth.tidal.com/v1/oauth2/"
	DeviceAuthScope = "r_usr w_usr"
)

// clientCredentials holds a client ID and secret pair for the device auth flow.
type clientCredentials struct {
	ID     string
	Secret string
}

// knownClients is a list of client credentials to try in order.
// These are extracted from Tidal apps and used by various open-source tools.
// If one is revoked, the next is attempted automatically.
var knownClients = []clientCredentials{
	{ID: "zU4XHVVkc2tDPo4t", Secret: "VJKhDFqJPqvsPVNBV6ukXTJmwlvbttP7wlMlrc72se4="}, // streamrip
	{ID: "4N3n6Q1x95LL5K7p", Secret: "oKOXfJW371cX6xaZ0PyhgGNBdNLlBZd4AKKYougMjik="}, // orpheusdl (TV)
	{ID: "cgiF7TQuB97BUIu3", Secret: "1nqpgx8uvBdZigrx4hUPDV2hOwgYAAAG5DYXOr6uNf8="}, // orpheusdl (alt)
}

// DeviceAuthResponse is returned when requesting a device code.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// TokenResponse is returned when polling for or refreshing a token.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         struct {
		UserID      int    `json:"userId"`
		CountryCode string `json:"countryCode"`
	} `json:"user"`
}

// TokenErrorResponse is returned on token polling errors.
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	SubStatus        int    `json:"sub_status"`
}

// SaveAuth writes authentication data to a JSON file.
func SaveAuth(path string, auth Auth) error {
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}

	return nil
}

// IsTokenExpired checks whether the access token has expired (or will expire within 5 minutes).
func (a *Auth) IsTokenExpired() bool {
	if a.ExpiresAt == 0 {
		return false // no expiry info, assume valid
	}
	return time.Now().Unix() >= a.ExpiresAt-300 // 5 minute buffer
}

// RequestDeviceCode initiates the device authorization flow by requesting a device code.
// It tries each known client credential until one works.
func RequestDeviceCode() (*DeviceAuthResponse, *clientCredentials, error) {
	var lastErr error

	for i := range knownClients {
		creds := &knownClients[i]
		deviceAuth, err := tryRequestDeviceCode(creds)
		if err == nil {
			return deviceAuth, creds, nil
		}
		lastErr = err
	}

	return nil, nil, fmt.Errorf("all client credentials failed - %w", lastErr)
}

// tryRequestDeviceCode attempts device authorization with a specific set of credentials.
func tryRequestDeviceCode(creds *clientCredentials) (*DeviceAuthResponse, error) {
	data := url.Values{
		"client_id": {creds.ID},
		"scope":     {DeviceAuthScope},
	}

	resp, err := http.PostForm(AuthBaseURL+"device_authorization", data)
	if err != nil {
		return nil, fmt.Errorf("failed to request device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed (status %d): %s", resp.StatusCode, string(body))
	}

	var deviceAuth DeviceAuthResponse
	if err := json.Unmarshal(body, &deviceAuth); err != nil {
		return nil, fmt.Errorf("failed to parse device auth response: %w", err)
	}

	return &deviceAuth, nil
}

// PollForToken polls the token endpoint until the user authorizes the device or the code expires.
// It prints a styled prompt with the verification URL and user code.
func PollForToken(deviceAuth *DeviceAuthResponse, creds *clientCredentials) (*Auth, error) {
	interval := time.Duration(deviceAuth.Interval) * time.Second
	if interval < 1*time.Second {
		interval = 5 * time.Second
	}

	deadline := time.Now().Add(time.Duration(deviceAuth.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		auth, done, err := checkDeviceToken(deviceAuth.DeviceCode, creds)
		if err != nil {
			return nil, err
		}
		if done {
			return auth, nil
		}
	}

	return nil, fmt.Errorf("device authorization timed out - the code expired")
}

// checkDeviceToken makes a single poll request to the token endpoint.
// Returns (auth, true, nil) on success, (nil, false, nil) if still pending, or (nil, false, err) on failure.
func checkDeviceToken(deviceCode string, creds *clientCredentials) (*Auth, bool, error) {
	data := url.Values{
		"client_id":     {creds.ID},
		"client_secret": {creds.Secret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		"scope":         {DeviceAuthScope},
	}

	resp, err := http.PostForm(AuthBaseURL+"token", data)
	if err != nil {
		return nil, false, fmt.Errorf("failed to poll token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read token response: %w", err)
	}

	// Still waiting for user authorization
	if resp.StatusCode == http.StatusBadRequest {
		var tokenErr TokenErrorResponse
		if err := json.Unmarshal(body, &tokenErr); err == nil {
			if tokenErr.SubStatus == 1002 || tokenErr.Error == "authorization_pending" {
				return nil, false, nil // still pending, keep polling
			}
			if tokenErr.Error == "expired_token" {
				return nil, false, fmt.Errorf("device code expired")
			}
		}
		return nil, false, fmt.Errorf("token request failed: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, false, fmt.Errorf("failed to parse token response: %w", err)
	}

	auth := &Auth{
		Token:        tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Unix() + int64(tokenResp.ExpiresIn),
		UserID:       fmt.Sprintf("%d", tokenResp.User.UserID),
		CountryCode:  tokenResp.User.CountryCode,
	}

	return auth, true, nil
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
// It tries each known client credential until one works.
func RefreshAccessToken(auth *Auth) error {
	if auth.RefreshToken == "" {
		return fmt.Errorf("no refresh token available - please log in again")
	}

	var lastErr error
	for _, creds := range knownClients {
		err := tryRefreshToken(auth, &creds)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("token refresh failed with all credentials: %w", lastErr)
}

// tryRefreshToken attempts to refresh the token with a specific set of credentials.
func tryRefreshToken(auth *Auth, creds *clientCredentials) error {
	data := url.Values{
		"client_id":     {creds.ID},
		"client_secret": {creds.Secret},
		"refresh_token": {auth.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm(AuthBaseURL+"token", data)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse refresh response: %w", err)
	}

	auth.Token = tokenResp.AccessToken
	auth.ExpiresAt = time.Now().Unix() + int64(tokenResp.ExpiresIn)

	// Update refresh token if a new one was provided
	if tokenResp.RefreshToken != "" {
		auth.RefreshToken = tokenResp.RefreshToken
	}

	return nil
}

// DeviceLogin runs the full device authorization flow interactively:
// requests a device code, prints instructions, and polls until authorized.
func DeviceLogin() (*Auth, error) {
	deviceAuth, creds, err := RequestDeviceCode()
	if err != nil {
		return nil, err
	}

	// Display login instructions using the project's style
	linkStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06B6D4"))
	codeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	verifyURL := deviceAuth.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = deviceAuth.VerificationURI
	}
	if !strings.HasPrefix(verifyURL, "https://") && !strings.HasPrefix(verifyURL, "http://") {
		verifyURL = "https://" + verifyURL
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED")).Render("Tidal Login"))
	fmt.Println()
	fmt.Printf("  Open this URL in your browser:\n")
	fmt.Printf("  %s\n\n", linkStyle.Render(verifyURL))
	fmt.Printf("  Or go to %s and enter code: %s\n\n",
		linkStyle.Render("https://link.tidal.com"),
		codeStyle.Render(deviceAuth.UserCode))
	fmt.Println(mutedStyle.Render(fmt.Sprintf("  Waiting for authorization (expires in %ds)...", deviceAuth.ExpiresIn)))
	fmt.Println()

	auth, err := PollForToken(deviceAuth, creds)
	if err != nil {
		return nil, err
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	fmt.Println(successStyle.Render("  ✓ Successfully logged in to Tidal!"))
	fmt.Println()

	return auth, nil
}

// EnsureAuth checks the current auth state and either refreshes the token or
// initiates a new device login. It saves the updated auth to disk.
// Returns the valid auth and any error.
func EnsureAuth(authPath string, current Auth) (Auth, error) {
	// If we have a token that isn't the placeholder, check if it needs refresh
	if current.Token != "" && current.Token != "YOUR_TIDAL_AUTH_TOKEN_HERE" {
		if !current.IsTokenExpired() {
			return current, nil // token is still valid
		}

		// Try to refresh
		if current.RefreshToken != "" {
			fmt.Println(strings.Repeat(" ", 0) + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("Token expired, refreshing..."))
			if err := RefreshAccessToken(&current); err == nil {
				if saveErr := SaveAuth(authPath, current); saveErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not save refreshed token: %v\n", saveErr)
				}
				return current, nil
			}
			// Refresh failed, fall through to device login
			fmt.Fprintf(os.Stderr, "Warning: token refresh failed, starting new login...\n")
		}
	}

	// No valid token - run device login
	auth, err := DeviceLogin()
	if err != nil {
		return Auth{}, fmt.Errorf("login failed: %w", err)
	}

	// Save the new auth to disk
	if err := SaveAuth(authPath, *auth); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save auth file: %v\n", err)
	}

	return *auth, nil
}
