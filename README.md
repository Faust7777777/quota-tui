# AI Quota Monitor

A local terminal monitor for Claude Code and Codex CLI quota windows.

It shows:

- Claude Code 5-hour limit usage, reset countdown, and estimated USD cost.
- Claude Code weekly limit usage, reset countdown, and estimated USD cost.
- Codex CLI 5-hour limit usage, reset countdown, and estimated USD cost.
- Codex CLI weekly limit usage, reset countdown, and estimated USD cost.

The cost estimates are calculated from local CLI JSONL logs. The quota percentages
and reset times come from the user's existing local Claude Code / Codex CLI login
state.

## Requirements

- Go 1.24.2 or newer.
- Claude Code logged in on the same machine, for Claude quota:
  - `~/.claude/.credentials.json`
  - `~/.claude/projects/**/*.jsonl`
- Codex CLI logged in on the same machine, for Codex quota:
  - `~/.codex/auth.json`
  - `~/.codex/sessions/**/*.jsonl`

## Run

```powershell
go run .
```

Build a Windows executable:

```powershell
go build -o quota-tui.exe .
.\quota-tui.exe
```

## Proxy

By default, the app uses the normal system environment proxy settings and does not
force a local proxy.

If your network requires a proxy, set `QUOTA_TUI_PROXY`:

```powershell
$env:QUOTA_TUI_PROXY = "http://127.0.0.1:7890"
go run .
```

## Keys

- `r`: refresh now
- `q` or `Ctrl+C`: quit

## Privacy

This app is intended to run locally. It reads local Claude Code and Codex CLI
credential files only to request quota window metadata, and reads local JSONL logs
to estimate cost. Do not commit or share your `~/.claude` or `~/.codex` files.

## Notes

This is a personal quota monitor, not an API proxy, task runner, or multi-account
router. It does not send prompts through a third-party service.
