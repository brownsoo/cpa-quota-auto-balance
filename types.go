package main

import "encoding/json"

const (
	PluginID      = "quota-auto-balance"
	PluginName    = "Quota Auto-Balance"
	PluginVersion = "0.1.0"
	ABIVersion    = 1

	CapabilityManagementAPI = "management_api"

	MethodPluginRegister     = "plugin.register"
	MethodPluginReconfigure  = "plugin.reconfigure"
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
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ConfigFieldOption describes an option in a select config field.
type ConfigFieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ConfigField defines a user-configurable setting exposed in the host UI.
type ConfigField struct {
	Name        string              `json:"name"`
	Label       string              `json:"label,omitempty"`
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Required    bool                `json:"required,omitempty"`
	Default     any                 `json:"default,omitempty"`
	Placeholder string              `json:"placeholder,omitempty"`
	Options     []ConfigFieldOption `json:"options,omitempty"`
}

// Metadata describes the plugin registration metadata.
type Metadata struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	Description   string        `json:"description,omitempty"`
	Capabilities  []string      `json:"capabilities"`
	Providers     []string      `json:"providers,omitempty"`
	EndpointTypes []string      `json:"endpoint_types,omitempty"`
	Priority      int           `json:"priority,omitempty"`
	ConfigFields  []ConfigField `json:"config_fields,omitempty"`
}

// RegistrationRequest is passed by the host on plugin.register.
type RegistrationRequest struct {
	HostVersion string          `json:"host_version,omitempty"`
	PluginDir   string          `json:"plugin_dir,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
	ConfigYAML  string          `json:"config_yaml,omitempty"`
}

// RegistrationResponse is returned by plugin.register.
type RegistrationResponse struct {
	Metadata Metadata `json:"metadata"`
}

// ReconfigureRequest is passed by the host on plugin.reconfigure.
type ReconfigureRequest struct {
	PluginDir  string          `json:"plugin_dir,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
	ConfigYAML string          `json:"config_yaml,omitempty"`
}

// ReconfigureResponse is returned by plugin.reconfigure.
type ReconfigureResponse struct {
	Applied bool `json:"applied"`
}

// ManagementRoute declares a route under /v0/management/plugins/<id>/...
type ManagementRoute struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// ResourceRoute declares a browser-accessible route under /v0/resource/plugins/<id>/...
type ResourceRoute struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Menu        string `json:"menu,omitempty"`
	Description string `json:"description,omitempty"`
}

// ManagementRegistrationResponse is returned by management.register.
type ManagementRegistrationResponse struct {
	Routes    []ManagementRoute `json:"routes"`
	Resources []ResourceRoute   `json:"resources,omitempty"`
}

// ManagementRequest represents an HTTP request forwarded to management.handle.
type ManagementRequest struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	RawPath string              `json:"raw_path,omitempty"`
	Query   string              `json:"query,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// ManagementResponse represents an HTTP response returned by management.handle.
type ManagementResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// HostAuthFileEntry represents an entry returned by host.auth.list.
type HostAuthFileEntry struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Type     string         `json:"type,omitempty"`
	Provider string         `json:"provider,omitempty"`
	Label    string         `json:"label,omitempty"`
	Status   string         `json:"status,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
	Priority *int           `json:"priority,omitempty"`
	Weight   *int           `json:"weight,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// HostAuthListResponse wraps the response from host.auth.list.
type HostAuthListResponse struct {
	Files []HostAuthFileEntry `json:"files"`
}

// HostAuthGetRequest is sent to host.auth.get.
type HostAuthGetRequest struct {
	Name string `json:"name"`
}

// HostAuthGetResponse is returned by host.auth.get.
type HostAuthGetResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Type     string         `json:"type,omitempty"`
	Provider string         `json:"provider,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Raw      string         `json:"raw,omitempty"`
}

// HostAuthSaveRequest is sent to host.auth.save.
type HostAuthSaveRequest struct {
	Name     string         `json:"name"`
	Raw      string         `json:"raw,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// HostAuthSaveResponse is returned by host.auth.save.
type HostAuthSaveResponse struct {
	Saved bool   `json:"saved"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
}

// HostHTTPRequest is sent to host.http.do.
type HostHTTPRequest struct {
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           []byte            `json:"body,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	AuthID         string            `json:"auth_id,omitempty"`
}

// HostHTTPResponse is returned by host.http.do.
type HostHTTPResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
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
