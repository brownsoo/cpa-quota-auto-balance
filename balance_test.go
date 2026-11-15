package main

import (
	"encoding/json"
	"testing"
)

func TestComputeQuotaRoutingTarget_Tiers(t *testing.T) {
	cfg := DefaultConfig()

	// Healthy tier: 5h=80%, 7d=60% -> bottleneck=60, avg=70 -> effective = 60*0.7 + 70*0.3 = 63
	healthy := ComputeQuotaRoutingTarget(&QuotaState{
		Status: "ok",
		Rows: []QuotaRow{
			{Label: "5h", Percent: 80},
			{Label: "7d", Percent: 60},
		},
	}, cfg)
	if healthy == nil {
		t.Fatal("expected non-nil target for healthy quota")
	}
	if healthy.Priority != 0 || healthy.Weight != 63 || healthy.Tier != "healthy" {
		t.Fatalf("unexpected healthy target: %+v", healthy)
	}

	// Low tier: 5h=12%, 7d=50% -> bottleneck=12 (<15, >=5) -> priority=-5, weight=12
	low := ComputeQuotaRoutingTarget(&QuotaState{
		Status: "ok",
		Rows: []QuotaRow{
			{Label: "5h", Percent: 12},
			{Label: "7d", Percent: 50},
		},
	}, cfg)
	if low == nil {
		t.Fatal("expected non-nil target for low quota")
	}
	if low.Priority != -5 || low.Weight != 12 || low.Tier != "low" {
		t.Fatalf("unexpected low target: %+v", low)
	}

	// Critical tier: 5h=3%, 7d=40% -> bottleneck=3 (<5) -> priority=-10, weight=1
	critical := ComputeQuotaRoutingTarget(&QuotaState{
		Status: "ok",
		Rows: []QuotaRow{
			{Label: "5h", Percent: 3},
			{Label: "7d", Percent: 40},
		},
	}, cfg)
	if critical == nil {
		t.Fatal("expected non-nil target for critical quota")
	}
	if critical.Priority != -10 || critical.Weight != 1 || critical.Tier != "critical" {
		t.Fatalf("unexpected critical target: %+v", critical)
	}
}

func TestRoutingPercents_AccountVsScopedGroups(t *testing.T) {
	cfg := DefaultConfig()

	// Account-wide windows ignore scoped Code Review exhaustion
	target := ComputeQuotaRoutingTarget(&QuotaState{
		Status: "ok",
		Rows: []QuotaRow{
			{Label: "5h", Percent: 70, Scoped: false},
			{Label: "7d", Percent: 50, Scoped: false},
			{Label: "Code Review 5h", Percent: 0, Scoped: true, GroupLabel: "Code Review"},
		},
	}, cfg)
	if target == nil || target.BottleneckPercent != 50 || target.Priority != 0 {
		t.Fatalf("expected account-wide bottleneck 50, got %+v", target)
	}

	// Scoped-only (Antigravity): min within each group, max across groups
	agTarget := ComputeQuotaRoutingTarget(&QuotaState{
		Status: "ok",
		Rows: []QuotaRow{
			{Label: "claude-sonnet-4", Percent: 0, Scoped: true, GroupLabel: "Claude / GPT"},
			{Label: "gemini-2.5-pro", Percent: 80, Scoped: true, GroupLabel: "Gemini Pro"},
			{Label: "gemini-2.5-pro-preview", Percent: 60, Scoped: true, GroupLabel: "Gemini Pro"},
			{Label: "gemini-2.5-flash", Percent: 100, Scoped: true, GroupLabel: "Gemini Flash"},
		},
	}, cfg)
	if agTarget == nil {
		t.Fatal("expected non-nil target for Antigravity groups")
	}
	// Group mins: Claude/GPT=0, Gemini Pro=60, Gemini Flash=100 -> bottleneck = max(0,60,100) = 100
	if agTarget.BottleneckPercent != 100 || agTarget.Priority != 0 {
		t.Fatalf("unexpected Antigravity target: %+v", agTarget)
	}
}

func TestSyncAndRestore_PreservesManualEdits(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"codex-a.json": `{"type":"codex","priority":3,"weight":10}`,
		"codex-b.json": `{"type":"codex"}`,
	}

	host := HostClient{
		AuthList: func() ([]HostAuthFileEntry, error) {
			return []HostAuthFileEntry{
				{Name: "codex-a.json", Provider: "codex"},
				{Name: "codex-b.json", Provider: "codex"},
			}, nil
		},
		AuthGet: func(name string) (*HostAuthGetResponse, error) {
			return &HostAuthGetResponse{Name: name, Raw: files[name]}, nil
		},
		AuthSave: func(req HostAuthSaveRequest) (*HostAuthSaveResponse, error) {
			files[req.Name] = req.Raw
			return &HostAuthSaveResponse{Saved: true, Name: req.Name}, nil
		},
	}

	engine := NewBalanceEngine(host)
	engine.Configure(tmpDir, DefaultConfig(), false)

	quotas := map[string]*QuotaState{
		"codex-a.json": {
			Status: "ok",
			Rows:   []QuotaRow{{Label: "5h", Percent: 80}, {Label: "7d", Percent: 80}},
		},
		"codex-b.json": {
			Status: "ok",
			Rows:   []QuotaRow{{Label: "5h", Percent: 2}, {Label: "7d", Percent: 40}},
		},
	}

	summary, err := engine.SyncAll(quotas)
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}
	if summary.Updated != 2 {
		t.Fatalf("expected 2 updated files, got %+v", summary)
	}

	// Simulate user manually editing codex-b.json priority to 99 while auto-balance is active
	files["codex-b.json"] = `{"type":"codex","priority":99,"weight":50}`

	// Now restore originals: codex-a.json should restore to (3, 10), while codex-b.json keeps (99, 50)
	restoreSummary, err := engine.RestoreOriginals()
	if err != nil {
		t.Fatalf("RestoreOriginals failed: %v", err)
	}
	if restoreSummary.Restored != 1 {
		t.Fatalf("expected 1 restored file, got %+v", restoreSummary)
	}

	var docA map[string]any
	_ = json.Unmarshal([]byte(files["codex-a.json"]), &docA)
	if int(docA["priority"].(float64)) != 3 || int(docA["weight"].(float64)) != 10 {
		t.Fatalf("expected codex-a.json restored to 3/10, got %v", docA)
	}

	var docB map[string]any
	_ = json.Unmarshal([]byte(files["codex-b.json"]), &docB)
	if int(docB["priority"].(float64)) != 99 || int(docB["weight"].(float64)) != 50 {
		t.Fatalf("expected codex-b.json manual edit 99/50 preserved, got %v", docB)
	}
}
