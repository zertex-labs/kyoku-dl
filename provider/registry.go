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

// ListProviders returns the names of all registered providers.
func (r *Registry) ListProviders() []string {
	names := make([]string, len(r.providers))
	for i, p := range r.providers {
		names[i] = p.Name()
	}
	return names
}
