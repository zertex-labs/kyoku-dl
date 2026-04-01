package deezer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

// LoadAuth reads authentication data from a JSON file.
func LoadAuth(path string) (Auth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Auth{}, fmt.Errorf("failed to read auth file: %w", err)
	}

	var auth Auth
	if err := json.Unmarshal(data, &auth); err != nil {
		return Auth{}, fmt.Errorf("failed to parse auth file: %w", err)
	}

	return auth, nil
}

// InitSession initializes a Deezer session using an ARL cookie.
// It contacts the private gw-light API to retrieve user data, license token,
// and the checkForm (API token) needed for subsequent private API calls.
// This is a standalone function used by the `auth login` CLI command and
// EnsureAuth when no Deezer struct is available yet.
func InitSession(arl string) (*Auth, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Set the ARL cookie on deezer.com
	deezerURL, _ := url.Parse("https://www.deezer.com")
	jar.SetCookies(deezerURL, []*http.Cookie{
		{
			Name:   "arl",
			Value:  arl,
			Domain: ".deezer.com",
			Path:   "/",
		},
	})

	client := &http.Client{Jar: jar}

	// Call deezer.getUserData with empty api_token (first call doesn't need one)
	apiURL := "https://www.deezer.com/ajax/gw-light.php?method=deezer.getUserData&input=3&api_version=1.0&api_token="

	req, err := http.NewRequest("POST", apiURL, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("failed to create getUserData request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call getUserData: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read getUserData response: %w", err)
	}

	// Parse the GW response wrapper
	var gwResp GWResponse
	if err := json.Unmarshal(body, &gwResp); err != nil {
		return nil, fmt.Errorf("failed to parse getUserData response: %w", err)
	}

	// Parse the results into GWUserData
	var userData GWUserData
	if err := json.Unmarshal(gwResp.Results, &userData); err != nil {
		return nil, fmt.Errorf("failed to parse user data: %w", err)
	}

	// Check if logged in
	if userData.USER.USER_ID == 0 {
		return nil, fmt.Errorf("invalid ARL — not logged in")
	}

	// Extract session ID from response cookies
	sessionID := userData.SessionID
	for _, cookie := range jar.Cookies(deezerURL) {
		if cookie.Name == "sid" {
			sessionID = cookie.Value
			break
		}
	}

	auth := &Auth{
		ARL:               arl,
		SessionID:         sessionID,
		LicenseToken:      userData.USER.OPTIONS.LicenseToken,
		APIToken:          userData.CheckForm,
		UserID:            fmt.Sprintf("%d", userData.USER.USER_ID),
		CanStreamLossless: userData.USER.OPTIONS.WebLossless,
		CanStreamHQ:       userData.USER.OPTIONS.WebHQ,
	}

	return auth, nil
}

// InitSessionFromARL initializes the Deezer struct's persistent HTTP client
// and session by calling getUserData with the given ARL cookie. The cookie jar
// on d.httpClient accumulates ALL cookies returned by the server (ARL + sid +
// any others), so subsequent API calls automatically carry them.
func (d *Deezer) InitSessionFromARL(arl string) error {
	// Set up the persistent client with ARL cookie
	d.initSessionClient(arl, "")

	// Call getUserData using the persistent client (which now has ARL in its jar)
	apiURL := "https://www.deezer.com/ajax/gw-light.php?method=deezer.getUserData&input=3&api_version=1.0&api_token="

	req, err := http.NewRequest("POST", apiURL, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("failed to create getUserData request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call getUserData: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read getUserData response: %w", err)
	}

	var gwResp GWResponse
	if err := json.Unmarshal(body, &gwResp); err != nil {
		return fmt.Errorf("failed to parse getUserData response: %w", err)
	}

	var userData GWUserData
	if err := json.Unmarshal(gwResp.Results, &userData); err != nil {
		return fmt.Errorf("failed to parse user data: %w", err)
	}

	if userData.USER.USER_ID == 0 {
		return fmt.Errorf("invalid ARL — not logged in")
	}

	// Extract session ID from the cookie jar (set by the server response)
	deezerURL, _ := url.Parse("https://www.deezer.com")
	sessionID := userData.SessionID
	for _, cookie := range d.httpClient.Jar.Cookies(deezerURL) {
		if cookie.Name == "sid" {
			sessionID = cookie.Value
			break
		}
	}

	// Update auth with the extracted values
	d.auth = Auth{
		ARL:               arl,
		SessionID:         sessionID,
		LicenseToken:      userData.USER.OPTIONS.LicenseToken,
		APIToken:          userData.CheckForm,
		UserID:            fmt.Sprintf("%d", userData.USER.USER_ID),
		CanStreamLossless: userData.USER.OPTIONS.WebLossless,
		CanStreamHQ:       userData.USER.OPTIONS.WebHQ,
	}

	// The cookie jar on d.httpClient now has all cookies from the getUserData
	// response (ARL + sid + any others the server set). Update sid in jar if
	// the server returned a new one that wasn't automatically set.
	if sessionID != "" {
		d.initSessionClient(arl, sessionID)
	}

	return nil
}

// EnsureAuth checks the current auth state. If a valid ARL exists, it re-initializes
// the session (since Deezer sessions expire). If no ARL is present, it prompts
// the user to provide one interactively. The auth is saved to disk.
func EnsureAuth(authPath string, current Auth) (Auth, error) {
	if current.ARL != "" {
		// Re-init session (sessions expire, need fresh license token + checkForm)
		auth, err := InitSession(current.ARL)
		if err != nil {
			// ARL may be expired — prompt for new one
			fmt.Fprintf(os.Stderr, "Warning: session init failed (%v), requesting new ARL...\n", err)
		} else {
			if saveErr := SaveAuth(authPath, *auth); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not save auth file: %v\n", saveErr)
			}
			return *auth, nil
		}
	}

	// No valid ARL — prompt user
	printLoginInstructions()

	reader := bufio.NewReader(os.Stdin)
	arlInput, err := reader.ReadString('\n')
	if err != nil {
		return Auth{}, fmt.Errorf("failed to read ARL input: %w", err)
	}

	arl := strings.TrimSpace(arlInput)
	if arl == "" {
		return Auth{}, fmt.Errorf("no ARL provided")
	}

	auth, err := InitSession(arl)
	if err != nil {
		return Auth{}, fmt.Errorf("login failed: %w", err)
	}

	if saveErr := SaveAuth(authPath, *auth); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save auth file: %v\n", saveErr)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	fmt.Println(successStyle.Render("  \u2713 Successfully logged in to Deezer!"))
	fmt.Println()

	return *auth, nil
}

// printLoginInstructions displays styled instructions for obtaining the ARL cookie.
func printLoginInstructions() {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	linkStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06B6D4"))

	fmt.Println()
	fmt.Println(titleStyle.Render("Deezer Login"))
	fmt.Println()
	fmt.Println("  To log in, you need your ARL cookie from deezer.com:")
	fmt.Println()
	fmt.Printf("  1. Open %s and log in\n", linkStyle.Render("https://www.deezer.com"))
	fmt.Printf("  2. Open DevTools (F12) %s Application %s Cookies %s deezer.com\n",
		mutedStyle.Render("\u2192"), mutedStyle.Render("\u2192"), mutedStyle.Render("\u2192"))
	fmt.Println("  3. Copy the value of the \"arl\" cookie (192 characters)")
	fmt.Println()
	fmt.Print("  Paste your ARL: ")
}
