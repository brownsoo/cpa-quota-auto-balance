package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Config holds the runtime configuration for Quota Auto-Balance.
type Config struct {
	AutoBalanceEnabled  bool    `json:"auto_balance_enabled"`
	SyncIntervalSeconds int     `json:"sync_interval_seconds"`
	HealthyThreshold    float64 `json:"healthy_threshold"`
	CriticalThreshold   float64 `json:"critical_threshold"`
	PriorityHealthy     int     `json:"priority_healthy"`
	PriorityLow         int     `json:"priority_low"`
	PriorityCritical    int     `json:"priority_critical"`
	RestoreOnDisable    bool    `json:"restore_on_disable"`
}

// DefaultConfig returns the default auto-balance configuration matching EasyCLIProxyAPI's design.
func DefaultConfig() Config {
	return Config{
		AutoBalanceEnabled:  true,
		SyncIntervalSeconds: 300,
		HealthyThreshold:    15.0,
		CriticalThreshold:   5.0,
		PriorityHealthy:     0,
		PriorityLow:         -5,
		PriorityCritical:    -10,
		RestoreOnDisable:    true,
	}
}

// PluginConfigFields returns the schema of user-configurable fields displayed in the host UI.
func PluginConfigFields() []ConfigField {
	def := DefaultConfig()
	return []ConfigField{
		{
			Name:        "auto_balance_enabled",
			Label:       "Enable Auto-Balance",
			Type:        "boolean",
			Description: "Automatically synchronize credential priority and weight based on remaining quota windows.",
			Default:     def.AutoBalanceEnabled,
		},
		{
			Name:        "sync_interval_seconds",
			Label:       "Sync Interval (seconds)",
			Type:        "number",
			Description: "Background quota polling and auto-balance interval in seconds (0 to disable periodic polling).",
			Default:     def.SyncIntervalSeconds,
		},
		{
			Name:        "healthy_threshold",
			Label:       "Healthy Quota Threshold (%)",
			Type:        "number",
			Description: "Minimum bottleneck remaining quota percentage for the Healthy priority tier.",
			Default:     def.HealthyThreshold,
		},
		{
			Name:        "critical_threshold",
			Label:       "Critical Quota Threshold (%)",
			Type:        "number",
			Description: "Bottleneck remaining quota percentage below which a credential enters the Critical tier (weight = 1).",
			Default:     def.CriticalThreshold,
		},
		{
			Name:        "priority_healthy",
			Label:       "Healthy Tier Priority",
			Type:        "number",
			Description: "Routing priority assigned when bottleneck quota >= Healthy Threshold (default 0 matches CPA default).",
			Default:     def.PriorityHealthy,
		},
		{
			Name:        "priority_low",
			Label:       "Low Tier Priority",
			Type:        "number",
			Description: "Routing priority assigned when Critical Threshold <= bottleneck quota < Healthy Threshold.",
			Default:     def.PriorityLow,
		},
		{
			Name:        "priority_critical",
			Label:       "Critical Tier Priority",
			Type:        "number",
			Description: "Routing priority assigned when bottleneck quota < Critical Threshold.",
			Default:     def.PriorityCritical,
		},
		{
			Name:        "restore_on_disable",
			Label:       "Restore Originals on Disable",
			Type:        "boolean",
			Description: "Automatically restore manual priority and weight values when Auto-Balance is turned off.",
			Default:     def.RestoreOnDisable,
		},
	}
}

// ParseConfig parses a Config from raw JSON and/or YAML strings, falling back to DefaultConfig().
func ParseConfig(rawJSON json.RawMessage, rawYAML string) Config {
	cfg := DefaultConfig()

	if len(rawJSON) > 0 && string(rawJSON) != "null" {
		var m map[string]any
		if err := json.Unmarshal(rawJSON, &m); err == nil {
			applyConfigMap(&cfg, m)
		}
	}

	if strings.TrimSpace(rawYAML) != "" {
		m := parseSimpleYAMLMap(rawYAML)
		applyConfigMap(&cfg, m)
	}

	normalizeConfig(&cfg)
	return cfg
}

func normalizeConfig(cfg *Config) {
	if cfg.SyncIntervalSeconds < 0 {
		cfg.SyncIntervalSeconds = 0
	}
	if cfg.CriticalThreshold < 0 {
		cfg.CriticalThreshold = 0
	}
	if cfg.CriticalThreshold > 100 {
		cfg.CriticalThreshold = 100
	}
	if cfg.HealthyThreshold < cfg.CriticalThreshold {
		cfg.HealthyThreshold = cfg.CriticalThreshold
	}
	if cfg.HealthyThreshold > 100 {
		cfg.HealthyThreshold = 100
	}
}

func applyConfigMap(cfg *Config, m map[string]any) {
	if v, ok := m["auto_balance_enabled"]; ok {
		if b, ok := toBool(v); ok {
			cfg.AutoBalanceEnabled = b
		}
	}
	if v, ok := m["sync_interval_seconds"]; ok {
		if n, ok := toInt(v); ok {
			cfg.SyncIntervalSeconds = n
		}
	}
	if v, ok := m["healthy_threshold"]; ok {
		if f, ok := toFloat(v); ok {
			cfg.HealthyThreshold = f
		}
	}
	if v, ok := m["critical_threshold"]; ok {
		if f, ok := toFloat(v); ok {
			cfg.CriticalThreshold = f
		}
	}
	if v, ok := m["priority_healthy"]; ok {
		if n, ok := toInt(v); ok {
			cfg.PriorityHealthy = n
		}
	}
	if v, ok := m["priority_low"]; ok {
		if n, ok := toInt(v); ok {
			cfg.PriorityLow = n
		}
	}
	if v, ok := m["priority_critical"]; ok {
		if n, ok := toInt(v); ok {
			cfg.PriorityCritical = n
		}
	}
	if v, ok := m["restore_on_disable"]; ok {
		if b, ok := toBool(v); ok {
			cfg.RestoreOnDisable = b
		}
	}
}

func parseSimpleYAMLMap(yamlStr string) map[string]any {
	out := make(map[string]any)
	// First check if the YAML string is actually valid JSON
	trimmed := strings.TrimSpace(yamlStr)
	if strings.HasPrefix(trimmed, "{") {
		_ = json.Unmarshal([]byte(trimmed), &out)
		return out
	}

	lines := strings.Split(yamlStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if commentIdx := strings.Index(val, " #"); commentIdx >= 0 {
			val = strings.TrimSpace(val[:commentIdx])
		}
		val = strings.Trim(val, `"'`)
		out[key] = val
	}
	return out
}

func toBool(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		return b, err == nil
	case float64:
		return val != 0, true
	case int:
		return val != 0, true
	default:
		return false, false
	}
}

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case json.Number:
		i, err := val.Int64()
		return int(i), err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return int(f), err == nil
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
