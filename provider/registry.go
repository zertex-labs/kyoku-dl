package provider

import (
	"fmt"
)

// Registry holds all registered music service providers.
type Registry struct {
	providers []Provider
}

// NewRegistry creates a new empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make([]Provider, 0),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.providers = append(r.providers, p)
}

// GetProviderForURL returns the appropriate provider for a given URL.
// Returns an error if no matching provider is found.
func (r *Registry) GetProviderForURL(url string) (Provider, error) {
	for _, p := range r.providers {
		if p.MatchesURL(url) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no provider found for URL: %s", url)
}

// GetProviderByName returns a provider by its service name.
// Returns an error if no matching provider is found.
func (r *Registry) GetProviderByName(name string) (Provider, error) {
	for _, p := range r.providers {
		if p.Name() == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no provider found with name: %s", name)
}

// ListProviders returns the names of all registered providers.
func (r *Registry) ListProviders() []string {
	names := make([]string, len(r.providers))
	for i, p := range r.providers {
		names[i] = p.Name()
	}
	return names
}

// Download auto-detects the service from the URL and downloads the content.
func (r *Registry) Download(url string) error {
	p, err := r.GetProviderForURL(url)
	if err != nil {
		return err
	}

	// Check if it's a track or album URL
	// For now, we'll try track first, then album if that fails
	// Each provider's MatchesURL already validated the URL belongs to that service

	if err := p.DownloadTrack(url); err != nil {
		// Try album download
		if albumErr := p.DownloadAlbum(url); albumErr != nil {
			// Return the original track error if album also fails
			return fmt.Errorf("download failed: %w", err)
		}
	}

	return nil
}

// SetDebugAll enables or disables debug mode on all registered providers.
func (r *Registry) SetDebugAll(enabled bool) {
	for _, p := range r.providers {
		p.SetDebug(enabled)
	}
}
