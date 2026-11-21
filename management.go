package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed ui_template.html
var dashboardHTML []byte

// BuildManagementRegistration returns the management API and resource routes exposed by this plugin.
// Note: ManagementRoute.Path is resolved under /v0/management/ by the host, so "/plugins/quota-auto-balance/status"
// registers at "/v0/management/plugins/quota-auto-balance/status".
// ResourceRoute.Path is resolved under "/v0/resource/plugins/quota-auto-balance/" by the host.
func BuildManagementRegistration() ManagementRegistrationResponse {
	base := "/plugins/" + PluginID + "/"
	return ManagementRegistrationResponse{
		Routes: []ManagementRoute{
			{
				Method:      "GET",
				Path:        base + "status",
				Description: "Get current auto-balance configuration, credential routing state, and quota targets.",
			},
			{
				Method:      "POST",
				Path:        base + "sync",
				Description: "Trigger an immediate quota inspection and auto-balance synchronization.",
			},
			{
				Method:      "POST",
				Path:        base + "restore",
				Description: "Restore all tracked credentials to their original manual priority and weight values.",
			},
			{
				Method:      "POST",
				Path:        base + "config",
				Description: "Update runtime auto-balance configuration.",
			},
		},
		Resources: []ResourceRoute{
			{
				Path:        "/dashboard",
				Menu:        "Quota Auto-Balance",
				Description: "Auto-balance credential priority and weight by remaining quota",
			},
		},
	}
}

type syncRequestPayload struct {
	Quotas map[string]*QuotaState `json:"quotas,omitempty"`
}

// HandleManagementRequest handles incoming HTTP requests to /v0/management/plugins/quota-auto-balance/...
// and /v0/resource/plugins/quota-auto-balance/...
func (e *BalanceEngine) HandleManagementRequest(req ManagementRequest) ManagementResponse {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	subPath := extractSubPath(req.Path)

	switch {
	case method == "GET" && (subPath == "dashboard" || subPath == "" || subPath == "index.html"):
		return ManagementResponse{
			StatusCode: http.StatusOK,
			Headers: map[string][]string{
				"Content-Type":           {"text/html; charset=utf-8"},
				"Cache-Control":          {"no-store"},
				"X-Content-Type-Options": {"nosniff"},
			},
			Body: dashboardHTML,
		}

	case method == "GET" && subPath == "status":
		status, err := e.GetStatus()
		if err != nil {
			return jsonErrorResponse(http.StatusInternalServerError, err.Error())
		}
		return jsonResponse(http.StatusOK, status)

	case method == "POST" && subPath == "sync":
		var payload syncRequestPayload
		if len(req.Body) > 0 {
			_ = json.Unmarshal(req.Body, &payload)
		}
		summary, err := e.SyncAll(payload.Quotas)
		status, _ := e.GetStatus()
		respBody := map[string]any{
			"summary": summary,
			"status":  status,
		}
		if err != nil {
			respBody["error"] = err.Error()
			return jsonResponse(http.StatusInternalServerError, respBody)
		}
		return jsonResponse(http.StatusOK, respBody)

	case method == "POST" && subPath == "restore":
		summary, err := e.RestoreOriginals()
		status, _ := e.GetStatus()
		respBody := map[string]any{
			"summary": summary,
			"status":  status,
		}
		if err != nil {
			respBody["error"] = err.Error()
			return jsonResponse(http.StatusInternalServerError, respBody)
		}
		return jsonResponse(http.StatusOK, respBody)

	case method == "POST" && subPath == "config":
		currentCfg := e.GetConfig()
		currentBytes, _ := json.Marshal(currentCfg)
		mergedCfg := ParseConfig(currentBytes, "")
		if len(req.Body) > 0 {
			var patch map[string]any
			if err := json.Unmarshal(req.Body, &patch); err != nil {
				return jsonErrorResponse(http.StatusBadRequest, "invalid JSON payload")
			}
			applyConfigMap(&mergedCfg, patch)
			normalizeConfig(&mergedCfg)
		}
		e.Configure("", mergedCfg, true)
		status, _ := e.GetStatus()
		return jsonResponse(http.StatusOK, status)

	default:
		return jsonErrorResponse(http.StatusNotFound, "unknown plugin route: "+method+" "+subPath)
	}
}

func extractSubPath(pathStr string) string {
	p := strings.TrimSpace(pathStr)
	p = strings.TrimPrefix(p, "/v0/management/plugins/"+PluginID+"/")
	p = strings.TrimPrefix(p, "/v8/management/plugins/"+PluginID+"/")
	p = strings.TrimPrefix(p, "/plugins/"+PluginID+"/")
	p = strings.TrimPrefix(p, "/v0/resource/plugins/"+PluginID+"/")
	p = strings.Trim(p, "/")
	return p
}

func jsonResponse(status int, payload any) ManagementResponse {
	data, _ := json.Marshal(payload)
	return ManagementResponse{
		StatusCode: status,
		Headers: map[string][]string{
			"Content-Type":           {"application/json; charset=utf-8"},
			"Cache-Control":          {"no-store"},
			"X-Content-Type-Options": {"nosniff"},
		},
		Body: data,
	}
}

func jsonErrorResponse(status int, message string) ManagementResponse {
	return jsonResponse(status, map[string]any{
		"error": message,
	})
}
