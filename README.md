# CPA Quota Auto-Balance Plugin (`quota-auto-balance`)

CLIProxyAPI / EasyCLIProxyAPI native C-ABI plugin that automatically synchronizes credential `priority` and `weight` based on remaining quota windows (`5h`, `7d`, and per-model groups), while preserving and restoring manual user settings.

## Key Features

- **3-Tier Bottleneck Priority & Weighted Routing**:
  - **Healthy (`>= 15%`)**: `priority = 0` (matches CPA default), `weight = round(bottleneck * 0.7 + average * 0.3)`
  - **Low (`5% <= bottleneck < 15%`)**: `priority = -5`, `weight = max(1, round(bottleneck))`
  - **Critical (`< 5%`)**: `priority = -10`, `weight = 1`
- **Account-Wide vs. Scoped/Grouped Quota Windows**:
  - Account-wide windows (e.g., Codex/Claude `5h` + `7d`) use `min(windows)` as the bottleneck.
  - Scoped/grouped windows (e.g., Antigravity model groups `Claude / GPT`, `Gemini Pro`, `Gemini Flash`) compute `min` within each pool and `max` across pools so exhaustion in one model group does not penalize other available groups.
- **Manual Baseline Preservation & Safe Restoration**:
  - Records original manual `priority` and `weight` in `quota_auto_balance_originals.json` before applying changes.
  - Detects if a user manually edits `priority` or `weight` while Auto-Balance is active and preserves the manual edit on restore.
- **Hybrid Operation (Background Sync + Web UI Dashboard)**:
  - Runs background quota synchronization at a configurable interval (`sync_interval_seconds`).
  - Exposes a Web UI Dashboard at `/v0/resource/plugins/quota-auto-balance/dashboard` (**Plugins → Quota Auto-Balance** menu in EasyCLIProxyAPI).
  - Exposes Management API endpoints under `/v0/management/plugins/quota-auto-balance/{status,sync,restore,config}`.

## Building

Requires Go 1.22+ with CGO enabled (`CGO_ENABLED=1`):

```bash
make test
make build
```

This produces `dist/quota-auto-balance.<dylib|so|dll>`, which can be installed directly via **EasyCLIProxyAPI → Plugins → Install Local Plugin** or placed in the CLIProxyAPI `plugins/` directory.
