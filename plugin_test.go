package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	plugintypes "github.com/zerelio/plugin-sdk/plugin"
)

func TestGrafanaPlugin_ValidateConfig(t *testing.T) {
	p := &GrafanaPlugin{}
	r, _ := p.ValidateConfig(context.Background(), map[string]string{})
	if r.Valid { t.Fatal("expected invalid") }
	r, _ = p.ValidateConfig(context.Background(), map[string]string{"base_url": "http://g:3000"})
	if !r.Valid { t.Fatal("expected valid") }
}

func TestGrafanaPlugin_Discover(t *testing.T) {
	p := &GrafanaPlugin{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{{"uid":"abc","title":"Dash","type":"dash-db"}})
	})
	mux.HandleFunc("/api/datasources", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{{"id":float64(1),"name":"Prom","type":"prometheus"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ch, _ := p.Discover(context.Background(), &plugintypes.DiscoverRequest{Config: map[string]string{"base_url": srv.URL, "api_key": "test"}})
	var resources int
	for evt := range ch { if evt.Resource != nil { resources++ } }
	if resources != 2 { t.Fatalf("expected 2, got %d", resources) }
}
