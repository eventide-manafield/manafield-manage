package coreclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Module struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capability struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type Resource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Provides struct {
		Capabilities []Capability `json:"capabilities"`
	} `json:"provides"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

func (c *Client) ListModules(ctx context.Context) ([]Module, error) {
	var modules []Module
	if err := c.getJSON(ctx, "/modules", &modules); err != nil {
		return nil, err
	}
	return modules, nil
}

func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource
	if err := c.getJSON(ctx, "/resources", &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create Core request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request Core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Core returned %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Core response: %w", err)
	}

	return nil
}
