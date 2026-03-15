package main

import (
	"context"
	"fmt"
	"net/http"

	openapigraphql "github.com/AkinoKaede/rdai-bot/graphql"

	"github.com/Khan/genqlient/graphql"
)

type IssuedKey struct {
	Name   string   `json:"name"`
	Key    string   `json:"key"`
	Scopes []string `json:"scopes"`
}

type KeyIssuer interface {
	CreateAPIKey(ctx context.Context, name string) (*IssuedKey, error)
}

type AxonHubKeyIssuer struct {
	client graphql.Client
}

func NewAxonHubKeyIssuer(cfg Config) (*AxonHubKeyIssuer, error) {
	httpClient := &http.Client{
		Transport: &headerTransport{
			apiKey: cfg.AxonHubAPIKey,
			base:   http.DefaultTransport,
		},
	}

	return &AxonHubKeyIssuer{
		client: graphql.NewClient(cfg.AxonHubEndpoint, httpClient),
	}, nil
}

func (a *AxonHubKeyIssuer) CreateAPIKey(ctx context.Context, name string) (*IssuedKey, error) {
	resp, err := openapigraphql.CreateAPIKey(ctx, a.client, name)
	if err != nil {
		return nil, fmt.Errorf("create axonhub api key: %w", err)
	}
	if resp == nil || resp.CreateLLMAPIKey == nil {
		return nil, fmt.Errorf("create axonhub api key: empty response")
	}

	return &IssuedKey{
		Name:   resp.CreateLLMAPIKey.Name,
		Key:    resp.CreateLLMAPIKey.Key,
		Scopes: append([]string(nil), resp.CreateLLMAPIKey.Scopes...),
	}, nil
}

type headerTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	return t.base.RoundTrip(req)
}
