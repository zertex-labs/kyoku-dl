package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// shareHosts maps share-link hostnames to their parent service domain.
// When a URL's host matches a key, it's a share/short link that needs
// redirect resolution before provider matching.
var shareHosts = map[string]bool{
	"link.deezer.com":  true,
	"link.tidal.com":   true,
	"deezer.page.link": true,
}

// ResolveURL follows redirects for known share/short-link domains and returns
// the final canonical URL. Non-share URLs are returned unchanged.
//
// Deezer share links redirect like:
//
//	link.deezer.com/s/XXX → link.deezer.com/?dest=... → www.deezer.com/album/12345 → www.deezer.com/en/album/12345
//
// We follow up to 10 redirects and return the final Location.
func ResolveURL(rawURL string) (string, error) {
	// Quick check: only resolve known share-link hosts
	isShare := false
	for host := range shareHosts {
		if strings.Contains(rawURL, host) {
			isShare = true
			break
		}
	}
	if !isShare {
		return rawURL, nil
	}

	// Use a client that does NOT follow redirects automatically — we read
	// the Location header ourselves so we can capture the final URL.
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Stop following — we'll read Location manually
			return http.ErrUseLastResponse
		},
	}

	current := rawURL
	for i := 0; i < 10; i++ {
		req, err := http.NewRequest("GET", current, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create resolve request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to resolve share link: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc == "" {
				return current, nil
			}
			current = loc
			continue
		}

		// Non-redirect — we've arrived
		return current, nil
	}

	return current, nil
}
