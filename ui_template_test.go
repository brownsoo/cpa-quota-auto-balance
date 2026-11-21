package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var (
	scriptBlockRe       = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	jsBlockCommentRe    = regexp.MustCompile(`(?s)/\*.*?\*/`)
	jsLineCommentRe     = regexp.MustCompile(`(?m)//[^\n]*`)
	inlineEventAttrRe   = regexp.MustCompile(`(?i)\bon[a-z]+\s*=`)
)

func TestDashboardHTML_NoUnsafeDOMSinksOrInlineHandlers(t *testing.T) {
	html := string(dashboardHTML)

	// Verify static markup contains no inline event handler attributes (e.g. onclick=, onchange=).
	markupWithoutScript := scriptBlockRe.ReplaceAllString(html, "")
	if match := inlineEventAttrRe.FindString(markupWithoutScript); match != "" {
		t.Errorf("dashboard HTML markup must not contain inline event handler %q", match)
	}

	matches := scriptBlockRe.FindAllStringSubmatch(html, -1)
	if len(matches) != 1 {
		t.Fatalf("expected 1 inline <script> block, found %d", len(matches))
	}

	// Strip JS comments before checking for dangerous sinks so explanatory comments do not false-positive.
	scriptCode := jsBlockCommentRe.ReplaceAllString(matches[0][1], "")
	scriptCode = jsLineCommentRe.ReplaceAllString(scriptCode, "")

	unsafeSinks := []string{
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
		"document.write",
	}
	for _, sink := range unsafeSinks {
		if strings.Contains(scriptCode, sink) {
			t.Errorf("dashboard script must not use unsafe DOM sink %q", sink)
		}
	}

	if !strings.Contains(scriptCode, "textContent") {
		t.Errorf("expected dashboard script to render untrusted text via textContent")
	}
}

func TestManagementResponseSecurityHeaders(t *testing.T) {
	engine := NewBalanceEngine(HostClient{})

	dashResp := engine.HandleManagementRequest(ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/" + PluginID + "/dashboard",
	})
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d", dashResp.StatusCode)
	}
	if got := dashResp.Headers["X-Content-Type-Options"]; len(got) == 0 || got[0] != "nosniff" {
		t.Fatalf("expected dashboard X-Content-Type-Options: nosniff, got %v", got)
	}

	notFoundResp := engine.HandleManagementRequest(ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/management/plugins/" + PluginID + "/nonexistent",
	})
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", notFoundResp.StatusCode)
	}
	if got := notFoundResp.Headers["X-Content-Type-Options"]; len(got) == 0 || got[0] != "nosniff" {
		t.Fatalf("expected JSON response X-Content-Type-Options: nosniff, got %v", got)
	}
}
