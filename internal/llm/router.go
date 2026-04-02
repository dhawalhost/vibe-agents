package llm

import (
	"context"
	"fmt"
)

// AgentRouteKey identifies an agent for routing
type AgentRouteKey string

// ProviderRoute defines routing config for an agent
type ProviderRoute struct {
	Provider string
	Model    string
	Fallback string // fallback provider name
}

// ProviderRouter routes LLM requests to specific providers based on agent
type ProviderRouter struct {
	providers       map[string]LLMProvider
	routes          map[AgentRouteKey]*ProviderRoute
	defaultProvider string
	defaultModel    string
}

func NewProviderRouter(defaultProvider, defaultModel string) *ProviderRouter {
	return &ProviderRouter{
		providers:       make(map[string]LLMProvider),
		routes:          make(map[AgentRouteKey]*ProviderRoute),
		defaultProvider: defaultProvider,
		defaultModel:    defaultModel,
	}
}

func (r *ProviderRouter) Register(name string, provider LLMProvider) {
	r.providers[name] = provider
}

func (r *ProviderRouter) SetRoute(agent AgentRouteKey, route *ProviderRoute) {
	r.routes[agent] = route
}

func (r *ProviderRouter) GetProvider(agent AgentRouteKey) (LLMProvider, string, error) {
	route, ok := r.routes[agent]
	if !ok {
		// Use default
		provider, ok := r.providers[r.defaultProvider]
		if !ok {
			return nil, "", fmt.Errorf("default provider %q not registered", r.defaultProvider)
		}
		return provider, r.defaultModel, nil
	}

	provider, ok := r.providers[route.Provider]
	if !ok {
		// Try fallback
		if route.Fallback != "" {
			provider, ok = r.providers[route.Fallback]
			if ok {
				return provider, route.Model, nil
			}
		}
		return nil, "", fmt.Errorf("provider %q not registered", route.Provider)
	}
	return provider, route.Model, nil
}

func (r *ProviderRouter) Generate(ctx context.Context, agent AgentRouteKey, req LLMRequest) (*LLMResponse, error) {
	provider, model, err := r.GetProvider(agent)
	if err != nil {
		return nil, fmt.Errorf("route agent %q: %w", agent, err)
	}
	if req.Model == "" {
		req.Model = model
	}
	return provider.Generate(ctx, req)
}

func (r *ProviderRouter) GenerateStream(ctx context.Context, agent AgentRouteKey, req LLMRequest) (<-chan StreamChunk, error) {
	provider, model, err := r.GetProvider(agent)
	if err != nil {
		return nil, fmt.Errorf("route agent %q: %w", agent, err)
	}
	if req.Model == "" {
		req.Model = model
	}
	return provider.GenerateStream(ctx, req)
}
