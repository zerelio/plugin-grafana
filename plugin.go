package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	plugintypes "github.com/zerelio/plugin-sdk/plugin"
	sdk "github.com/zerelio/plugin-sdk"
)

type GrafanaPlugin struct {
	sdk.BasePlugin
}

type GrafanaConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

func (p *GrafanaPlugin) Info(ctx context.Context) (*plugintypes.PluginInfo, error) {
	return &plugintypes.PluginInfo{
		Name:        "grafana",
		Version:     "1.0.0",
		Description: "Discovers Grafana dashboards and datasources via the Grafana HTTP API",
		Capabilities: []string{"observability", "monitoring"},
		CredentialTypes: []string{"api_key"},
		ConfigSchema: `{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type": "object",
			"required": ["base_url"],
			"properties": {
				"base_url": {"type": "string", "description": "Grafana base URL"},
				"api_key": {"type": "string", "description": "Grafana API key (optional)"}
			}
		}`,
	}, nil
}

func (p *GrafanaPlugin) ValidateConfig(ctx context.Context, config map[string]string) (*plugintypes.ValidationResult, error) {
	var cfg GrafanaConfig
	if err := sdk.ParseConfig(config, &cfg); err != nil {
		return &plugintypes.ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("failed to parse config: %v", err)},
		}, nil
	}
	var errors []string
	if cfg.BaseURL == "" {
		errors = append(errors, "base_url is required")
	}
	if len(errors) > 0 {
		return &plugintypes.ValidationResult{Valid: false, Errors: errors}, nil
	}
	return &plugintypes.ValidationResult{Valid: true}, nil
}

func (p *GrafanaPlugin) Discover(ctx context.Context, req *plugintypes.DiscoverRequest) (<-chan plugintypes.DiscoveryEvent, error) {
	ch := make(chan plugintypes.DiscoveryEvent, 100)
	go func() {
		defer close(ch)

		var cfg GrafanaConfig
		if err := sdk.ParseConfig(req.Config, &cfg); err != nil {
			ch <- sdk.SendError("INVALID_CONFIG", fmt.Sprintf("failed to parse config: %v", err), false)
			return
		}
		if cfg.BaseURL == "" {
			ch <- sdk.SendError("INVALID_CONFIG", "base_url is required", false)
			return
		}

		client := &http.Client{Timeout: 30 * time.Second}
		ch <- sdk.SendProgress("Connecting to Grafana", 5, 0, 0)

		// Fetch datasources first so we can link dashboards to them
		var datasources []grafanaDatasource
		ch <- sdk.SendProgress("Fetching datasources", 15, 0, 0)
		dsList, dsErr := fetchGrafanaDatasources(client, cfg.BaseURL, cfg.APIKey)
		if dsErr != nil {
			ch <- sdk.SendError("API_ERROR", fmt.Sprintf("failed to fetch datasources: %v", dsErr), true)
		} else {
			datasources = dsList
			for _, ds := range datasources {
				res := sdk.NewResource("grafana", "grafana.datasource", "", "", ds.UID)
				res.Properties = map[string]string{
					"type": ds.Type,
					"url":  ds.URL,
				}
				res.RawData = sdk.MarshalRawData(ds)
				sdk.EmitResource(ch, res)
			}
		}

		// Build datasource name set for relationship resolution
		dsNames := make(map[string]bool, len(datasources))
		for _, ds := range datasources {
			dsNames[ds.UID] = true
		}

		// Discover dashboards
		ch <- sdk.SendProgress("Fetching dashboards", 25, int32(len(datasources)), 0)
		dashboards, err := fetchGrafanaDashboards(client, cfg.BaseURL, cfg.APIKey)
		if err != nil {
			ch <- sdk.SendError("API_ERROR", fmt.Sprintf("failed to fetch dashboards: %v", err), true)
		} else {
			for _, d := range dashboards {
				res := sdk.NewResource("grafana", "grafana.dashboard", "", "", d.UID)
				res.Properties = map[string]string{
					"title": d.Title,
					"url":   fmt.Sprintf("%s/d/%s", strings.TrimRight(cfg.BaseURL, "/"), d.UID),
				}
				res.RawData = sdk.MarshalRawData(d)
				sdk.EmitResource(ch, res)

				// Fetch full dashboard to get panel datasource references
				fullDash, fetchErr := fetchGrafanaDashboardByUID(client, cfg.BaseURL, cfg.APIKey, d.UID)
				if fetchErr == nil && fullDash != nil {
					refDSNames := collectDatasourceRefs(fullDash.Dashboard.Panels, nil)
					for _, refName := range refDSNames {
						if dsNames[refName] {
							sdk.EmitRelationship(ch, sdk.NewRelationship(d.UID, refName, "USES_DATASOURCE"))
						}
					}
				}
			}
		}

		ch <- sdk.SendProgress("Grafana discovery complete", 100, int32(len(dashboards)+len(datasources)), 0)
	}()
	return ch, nil
}

// Grafana API response types.

type grafanaDashboard struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

type grafanaDatasource struct {
	Name string `json:"name"`
	Type string `json:"type"`
	UID  string `json:"uid"`
	URL  string `json:"url"`
}

// Full dashboard response when fetching by UID
type grafanaDashboardFull struct {
	Dashboard struct {
		Panels []grafanaPanel `json:"panels"`
	} `json:"dashboard"`
}

type grafanaPanel struct {
	Type       string `json:"type"`
	Datasource *grafanaPanelDS `json:"datasource,omitempty"`
	Targets    []grafanaPanelTarget `json:"targets,omitempty"`
	Panels     []grafanaPanel `json:"panels,omitempty"` // nested panels (rows)
}

type grafanaPanelDS struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type grafanaPanelTarget struct {
	Datasource struct {
		Type string `json:"type"`
		UID  string `json:"uid"`
	} `json:"datasource"`
}

func fetchGrafanaDashboards(client *http.Client, baseURL, apiKey string) ([]grafanaDashboard, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(baseURL, "/")+"/api/search", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var dashboards []grafanaDashboard
	if err := json.NewDecoder(resp.Body).Decode(&dashboards); err != nil {
		return nil, err
	}
	return dashboards, nil
}

func fetchGrafanaDashboardByUID(client *http.Client, baseURL, apiKey, uid string) (*grafanaDashboardFull, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(baseURL, "/")+"/api/dashboards/uid/"+uid, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var full grafanaDashboardFull
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		return nil, err
	}
	return &full, nil
}

func collectDatasourceRefs(panels []grafanaPanel, seen map[string]bool) []string {
	if seen == nil {
		seen = make(map[string]bool)
	}
	var refs []string
	for _, p := range panels {
		// Check panel-level datasource
		if p.Datasource != nil && p.Datasource.UID != "" {
			if !seen[p.Datasource.UID] {
				seen[p.Datasource.UID] = true
				refs = append(refs, p.Datasource.UID)
			}
		}
		// Check targets (some panels store datasource in targets)
		for _, t := range p.Targets {
			if t.Datasource.UID != "" && !seen[t.Datasource.UID] {
				seen[t.Datasource.UID] = true
				refs = append(refs, t.Datasource.UID)
			}
		}
		// Recurse into nested panels (rows)
		if len(p.Panels) > 0 {
			refs = append(refs, collectDatasourceRefs(p.Panels, seen)...)
		}
	}
	return refs
}

func fetchGrafanaDatasources(client *http.Client, baseURL, apiKey string) ([]grafanaDatasource, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(baseURL, "/")+"/api/datasources", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var datasources []grafanaDatasource
	if err := json.NewDecoder(resp.Body).Decode(&datasources); err != nil {
		return nil, err
	}
	return datasources, nil
}
