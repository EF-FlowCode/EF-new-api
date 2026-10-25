package oauth

import (
	"fmt"
	"maps"
	"sync"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
)

var (
	providers = make(map[string]Provider)
	mu        sync.RWMutex
	// customProviderSlugs tracks which providers are custom (can be unregistered)
	customProviderSlugs = make(map[string]bool)
)

type requestScopedProvider interface {
	forRequest() Provider
}

// Register registers an OAuth provider with the given name
func Register(name string, provider Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[name] = provider
}

// RegisterCustom registers a custom OAuth provider (can be unregistered later)
func RegisterCustom(name string, provider Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[name] = provider
	customProviderSlugs[name] = true
}

// Unregister removes a provider from the registry
func Unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(providers, name)
	delete(customProviderSlugs, name)
}

// GetProvider returns the OAuth provider for the given name
func GetProvider(name string) Provider {
	mu.RLock()
	provider := providers[name]
	mu.RUnlock()
	if scoped, ok := provider.(requestScopedProvider); ok {
		return scoped.forRequest()
	}
	return provider
}

// GetAllProviders returns all registered OAuth providers
func GetAllProviders() map[string]Provider {
	mu.RLock()
	defer mu.RUnlock()
	result := make(map[string]Provider, len(providers))
	maps.Copy(result, providers)
	return result
}

// GetEnabledCustomProviders returns all enabled custom OAuth providers
func GetEnabledCustomProviders() []*GenericOAuthProvider {
	mu.RLock()
	defer mu.RUnlock()
	var result []*GenericOAuthProvider
	for name, provider := range providers {
		if customProviderSlugs[name] {
			if gp, ok := provider.(*GenericOAuthProvider); ok && gp.IsEnabled() {
				result = append(result, gp)
			}
		}
	}
	return result
}

// IsProviderRegistered checks if a provider is registered
func IsProviderRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := providers[name]
	return ok
}

// IsCustomProvider checks if a provider is a custom provider
func IsCustomProvider(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return customProviderSlugs[name]
}

// LoadCustomProviders loads all custom OAuth providers from the database
func LoadCustomProviders() error {
	// First, unregister all existing custom providers
	mu.Lock()
	for name := range customProviderSlugs {
		delete(providers, name)
	}
	customProviderSlugs = make(map[string]bool)
	mu.Unlock()

	// Load all custom providers from database
	customProviders, err := model.GetAllCustomOAuthProviders()
	if err != nil {
		common.SysError("Failed to load custom OAuth providers: " + err.Error())
		return err
	}

	// Register each custom provider
	for _, config := range customProviders {
		provider := NewGenericOAuthProvider(config)
		RegisterCustom(config.Slug, provider)
		common.SysLog("Loaded custom OAuth provider: " + config.Name + " (" + config.Slug + ")")
	}

	common.SysLog(fmt.Sprintf("Loaded %d custom OAuth providers", len(customProviders)))
	return nil
}

// ReloadCustomProviders reloads all custom OAuth providers from the database
func ReloadCustomProviders() error {
	return LoadCustomProviders()
}

// RegisterOrUpdateCustomProvider registers or updates a single custom provider
func RegisterOrUpdateCustomProvider(config *model.CustomOAuthProvider) {
	provider := NewGenericOAuthProvider(config)
	mu.Lock()
	defer mu.Unlock()
	providers[config.Slug] = provider
	customProviderSlugs[config.Slug] = true
}

// UnregisterCustomProvider unregisters a custom provider by slug
func UnregisterCustomProvider(slug string) {
	Unregister(slug)
}
