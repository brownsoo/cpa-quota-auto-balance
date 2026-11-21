package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestDashboardHTML_NoUnsafeDOMSinks(t *testing.T) {
	html := string(dashboardHTML)

	// Guard against regressions that reintroduce HTML string sinks in the dashboard script.
	unsafeSinks := []string{
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
		"document.write",
	}
	for _, sink := range unsafeSinks {
		if strings.Contains(html, sink) {
			t.Errorf("dashboard HTML must not use unsafe DOM sink %q", sink)
		}
	}

	if !strings.Contains(html, "textContent") {
		t.Errorf("expected dashboard HTML to render untrusted text via textContent")
	}
}

func TestDashboardResponseHeaders(t *testing.T) {
	engine := NewBalanceEngine(HostClient{})
	resp := engine.HandleManagementRequest(ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/" + PluginID + "/dashboard",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Headers["X-Content-Type-Options"]; len(got) == 0 || got[0] != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff, got %v", got)
	}
}
