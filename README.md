# Zerelio Grafana Plugin

Discovers Grafana resources and streams them into the Zerelio knowledge graph.

## What It Discovers

| Resource type           | Description                              |
|-------------------------|------------------------------------------|
| `grafana.dashboard`     | Dashboards (all folders)                 |
| `grafana.datasource`    | Data sources and their configuration     |
| `grafana.alert_rule`    | Unified alerting rules                   |
| `grafana.team`          | Teams and their members                  |

## Configuration

| Environment variable    | Required | Description                                         |
|-------------------------|----------|-----------------------------------------------------|
| `GRAFANA_BASE_URL`      | Yes      | Grafana base URL, e.g. `https://grafana.example.com` |
| `GRAFANA_API_KEY`       | Yes*     | Service account token or legacy API key             |
| `GRAFANA_USERNAME`      | Yes*     | Basic auth username (if not using API key)          |
| `GRAFANA_PASSWORD`      | Yes*     | Basic auth password (if not using API key)          |

\* Either `GRAFANA_API_KEY` **or** `GRAFANA_USERNAME` + `GRAFANA_PASSWORD` must be set.

To create a service account token: **Administration → Service accounts → Add service account**, grant the **Viewer** role, then create a token.

## Adding to a Zerelio Scope

```json
{
  "scope_name": "My Grafana",
  "plugin_name": "grafana",
  "config": {
    "base_url": "https://grafana.example.com"
  },
  "schedule": "@every 1h"
}
```

## Part of Zerelio

This plugin is part of the [Zerelio](https://github.com/zerelio/knowledge) infrastructure knowledge graph platform.
