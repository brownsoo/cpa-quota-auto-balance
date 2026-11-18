package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	codexUsageURL       = "https://chatgpt.com/backend-api/wham/usage"
	claudeUsageURL      = "https://api.anthropic.com/api/oauth/usage"
	kimiUsageURL        = "https://api.kimi.com/coding/v1/usages"
	xaiApiKeyURL        = "https://api.x.ai/v1/api-key"
	antigravityQuotaURL = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
)

// NormalizeProvider normalizes provider/type identifiers from credential files.
func NormalizeProvider(entry HostAuthFileEntry, rawDoc map[string]any) string {
	candidates := []string{entry.Provider, entry.Type}
	if rawDoc != nil {
		if v, ok := rawDoc["type"].(string); ok {
			candidates = append(candidates, v)
		}
		if v, ok := rawDoc["provider"].(string); ok {
			candidates = append(candidates, v)
		}
	}
	for _, c := range candidates {
		lower := strings.ToLower(strings.TrimSpace(c))
		switch lower {
		case "codex", "openai":
			return "codex"
		case "claude", "anthropic":
			return "claude"
		case "antigravity", "gemini", "gemini-cli", "aistudio":
			return "antigravity"
		case "kimi", "moonshot":
			return "kimi"
		case "xai", "grok":
			return "xai"
		case "devin":
			return "devin"
		}
	}
	nameLower := strings.ToLower(entry.Name)
	for _, prefix := range []string{"codex", "claude", "antigravity", "kimi", "xai", "devin"} {
		if strings.HasPrefix(nameLower, prefix) {
			return prefix
		}
	}
	if entry.Provider != "" {
		return strings.ToLower(strings.TrimSpace(entry.Provider))
	}
	return strings.ToLower(strings.TrimSpace(entry.Type))
}

func extractAccessToken(doc map[string]any) string {
	for _, key := range []string{"access_token", "accessToken", "api_key", "apiKey", "token"} {
		if v, ok := doc[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if nested, ok := doc["token"].(map[string]any); ok {
		for _, key := range []string{"access_token", "accessToken"} {
			if v, ok := nested[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func extractAccountID(doc map[string]any) string {
	for _, key := range []string{"account_id", "accountId", "chatgpt_account_id"} {
		if v, ok := doc[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if idToken, ok := doc["id_token"].(string); ok && idToken != "" {
		if payload := decodeJWTPayload(idToken); payload != nil {
			if v, ok := payload["chatgpt_account_id"].(string); ok && v != "" {
				return v
			}
			if authObj, ok := payload["https://api.openai.com/auth"].(map[string]any); ok {
				if v, ok := authObj["chatgpt_account_id"].(string); ok && v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func decodeJWTPayload(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(decoded, &out); err != nil {
		return nil
	}
	return out
}

// FetchCredentialQuota queries the upstream provider usage API via host.http.do.
func FetchCredentialQuota(entry HostAuthFileEntry, rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error)) *QuotaState {
	provider := NormalizeProvider(entry, rawDoc)
	now := time.Now().UnixMilli()

	switch provider {
	case "codex":
		return fetchCodexQuota(rawDoc, httpDo, now)
	case "claude":
		return fetchClaudeQuota(rawDoc, httpDo, now)
	case "kimi":
		return fetchKimiQuota(rawDoc, httpDo, now)
	case "xai":
		return fetchXaiQuota(rawDoc, httpDo, now)
	case "antigravity":
		return fetchAntigravityQuota(rawDoc, httpDo, now)
	default:
		return &QuotaState{
			Status:    "unsupported",
			UpdatedAt: now,
		}
	}
}

func fetchCodexQuota(rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error), now int64) *QuotaState {
	headers := map[string][]string{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json"},
		"User-Agent":   {"codex_cli_rs/0.76.0"},
	}
	if token := extractAccessToken(rawDoc); token != "" {
		headers["Authorization"] = []string{"Bearer " + token}
	}
	if accountID := extractAccountID(rawDoc); accountID != "" {
		headers["Chatgpt-Account-Id"] = []string{accountID}
	}

	resp, err := httpDo(HostHTTPRequest{
		Method:  "GET",
		URL:     codexUsageURL,
		Headers: headers,
	})
	if err != nil {
		return &QuotaState{Status: "error", Error: err.Error(), UpdatedAt: now}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &QuotaState{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), UpdatedAt: now}
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return &QuotaState{Status: "error", Error: "invalid JSON response", UpdatedAt: now}
	}

	rows := parseCodexUsageRows(payload)
	if len(rows) == 0 {
		return &QuotaState{Status: "error", Error: "no quota windows found", UpdatedAt: now}
	}
	return &QuotaState{Status: "ok", Rows: rows, UpdatedAt: now}
}

func parseCodexUsageRows(payload map[string]any) []QuotaRow {
	var rows []QuotaRow
	addRateLimit := func(rl map[string]any, prefix string, scoped bool) {
		if rl == nil {
			return
		}
		if pw, ok := rl["primary_window"].(map[string]any); ok {
			if used, ok := toFloat(pw["used_percent"]); ok {
				label := "5h"
				if prefix != "" {
					label = prefix + " 5h"
				}
				rows = append(rows, QuotaRow{
					Label:      label,
					Percent:    clampPercent(100 - used),
					Scoped:     scoped,
					GroupLabel: prefix,
				})
			}
		}
		if sw, ok := rl["secondary_window"].(map[string]any); ok {
			if used, ok := toFloat(sw["used_percent"]); ok {
				label := "7d"
				if prefix != "" {
					label = prefix + " 7d"
				}
				rows = append(rows, QuotaRow{
					Label:      label,
					Percent:    clampPercent(100 - used),
					Scoped:     scoped,
					GroupLabel: prefix,
				})
			}
		}
	}

	if rl, ok := payload["rate_limit"].(map[string]any); ok {
		addRateLimit(rl, "", false)
	}
	if crl, ok := payload["code_review_rate_limit"].(map[string]any); ok {
		addRateLimit(crl, "Code Review", true)
	}
	return rows
}

func fetchClaudeQuota(rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error), now int64) *QuotaState {
	headers := map[string][]string{
		"Accept":         {"application/json"},
		"Content-Type":   {"application/json"},
		"anthropic-beta": {"oauth-2025-04-20"},
	}
	if token := extractAccessToken(rawDoc); token != "" {
		headers["Authorization"] = []string{"Bearer " + token}
	}

	resp, err := httpDo(HostHTTPRequest{
		Method:  "GET",
		URL:     claudeUsageURL,
		Headers: headers,
	})
	if err != nil {
		return &QuotaState{Status: "error", Error: err.Error(), UpdatedAt: now}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &QuotaState{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), UpdatedAt: now}
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return &QuotaState{Status: "error", Error: "invalid JSON response", UpdatedAt: now}
	}

	var rows []QuotaRow
	windowSpecs := []struct {
		key    string
		label  string
		scoped bool
	}{
		{"five_hour", "5h", false},
		{"seven_day", "7d", false},
		{"seven_day_sonnet", "7d Sonnet", true},
		{"seven_day_opus", "7d Opus", true},
		{"seven_day_oauth_apps", "7d OAuth Apps", true},
	}

	for _, spec := range windowSpecs {
		win, ok := payload[spec.key].(map[string]any)
		if !ok || win == nil {
			continue
		}
		util, ok := toFloat(win["utilization"])
		if !ok {
			continue
		}
		group := ""
		if spec.scoped {
			group = spec.label
		}
		rows = append(rows, QuotaRow{
			Label:      spec.label,
			Percent:    clampPercent(100 - util),
			Scoped:     spec.scoped,
			GroupLabel: group,
		})
	}

	if len(rows) == 0 {
		return &QuotaState{Status: "error", Error: "no quota windows found", UpdatedAt: now}
	}
	return &QuotaState{Status: "ok", Rows: rows, UpdatedAt: now}
}

func fetchKimiQuota(rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error), now int64) *QuotaState {
	headers := map[string][]string{
		"Accept": {"application/json"},
	}
	if token := extractAccessToken(rawDoc); token != "" {
		headers["Authorization"] = []string{"Bearer " + token}
	}

	resp, err := httpDo(HostHTTPRequest{
		Method:  "GET",
		URL:     kimiUsageURL,
		Headers: headers,
	})
	if err != nil {
		return &QuotaState{Status: "error", Error: err.Error(), UpdatedAt: now}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &QuotaState{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), UpdatedAt: now}
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return &QuotaState{Status: "error", Error: "invalid JSON response", UpdatedAt: now}
	}

	var rows []QuotaRow
	if usage, ok := payload["usage"].(map[string]any); ok {
		if pct, ok := computeRemainingFromLimitUsed(usage); ok {
			rows = append(rows, QuotaRow{Label: "Weekly", Percent: pct})
		}
	}
	if limits, ok := payload["limits"].([]any); ok {
		for _, item := range limits {
			lm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			detail, _ := lm["detail"].(map[string]any)
			if detail == nil {
				detail = lm
			}
			if pct, ok := computeRemainingFromLimitUsed(detail); ok {
				rows = append(rows, QuotaRow{Label: "Rate Limit", Percent: pct})
			}
		}
	}

	if len(rows) == 0 {
		return &QuotaState{Status: "error", Error: "no quota windows found", UpdatedAt: now}
	}
	return &QuotaState{Status: "ok", Rows: rows, UpdatedAt: now}
}

func computeRemainingFromLimitUsed(m map[string]any) (float64, bool) {
	limit, okL := toFloat(m["limit"])
	if !okL || limit <= 0 {
		return 0, false
	}
	if rem, okR := toFloat(m["remaining"]); okR {
		return clampPercent((rem / limit) * 100), true
	}
	if used, okU := toFloat(m["used"]); okU {
		return clampPercent(((limit - used) / limit) * 100), true
	}
	return 0, false
}

func fetchXaiQuota(rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error), now int64) *QuotaState {
	headers := map[string][]string{
		"Accept": {"application/json"},
	}
	if token := extractAccessToken(rawDoc); token != "" {
		headers["Authorization"] = []string{"Bearer " + token}
	}

	resp, err := httpDo(HostHTTPRequest{
		Method:  "GET",
		URL:     xaiApiKeyURL,
		Headers: headers,
	})
	if err != nil {
		return &QuotaState{Status: "error", Error: err.Error(), UpdatedAt: now}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &QuotaState{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), UpdatedAt: now}
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return &QuotaState{Status: "error", Error: "invalid JSON response", UpdatedAt: now}
	}

	blocked, _ := toBool(payload["team_blocked"])
	disabled, _ := toBool(payload["disabled"])
	aclBlocked, _ := toBool(payload["modified_by_acl"])
	pct := 100.0
	if blocked || disabled || aclBlocked {
		pct = 0.0
	}
	return &QuotaState{
		Status:    "ok",
		Rows:      []QuotaRow{{Label: "API Key", Percent: pct}},
		UpdatedAt: now,
	}
}

func fetchAntigravityQuota(rawDoc map[string]any, httpDo func(HostHTTPRequest) (*HostHTTPResponse, error), now int64) *QuotaState {
	headers := map[string][]string{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json"},
		"User-Agent":   {"antigravity/1.11.5"},
	}
	if token := extractAccessToken(rawDoc); token != "" {
		headers["Authorization"] = []string{"Bearer " + token}
	}

	bodyBytes := []byte("{}")
	if proj, ok := rawDoc["project_id"].(string); ok && strings.TrimSpace(proj) != "" {
		bodyBytes, _ = json.Marshal(map[string]string{"project": strings.TrimSpace(proj)})
	}

	resp, err := httpDo(HostHTTPRequest{
		Method:  "POST",
		URL:     antigravityQuotaURL,
		Headers: headers,
		Body:    bodyBytes,
	})
	if err != nil {
		return &QuotaState{Status: "error", Error: err.Error(), UpdatedAt: now}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &QuotaState{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode), UpdatedAt: now}
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return &QuotaState{Status: "error", Error: "invalid JSON response", UpdatedAt: now}
	}

	modelsMap, ok := payload["models"].(map[string]any)
	if !ok || len(modelsMap) == 0 {
		return &QuotaState{Status: "error", Error: "no models quota returned", UpdatedAt: now}
	}

	var rows []QuotaRow
	for modelID, rawModel := range modelsMap {
		mObj, ok := rawModel.(map[string]any)
		if !ok {
			continue
		}
		qInfo, ok := mObj["quotaInfo"].(map[string]any)
		if !ok {
			continue
		}
		frac, ok := toFloat(qInfo["remainingFraction"])
		if !ok || math.IsNaN(frac) {
			continue
		}
		group := classifyAntigravityGroup(modelID)
		rows = append(rows, QuotaRow{
			Label:      modelID,
			Percent:    clampPercent(frac * 100),
			Scoped:     true,
			GroupLabel: group,
		})
	}

	if len(rows) == 0 {
		return &QuotaState{Status: "error", Error: "no model quota fractions found", UpdatedAt: now}
	}
	return &QuotaState{Status: "ok", Rows: rows, UpdatedAt: now}
}

func classifyAntigravityGroup(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "claude") || strings.Contains(lower, "gpt"):
		return "Claude / GPT"
	case strings.Contains(lower, "pro"):
		return "Gemini Pro"
	case strings.Contains(lower, "flash"):
		return "Gemini Flash"
	default:
		return modelID
	}
}
