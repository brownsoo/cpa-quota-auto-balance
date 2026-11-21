package main

import "encoding/json"

const (
	PluginID         = "quota-auto-balance"
	PluginName       = "Quota Auto-Balance"
	PluginVersion    = "0.1.2"
	PluginAuthor     = "brownsoo"
	PluginRepository = "https://github.com/brownsoo/cpa-quota-auto-balance"

	ABIVersion    = 1
	SchemaVersion = 10

	MethodPluginRegister     = "plugin.register"
	MethodPluginQuiesce      = "plugin.quiesce"
	MethodPluginReconfigure  = "plugin.reconfigure"
	MethodPluginShutdown     = "plugin.shutdown"
	MethodManagementRegister = "management.register"
	MethodManagementHandle   = "management.handle"

	HostMethodAuthList = "host.auth.list"
	HostMethodAuthGet  = "host.auth.get"
	HostMethodAuthSave = "host.auth.save"
	HostMethodHTTPDo   = "host.http.do"
)

// Envelope wraps plugin and host JSON messages.
type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorInfo      `json:"error,omitempty"`
}

// ErrorInfo describes a structured plugin or host error.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ConfigField defines a user-configurable setting exposed in the host UI (matches pluginapi.ConfigField).
type ConfigField struct {
	Name        string   `json:"Name"`
	Type        string   `json:"Type"`
	Default     any      `json:"Default,omitempty"`
	EnumValues  []string `json:"EnumValues,omitempty"`
	Description string   `json:"Description,omitempty"`
}

// Metadata describes the plugin registration metadata (matches pluginapi.Metadata).
// Note: Host's validPlugin() requires Name, Version, Author, and GitHubRepository to all be non-empty.
type Metadata struct {
	Name             string        `json:"Name"`
	Version          string        `json:"Version"`
	Author           string        `json:"Author"`
	GitHubRepository string        `json:"GitHubRepository"`
	Logo             string        `json:"Logo,omitempty"`
	ConfigFields     []ConfigField `json:"ConfigFields,omitempty"`
}

// RPCCapabilities declares the integration hooks implemented by the plugin (matches pluginhost.rpcCapabilities).
type RPCCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

// LifecycleRequest is passed by the host on plugin.register and plugin.reconfigure (matches pluginhost.rpcLifecycleRequest).
type LifecycleRequest struct {
	ConfigYAML         json.RawMessage `json:"config_yaml,omitempty"`
	Config             json.RawMessage `json:"config,omitempty"`
	PluginDir          string          `json:"plugin_dir,omitempty"`
	SchemaVersion      uint32          `json:"schema_version,omitempty"`
	MultiAuthProviders bool            `json:"multi_auth_providers,omitempty"`
}

// RegistrationResponse is returned by both plugin.register and plugin.reconfigure (matches pluginhost.rpcRegistration).
type RegistrationResponse struct {
	SchemaVersion uint32          `json:"schema_version"`
	Metadata      Metadata        `json:"metadata"`
	Capabilities  RPCCapabilities `json:"capabilities"`
}

// ManagementRoute declares a route under /v0/management/... (matches pluginapi.ManagementRoute).
type ManagementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Menu        string `json:"Menu,omitempty"`
	Description string `json:"Description,omitempty"`
}

// ResourceRoute declares a browser-accessible route under /v0/resource/plugins/<id>/... (matches pluginapi.ResourceRoute).
type ResourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu,omitempty"`
	Description string `json:"Description,omitempty"`
}

// ManagementRegistrationResponse is returned by management.register (matches pluginhost.rpcManagementRegistrationResponse).
type ManagementRegistrationResponse struct {
	Routes    []ManagementRoute `json:"routes,omitempty"`
	Resources []ResourceRoute   `json:"resources,omitempty"`
}

// ManagementRequest represents an HTTP request forwarded to management.handle (matches pluginhost.rpcManagementRequest).
type ManagementRequest struct {
	Method         string              `json:"Method"`
	Path           string              `json:"Path"`
	Headers        map[string][]string `json:"Headers,omitempty"`
	Query          map[string][]string `json:"Query,omitempty"`
	Body           []byte              `json:"Body,omitempty"`
	HostCallbackID string              `json:"host_callback_id,omitempty"`
}

// ManagementResponse represents an HTTP response returned by management.handle (matches pluginapi.ManagementResponse).
type ManagementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body,omitempty"`
}

// HostAuthFileEntry represents an entry returned by host.auth.list (matches pluginapi.HostAuthFileEntry).
type HostAuthFileEntry struct {
	ID        string `json:"id,omitempty"`
	AuthIndex string `json:"auth_index,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Label     string `json:"label,omitempty"`
	Status    string `json:"status,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Path      string `json:"path,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

// HostAuthListResponse wraps the response from host.auth.list.
type HostAuthListResponse struct {
	Files []HostAuthFileEntry `json:"files"`
}

// HostAuthGetRequest is sent to host.auth.get (matches pluginapi.HostAuthGetRequest).
type HostAuthGetRequest struct {
	AuthIndex string `json:"auth_index"`
}

// HostAuthGetResponse is returned by host.auth.get (matches pluginapi.HostAuthGetResponse).
type HostAuthGetResponse struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"`
	JSON      json.RawMessage `json:"json"`
}

// HostAuthSaveRequest is sent to host.auth.save (matches pluginapi.HostAuthSaveRequest).
type HostAuthSaveRequest struct {
	Name string          `json:"name"`
	JSON json.RawMessage `json:"json"`
}

// HostAuthSaveResponse is returned by host.auth.save (matches pluginapi.HostAuthSaveResponse).
type HostAuthSaveResponse struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// HostHTTPRequest is sent to host.http.do (matches pluginhost.rpcHostHTTPRequest).
type HostHTTPRequest struct {
	HostCallbackID string              `json:"host_callback_id,omitempty"`
	Method         string              `json:"method"`
	URL            string              `json:"url"`
	Headers        map[string][]string `json:"headers,omitempty"`
	Body           []byte              `json:"body,omitempty"`
}

// HostHTTPResponse is returned by host.http.do (matches pluginapi.HTTPResponse).
type HostHTTPResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body,omitempty"`
}

// QuotaRow represents a single quota window (e.g. 5h, 7d, model group).
type QuotaRow struct {
	Label      string  `json:"label"`
	Percent    float64 `json:"percent"`
	Reset      string  `json:"reset,omitempty"`
	Scoped     bool    `json:"scoped,omitempty"`
	GroupLabel string  `json:"groupLabel,omitempty"`
}

// QuotaState holds the quota inspection result for a single credential.
type QuotaState struct {
	Status    string     `json:"status"` // "ok", "error", "unsupported", "loading"
	Rows      []QuotaRow `json:"rows,omitempty"`
	Error     string     `json:"error,omitempty"`
	UpdatedAt int64      `json:"updatedAt,omitempty"`
}

// QuotaRoutingTarget holds the computed routing priority and weight for a credential.
type QuotaRoutingTarget struct {
	Priority          int     `json:"priority"`
	Weight            int     `json:"weight"`
	Tier              string  `json:"tier"` // "healthy", "low", "critical"
	BottleneckPercent float64 `json:"bottleneckPercent"`
	AveragePercent    float64 `json:"averagePercent"`
}

// AppliedRouting records the priority and weight values last written by auto-balance.
type AppliedRouting struct {
	Priority int `json:"priority"`
	Weight   int `json:"weight"`
}

// StoredOriginal preserves the user's manual priority and weight before auto-balance modifies them.
type StoredOriginal struct {
	Priority *int            `json:"priority"`
	Weight   *int            `json:"weight"`
	Applied  *AppliedRouting `json:"applied,omitempty"`
}

// CredentialBalanceStatus summarizes a credential's current routing and quota balance state for the UI/API.
type CredentialBalanceStatus struct {
	Name             string              `json:"name"`
	Provider         string              `json:"provider"`
	Label            string              `json:"label,omitempty"`
	Disabled         bool                `json:"disabled,omitempty"`
	CurrentPriority  *int                `json:"currentPriority"`
	CurrentWeight    *int                `json:"currentWeight"`
	OriginalPriority *int                `json:"originalPriority,omitempty"`
	OriginalWeight   *int                `json:"originalWeight,omitempty"`
	HasOriginal      bool                `json:"hasOriginal"`
	Quota            *QuotaState         `json:"quota,omitempty"`
	Target           *QuotaRoutingTarget `json:"target,omitempty"`
	Modified         bool                `json:"modified"`
}
