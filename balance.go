package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func clampPercent(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(0, math.Min(100, value))
}

func roundToOneDecimal(value float64) float64 {
	return math.Round(value*10) / 10
}

// RoutingPercents extracts the bottleneck percentage and window values from a credential's quota rows.
//
// Account-wide windows (e.g., Codex/Claude 5h + 7d) all gate the credential simultaneously, so the tightest
// window is the true bottleneck.
//
// Scoped/grouped windows (e.g., Antigravity per-model groups like Claude/GPT vs Gemini Pro vs Gemini Flash)
// only gate their respective model group; exhaustion in one group does not block requests for other groups on
// the same credential. However, rows within the same group share a pool (bottleneck = min within group).
func RoutingPercents(rows []QuotaRow) (bottleneck float64, values []float64, ok bool) {
	var account []float64
	grouped := make(map[string][]float64)
	var groupOrder []string

	for _, row := range rows {
		if math.IsNaN(row.Percent) || math.IsInf(row.Percent, 0) || row.Percent < 0 {
			continue
		}
		pct := clampPercent(row.Percent)
		groupLabel := strings.TrimSpace(row.GroupLabel)
		if !row.Scoped && groupLabel == "" {
			account = append(account, pct)
		} else {
			key := groupLabel
			if key == "" {
				key = row.Label
			}
			if _, exists := grouped[key]; !exists {
				groupOrder = append(groupOrder, key)
			}
			grouped[key] = append(grouped[key], pct)
		}
	}

	if len(account) > 0 {
		minVal := account[0]
		for _, v := range account[1:] {
			if v < minVal {
				minVal = v
			}
		}
		return minVal, account, true
	}

	if len(groupOrder) == 0 {
		return 0, nil, false
	}

	groupMins := make([]float64, 0, len(groupOrder))
	maxGroupMin := -1.0
	for _, key := range groupOrder {
		vals := grouped[key]
		if len(vals) == 0 {
			continue
		}
		gMin := vals[0]
		for _, v := range vals[1:] {
			if v < gMin {
				gMin = v
			}
		}
		groupMins = append(groupMins, gMin)
		if gMin > maxGroupMin {
			maxGroupMin = gMin
		}
	}

	if len(groupMins) == 0 {
		return 0, nil, false
	}
	return maxGroupMin, groupMins, true
}

// ComputeQuotaRoutingTarget computes the recommended routing priority and weight from a credential's quota state.
func ComputeQuotaRoutingTarget(quota *QuotaState, cfg Config) *QuotaRoutingTarget {
	if quota == nil || quota.Status != "ok" || len(quota.Rows) == 0 {
		return nil
	}

	bottleneck, values, ok := RoutingPercents(quota.Rows)
	if !ok || len(values) == 0 {
		return nil
	}

	var sum float64
	for _, v := range values {
		sum += v
	}

	bottleneckPercent := clampPercent(bottleneck)
	averagePercent := clampPercent(sum / float64(len(values)))

	if bottleneckPercent < cfg.CriticalThreshold {
		return &QuotaRoutingTarget{
			Priority:          cfg.PriorityCritical,
			Weight:            1,
			Tier:              "critical",
			BottleneckPercent: roundToOneDecimal(bottleneckPercent),
			AveragePercent:    roundToOneDecimal(averagePercent),
		}
	}

	if bottleneckPercent < cfg.HealthyThreshold {
		weight := int(math.Max(1, math.Round(bottleneckPercent)))
		return &QuotaRoutingTarget{
			Priority:          cfg.PriorityLow,
			Weight:            weight,
			Tier:              "low",
			BottleneckPercent: roundToOneDecimal(bottleneckPercent),
			AveragePercent:    roundToOneDecimal(averagePercent),
		}
	}

	effective := bottleneckPercent*0.7 + averagePercent*0.3
	weight := int(math.Max(1, math.Round(effective)))
	return &QuotaRoutingTarget{
		Priority:          cfg.PriorityHealthy,
		Weight:            weight,
		Tier:              "healthy",
		BottleneckPercent: roundToOneDecimal(bottleneckPercent),
		AveragePercent:    roundToOneDecimal(averagePercent),
	}
}

// OriginalsStore manages persisted original manual priority and weight values per credential.
type OriginalsStore struct {
	mu       sync.RWMutex
	filePath string
	entries  map[string]StoredOriginal
}

// NewOriginalsStore creates an OriginalsStore backed by an optional JSON file on disk.
func NewOriginalsStore(pluginDir string) *OriginalsStore {
	s := &OriginalsStore{
		entries: make(map[string]StoredOriginal),
	}
	if strings.TrimSpace(pluginDir) != "" {
		s.filePath = filepath.Join(pluginDir, "quota_auto_balance_originals.json")
		s.loadFromDisk()
	}
	return s
}

// SetPluginDir updates the backing directory for originals persistence.
func (s *OriginalsStore) SetPluginDir(pluginDir string) {
	if strings.TrimSpace(pluginDir) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	newPath := filepath.Join(pluginDir, "quota_auto_balance_originals.json")
	if s.filePath == newPath {
		return
	}
	s.filePath = newPath
	s.loadFromDiskLocked()
}

func (s *OriginalsStore) loadFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadFromDiskLocked()
}

func (s *OriginalsStore) loadFromDiskLocked() {
	if s.filePath == "" {
		return
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil || len(data) == 0 {
		return
	}
	var parsed map[string]StoredOriginal
	if err := json.Unmarshal(data, &parsed); err == nil && parsed != nil {
		for k, v := range parsed {
			s.entries[k] = v
		}
	}
}

func (s *OriginalsStore) saveToDiskLocked() {
	if s.filePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.filePath), 0o755)
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.filePath, data, 0o600)
}

// Get returns the stored original for a credential, if any.
func (s *OriginalsStore) Get(name string) (StoredOriginal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.entries[name]
	return v, ok
}

// All returns a shallow copy of all stored originals.
func (s *OriginalsStore) All() map[string]StoredOriginal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]StoredOriginal, len(s.entries))
	for k, v := range s.entries {
		out[k] = v
	}
	return out
}

// RecordBeforeApply records or updates the manual baseline before auto-balance writes a new target.
// If the credential's current priority or weight differs from what auto-balance last applied,
// we treat the current values as a newer manual user edit and update the stored baseline.
func (s *OriginalsStore) RecordBeforeApply(name string, currentPriority, currentWeight *int, target QuotaRoutingTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, hasExisting := s.entries[name]
	basePriority := cloneIntPtr(currentPriority)
	baseWeight := cloneIntPtr(currentWeight)

	if hasExisting {
		if existing.Applied != nil {
			// Check if current matches what auto-balance previously wrote.
			// If it still matches, preserve the original manual baseline; otherwise, the user edited it manually.
			prioUnchanged := currentPriority != nil && *currentPriority == existing.Applied.Priority
			weightUnchanged := currentWeight != nil && *currentWeight == existing.Applied.Weight
			if prioUnchanged && weightUnchanged {
				basePriority = cloneIntPtr(existing.Priority)
				baseWeight = cloneIntPtr(existing.Weight)
			}
		} else {
			basePriority = cloneIntPtr(existing.Priority)
			baseWeight = cloneIntPtr(existing.Weight)
		}
	}

	s.entries[name] = StoredOriginal{
		Priority: basePriority,
		Weight:   baseWeight,
		Applied: &AppliedRouting{
			Priority: target.Priority,
			Weight:   target.Weight,
		},
	}
	s.saveToDiskLocked()
}

// Delete removes a credential's stored original entry.
func (s *OriginalsStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, name)
	s.saveToDiskLocked()
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// ExtractRoutingFromRawJSON reads "priority" and "weight" from a raw credential JSON string.
func ExtractRoutingFromRawJSON(raw string) (priority *int, weight *int, doc map[string]any, err error) {
	doc = make(map[string]any)
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, nil, nil, err
	}
	if v, exists := doc["priority"]; exists && v != nil {
		if n, ok := toInt(v); ok {
			priority = &n
		}
	}
	if v, exists := doc["weight"]; exists && v != nil {
		if n, ok := toInt(v); ok {
			weight = &n
		}
	}
	return priority, weight, doc, nil
}

// ApplyRoutingToJSONMap updates or removes "priority" and "weight" in a credential JSON map and marshals it.
func ApplyRoutingToJSONMap(doc map[string]any, priority *int, weight *int) (string, error) {
	if priority == nil {
		delete(doc, "priority")
	} else {
		doc["priority"] = *priority
	}

	if weight == nil {
		delete(doc, "weight")
	} else {
		doc["weight"] = *weight
	}

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
