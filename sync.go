package main

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// HostClient defines the host callback interface used by BalanceEngine.
type HostClient struct {
	AuthList func() ([]HostAuthFileEntry, error)
	AuthGet  func(name string) (*HostAuthGetResponse, error)
	AuthSave func(req HostAuthSaveRequest) (*HostAuthSaveResponse, error)
	HTTPDo   func(req HostHTTPRequest) (*HostHTTPResponse, error)
}

// SyncSummary reports the outcome of a synchronization or restore operation.
type SyncSummary struct {
	Checked  int      `json:"checked"`
	Updated  int      `json:"updated"`
	Restored int      `json:"restored"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
	SyncedAt int64    `json:"syncedAt"`
}

// StatusResponse is returned by GET /v0/management/plugins/quota-auto-balance/status.
type StatusResponse struct {
	Config         Config                    `json:"config"`
	LastSyncAt     int64                     `json:"lastSyncAt,omitempty"`
	LastError      string                    `json:"lastError,omitempty"`
	SyncInProgress bool                      `json:"syncInProgress"`
	Credentials    []CredentialBalanceStatus `json:"credentials"`
}

// BalanceEngine coordinates quota inspection, priority/weight calculation, original preservation, and background sync.
type BalanceEngine struct {
	mu             sync.RWMutex
	opMu           sync.Mutex
	cfg            Config
	pluginDir      string
	originals      *OriginalsStore
	quotaCache     map[string]*QuotaState
	lastSyncAt     int64
	lastError      string
	syncInProgress bool
	host           HostClient

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewBalanceEngine creates a new BalanceEngine with default configuration.
func NewBalanceEngine(host HostClient) *BalanceEngine {
	return &BalanceEngine{
		cfg:        DefaultConfig(),
		originals:  NewOriginalsStore(""),
		quotaCache: make(map[string]*QuotaState),
		host:       host,
	}
}

// Configure applies a new configuration and restarts the background worker if needed.
func (e *BalanceEngine) Configure(pluginDir string, newCfg Config, isReconfigure bool) {
	e.mu.Lock()
	prevEnabled := e.cfg.AutoBalanceEnabled
	if pluginDir != "" {
		e.pluginDir = pluginDir
		e.originals.SetPluginDir(pluginDir)
	}
	e.cfg = newCfg
	e.mu.Unlock()

	if isReconfigure && prevEnabled && !newCfg.AutoBalanceEnabled && newCfg.RestoreOnDisable {
		go func() {
			_, _ = e.RestoreOriginals()
		}()
	}

	e.restartWorker()
}

// GetConfig returns the active configuration.
func (e *BalanceEngine) GetConfig() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// Shutdown stops any running background sync loop.
func (e *BalanceEngine) Shutdown() {
	e.mu.Lock()
	if e.stopCh != nil {
		close(e.stopCh)
		e.stopCh = nil
	}
	e.mu.Unlock()
	e.wg.Wait()
}

func (e *BalanceEngine) restartWorker() {
	e.Shutdown()

	e.mu.Lock()
	cfg := e.cfg
	if !cfg.AutoBalanceEnabled || cfg.SyncIntervalSeconds <= 0 {
		e.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	e.stopCh = stopCh
	interval := time.Duration(cfg.SyncIntervalSeconds) * time.Second
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				if e.GetConfig().AutoBalanceEnabled {
					_, _ = e.SyncAll(nil)
				}
			}
		}
	}()
}

// SyncAll inspects credentials (using client-provided quotas if given, otherwise fetching via host.http.do)
// and updates priority and weight when AutoBalanceEnabled is true.
func (e *BalanceEngine) SyncAll(clientQuotas map[string]*QuotaState) (*SyncSummary, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	e.mu.Lock()
	e.syncInProgress = true
	cfg := e.cfg
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.syncInProgress = false
		e.mu.Unlock()
	}()

	now := time.Now().UnixMilli()
	summary := &SyncSummary{SyncedAt: now}

	if e.host.AuthList == nil || e.host.AuthGet == nil || e.host.AuthSave == nil {
		err := fmt.Errorf("host auth callbacks are not initialized")
		e.setLastError(err.Error())
		return summary, err
	}

	files, err := e.host.AuthList()
	if err != nil {
		e.setLastError(err.Error())
		return summary, err
	}

	for _, entry := range files {
		if entry.Disabled || entry.Name == "" {
			summary.Skipped++
			continue
		}
		summary.Checked++

		authDetail, err := e.host.AuthGet(entry.Name)
		if err != nil || authDetail == nil || authDetail.Raw == "" {
			if err != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", entry.Name, err))
			} else {
				summary.Skipped++
			}
			continue
		}

		currentPrio, currentWeight, doc, err := ExtractRoutingFromRawJSON(authDetail.Raw)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: invalid JSON: %v", entry.Name, err))
			continue
		}

		var quota *QuotaState
		if clientQuotas != nil {
			if cq, ok := clientQuotas[entry.Name]; ok && cq != nil {
				quota = cq
				if quota.UpdatedAt == 0 {
					quota.UpdatedAt = now
				}
			}
		}
		if quota == nil && e.host.HTTPDo != nil {
			quota = FetchCredentialQuota(entry, doc, e.host.HTTPDo)
		}

		if quota != nil {
			e.mu.Lock()
			e.quotaCache[entry.Name] = quota
			e.mu.Unlock()
		}

		if !cfg.AutoBalanceEnabled {
			summary.Skipped++
			continue
		}

		target := ComputeQuotaRoutingTarget(quota, cfg)
		if target == nil {
			summary.Skipped++
			continue
		}

		// Record manual baseline before applying (or detect if user manually changed values since last apply)
		e.originals.RecordBeforeApply(entry.Name, currentPrio, currentWeight, *target)

		if currentPrio != nil && *currentPrio == target.Priority &&
			currentWeight != nil && *currentWeight == target.Weight {
			summary.Skipped++
			continue
		}

		newPrio := target.Priority
		newWeight := target.Weight
		updatedRaw, err := ApplyRoutingToJSONMap(doc, &newPrio, &newWeight)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: marshal error: %v", entry.Name, err))
			continue
		}

		_, err = e.host.AuthSave(HostAuthSaveRequest{
			Name:     entry.Name,
			Raw:      updatedRaw,
			Metadata: authDetail.Metadata,
		})
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: save error: %v", entry.Name, err))
			continue
		}

		summary.Updated++
	}

	e.mu.Lock()
	e.lastSyncAt = now
	if len(summary.Errors) > 0 {
		e.lastError = summary.Errors[0]
	} else {
		e.lastError = ""
	}
	e.mu.Unlock()

	return summary, nil
}

// RestoreOriginals restores all credentials tracked in OriginalsStore back to their original manual priority and weight.
// If a credential's current priority or weight differs from what auto-balance last applied, the user's newer manual edit is preserved.
func (e *BalanceEngine) RestoreOriginals() (*SyncSummary, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()

	now := time.Now().UnixMilli()
	summary := &SyncSummary{SyncedAt: now}

	if e.host.AuthGet == nil || e.host.AuthSave == nil {
		err := fmt.Errorf("host auth callbacks are not initialized")
		return summary, err
	}

	storedMap := e.originals.All()
	names := make([]string, 0, len(storedMap))
	for name := range storedMap {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		stored := storedMap[name]
		summary.Checked++

		authDetail, err := e.host.AuthGet(name)
		if err != nil || authDetail == nil || authDetail.Raw == "" {
			e.originals.Delete(name)
			summary.Skipped++
			continue
		}

		currentPrio, currentWeight, doc, err := ExtractRoutingFromRawJSON(authDetail.Raw)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: invalid JSON: %v", name, err))
			continue
		}

		// Do not overwrite newer manual edits if the file no longer matches what auto-balance last wrote.
		if stored.Applied != nil {
			prioMatches := currentPrio != nil && *currentPrio == stored.Applied.Priority
			weightMatches := currentWeight != nil && *currentWeight == stored.Applied.Weight
			if !prioMatches || !weightMatches {
				e.originals.Delete(name)
				summary.Skipped++
				continue
			}
		}

		if intPtrEqual(currentPrio, stored.Priority) && intPtrEqual(currentWeight, stored.Weight) {
			e.originals.Delete(name)
			summary.Skipped++
			continue
		}

		updatedRaw, err := ApplyRoutingToJSONMap(doc, stored.Priority, stored.Weight)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: marshal error: %v", name, err))
			continue
		}

		_, err = e.host.AuthSave(HostAuthSaveRequest{
			Name:     name,
			Raw:      updatedRaw,
			Metadata: authDetail.Metadata,
		})
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: restore error: %v", name, err))
			continue
		}

		e.originals.Delete(name)
		summary.Restored++
	}

	return summary, nil
}

// GetStatus builds the complete StatusResponse for the management API and dashboard UI.
func (e *BalanceEngine) GetStatus() (*StatusResponse, error) {
	e.mu.RLock()
	cfg := e.cfg
	lastSyncAt := e.lastSyncAt
	lastError := e.lastError
	syncInProgress := e.syncInProgress
	quotaSnapshot := make(map[string]*QuotaState, len(e.quotaCache))
	for k, v := range e.quotaCache {
		quotaSnapshot[k] = v
	}
	e.mu.RUnlock()

	resp := &StatusResponse{
		Config:         cfg,
		LastSyncAt:     lastSyncAt,
		LastError:      lastError,
		SyncInProgress: syncInProgress,
		Credentials:    []CredentialBalanceStatus{},
	}

	if e.host.AuthList == nil {
		return resp, nil
	}

	files, err := e.host.AuthList()
	if err != nil {
		return resp, err
	}

	originalsMap := e.originals.All()

	for _, entry := range files {
		if entry.Name == "" {
			continue
		}
		currentPrio := entry.Priority
		currentWeight := entry.Weight
		var doc map[string]any

		if e.host.AuthGet != nil {
			if detail, err := e.host.AuthGet(entry.Name); err == nil && detail != nil && detail.Raw != "" {
				p, w, d, err := ExtractRoutingFromRawJSON(detail.Raw)
				if err == nil {
					currentPrio = p
					currentWeight = w
					doc = d
				}
			}
		}

		provider := NormalizeProvider(entry, doc)
		quota := quotaSnapshot[entry.Name]
		target := ComputeQuotaRoutingTarget(quota, cfg)
		orig, hasOrig := originalsMap[entry.Name]

		statusItem := CredentialBalanceStatus{
			Name:            entry.Name,
			Provider:        provider,
			Label:           entry.Label,
			Disabled:        entry.Disabled,
			CurrentPriority: currentPrio,
			CurrentWeight:   currentWeight,
			HasOriginal:     hasOrig,
			Quota:           quota,
			Target:          target,
		}
		if hasOrig {
			statusItem.OriginalPriority = orig.Priority
			statusItem.OriginalWeight = orig.Weight
			statusItem.Modified = !intPtrEqual(currentPrio, orig.Priority) || !intPtrEqual(currentWeight, orig.Weight)
		}

		resp.Credentials = append(resp.Credentials, statusItem)
	}

	return resp, nil
}

func (e *BalanceEngine) setLastError(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = msg
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
