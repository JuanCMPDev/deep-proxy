# DeepProxy — Roadmap

Status: **v0.1 functional**. Sync + streaming work end-to-end through a real DeepSeek session. Token, Cookie, and `x-hif-leim` are captured automatically by `deep-proxy login` (visible Chrome) and persisted to a credentials file that `deep-proxy start` reads on launch. PoW is auto-solved by an embedded SHA-3 WASM module on every request.

This document tracks **what is left**, not what was built. For an overview of the working system see [`README.md`](README.md); for the source layout see the package summary in the README's "Development" section.

---

## Known limitations of v0.1

| Limitation | Impact | Owner |
|---|---|---|
| `deep-proxy login` must be re-run every ~30 minutes when `cf_clearance` expires | Manual refresh interrupts long sessions | `auth/refresher` (C8/C9 below) |
| `--auto-refresh` headless mode is blocked by Cloudflare | Cannot refresh credentials in the background unattended | C8/C9 |
| `prompt_tokens` always reports 0 | DeepSeek's web API doesn't expose input tokens; only completion is tracked | Possibly unfixable |
| No tool/function calling | The OpenAI tools field is ignored | Investigate DeepSeek upstream support |
| No image inputs | Multimodal requests are not translated | Investigate |
| Each request creates a new chat session on DeepSeek | Slightly wasteful, prevents server-side prompt caching | B5 below |

---

## Roadmap

### Sprint S1 — Auto-refresh that actually works (C8/C9 v2)

**Goal:** eliminate the manual `deep-proxy login` every 30 min.

The current headless approach is detected by Cloudflare. Two paths to evaluate, in order:

1. **Off-screen visible Chrome** — same `--user-data-dir` profile, but launched with `--window-position=-2400,-2400` and `--window-size=1,1`. Real Chrome (passes Cloudflare) but invisible. Run for ~10 s every 25 min, capture creds, kill.
2. **CDP-attach to user's main Chrome** — user starts their normal Chrome with `--remote-debugging-port=9222`; the refresher attaches via DevTools instead of spawning a separate browser. Most reliable but requires user setup.

Implementation:
- New flag `--refresh-mode={offscreen|attach|none}`, default `offscreen`
- `auth/chrome.go`: alternative `OffscreenRefreshFunc(profileDir)` and `AttachRefreshFunc(debugPort)`
- Existing `ChromeRefreshFunc` deprecated (kept for fallback)
- Test that off-screen mode survives Cloudflare for 24 h continuously

**Estimate:** 1 day.

---

### Sprint S2 — Hot-reload of credfile

**Goal:** when the user re-runs `deep-proxy login` while the server is running, the server picks up new creds without restart.

Current behaviour: `start` reads credfile once on boot. New creds in the file are ignored until restart.

Implementation:
- `auth/credfile.go`: add `Watch(path string, onChange func(*Credentials))` using `fsnotify`
- `cli/start.go`: spawn a watcher goroutine that calls `store.Set(...)` on each file change
- Debounce 500 ms (file writes are non-atomic; we don't want to trigger on a half-written file)

**Estimate:** 0.5 day.

---

### Sprint S3 — Multi-turn session reuse

**Goal:** stop creating a fresh `chat_session_id` on every request.

Current behaviour: every OpenAI request triggers `POST /chat_session/create`. ~200 ms wasted per request, plus DeepSeek can't apply prompt caching.

Implementation:
- `upstream/Client`: cache `chatSessionID` after first creation, reuse for subsequent requests
- Track `parent_message_id` from each completion response, send it on next request
- Invalidate the cached session when the OpenAI client sends a request that doesn't extend the previous conversation (different system prompt, fewer messages, etc.)
- Fallback: TTL-based invalidation (sessions expire after 3 days per DeepSeek's response)

**Estimate:** 1 day.

---

### Sprint S4 — Auto-retry on token rotation (40003)

**Goal:** when DeepSeek invalidates the bearer token (e.g. user logged in elsewhere), automatically re-run `login` and continue serving.

Current behaviour: requests fail with `code=40003` and the user has to manually rerun `login`.

Implementation:
- Detect `code=40003` in `sendChat` (already detected, currently surfaces error)
- Trigger an out-of-band `auth.VisibleLogin` (visible Chrome window pops up, user re-authenticates)
- Update store, retry the original chat request once
- Bound by max 1 retry to avoid loops

This requires a UX decision: is popping up Chrome from the running server acceptable? Probably yes if the alternative is a hard failure.

**Estimate:** 0.5 day.

---

### Sprint S5 — Performance pass on amd64

**Goal:** verify and benchmark the production hot path.

Current state: development happens on a `windows/386` install where wazero falls back to interpreter mode and PoW takes 3–8 s. On amd64/arm64, compiler mode should bring this to ~100 ms but it has not been measured against a real DeepSeek session.

Implementation:
- Cross-build for `windows/amd64`, `linux/amd64`, `darwin/arm64`
- Run the existing `BenchmarkSolve` benchmark on each
- End-to-end latency test: 10 sequential `chat.completions` requests, measure p50/p95
- Document expected per-platform overhead in the README

**Estimate:** 0.25 day.

---

### Sprint S6 — Function calling and image inputs (stretch)

**Goal:** support OpenAI `tools` field and image content blocks.

Requires investigation: does the DeepSeek web API expose either feature? Probably not officially, but the chat UI does support file uploads and code interpretation. Reverse engineer the relevant endpoints.

If DeepSeek doesn't support tools natively, we could implement a fake by:
- Detecting `tools` in the OpenAI request
- Injecting a system prompt that instructs the model to emit JSON tool calls
- Parsing the model's response for tool-call JSON and re-emitting in OpenAI format

Most useful clients (aider, opencode) already work without tools.

**Estimate:** 2–3 days, lower priority.

---

### Sprint S7 — Distribution polish

**Goal:** make first-time setup smooth.

- Homebrew tap repo (`JuanCMPDev/homebrew-tap`) — uncomment the `brews` block in `.goreleaser.yaml` after creating the repo
- Scoop bucket repo (`JuanCMPDev/scoop-bucket`) — same for the `scoops` block
- `deep-proxy doctor` command: prints platform, Chrome version detected, PoW solve time, credfile status — helps users self-diagnose
- Demo GIF in README showing `aider` connecting in real time

**Estimate:** 1 day.

---

## Order of attack (recommended)

```
S1 ──► S2 ──► S4 ──► S5 ──► S3 ──► S7
       │
       └► S6 (stretch, parallel-able)
```

S1 (auto-refresh) is the biggest UX win and unblocks unattended use. S2 (hot-reload) is a small follow-up that makes the manual fallback painless. S4 (token rotation) handles the next-most-frequent failure. S5 (perf) verifies production assumptions before announcing the project. S3 (multi-turn) is an optimisation that's only worth it after the basics are bulletproof. S7 closes the distribution loop. S6 is nice-to-have if there's appetite.

Total estimate to v1.0: **~5 days of focused work**.
