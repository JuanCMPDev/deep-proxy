# DeepProxy

**An OpenAI-compatible local proxy that lets any OpenAI client (`opencode`, `aider`, `continue.dev`, the OpenAI SDK, …) consume DeepSeek's web chat using a browser session — no official API key required.**

A single static binary, ~12 MB. No Docker, no Python, no Node.

```text
opencode/aider/...  ──►  http://localhost:3000  ──►  chat.deepseek.com
                          (deep-proxy)              (your web session)
```

---

## What it does

DeepSeek's [official API](https://api-docs.deepseek.com/) is paid and uses a separate account from the web chat. If you already use chat.deepseek.com (free or paid tier), DeepProxy reuses that session as if it were an OpenAI endpoint.

The hard parts — handled automatically:

- **OpenAI ⇄ DeepSeek-web translation.** Sync + SSE streaming. JSON-Patch operational stream from DeepSeek is reassembled into `chat.completion.chunk` / `chat.completion`.
- **SHA-3 proof-of-work.** DeepSeek's `DeepSeekHashV1` WASM module is embedded and run on every request via [wazero](https://github.com/tetratelabs/wazero) (~100 ms on amd64/arm64, no CGO).
- **Chat session creation** via `POST /api/v0/chat_session/create`, retried on PoW expiry (40301).
- **Credential capture from a real Chrome profile.** `deep-proxy login` opens Chrome, intercepts the first authenticated `/api/v0/` POST via the Chrome DevTools Fetch domain, and pulls out **token + cookie + `x-hif-leim`** in one shot. They're saved to `~/.config/deep-proxy/credentials.json` for the proxy to read.
- **Reports completion tokens** in the OpenAI `usage` field.

---

## Caveats

- Single-user, localhost-only tool. **Don't expose on `0.0.0.0`.**
- Cloudflare's `cf_clearance` cookie expires every ~30 minutes. Re-run `deep-proxy login` to refresh. (Background headless refresh is implemented but Cloudflare actively blocks headless Chrome — see the roadmap in [BLUEPRINT.md](BLUEPRINT.md).)
- DeepSeek's web API doesn't expose `prompt_tokens` — that field is always 0. `completion_tokens` and `total_tokens` are accurate.
- No tool/function calling and no image inputs (yet).
- Use at your own risk and respect DeepSeek's terms of service.

---

## Install

### Releases (once published)

**macOS / Linux:**
```bash
curl -fsSL https://raw.githubusercontent.com/JuanCMPDev/deep-proxy/main/scripts/install.sh | sh
```

**Windows:** download the `.zip` from the [releases page](https://github.com/JuanCMPDev/deep-proxy/releases), extract `deep-proxy.exe`, put it on `PATH`.

### From source

```bash
git clone https://github.com/JuanCMPDev/deep-proxy
cd deep-proxy
go build -o deep-proxy ./cmd/deep-proxy
```

Requires Go 1.23+ and Chrome installed (any recent version).

---

## Quick start

### 1. Capture credentials

```bash
deep-proxy login
```

This opens a Chrome window pointed at chat.deepseek.com. Two steps:

1. **Sign in** (handles Cloudflare's captcha automatically).
2. **Click "New chat"** — or send any message. This triggers an authenticated POST that carries the `x-hif-leim` signature, which DeepProxy intercepts.

The window closes by itself with:
```
✓ Logged in.
  token captured  : true
  cookie captured : true (NNN bytes)
  x-hif-leim      : true (NN bytes)
✓ Credentials cached at <path>/credentials.json (valid ~30 minutes).
```

### 2. Start the proxy

```bash
deep-proxy start
```

You should see:
```
INFO deep-proxy starting addr=127.0.0.1:3000 ...
INFO credentials cache loaded age=5s ...
INFO ready — listening for OpenAI-compatible requests addr=http://127.0.0.1:3000/v1/chat/completions
```

### 3. Point your OpenAI-compatible client at it

```bash
export OPENAI_API_KEY="any-string"          # the proxy ignores this
export OPENAI_BASE_URL="http://localhost:3000/v1"
```

For **opencode** (`~/.config/opencode/config.json`):
```jsonc
{
  "providers": {
    "deepseek": {
      "baseURL": "http://localhost:3000/v1",
      "apiKey":  "any-string"
    }
  }
}
```

For **aider**:
```bash
aider --openai-api-base http://localhost:3000/v1 --openai-api-key any-string
```

Test it:
```bash
curl -s -X POST http://localhost:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}]}'
```

### 4. Refresh every ~30 minutes

When DeepSeek starts returning errors (Cloudflare cookie expired):

```bash
# Stop the server (Ctrl+C)
deep-proxy login    # 5–10 seconds
deep-proxy start
```

---

## Configuration

All flags accept matching `DEEPPROXY_<UPPERCASE_FLAG>` environment variables.

| Flag | Default | Notes |
|---|---|---|
| `--token` | from credfile | DeepSeek session JWT. Auto-captured by `login`. |
| `--cookie` | from credfile | Full `Cookie` header. Auto-captured by `login`. |
| `--hif-leim` | from credfile | `x-hif-leim` header. Auto-captured by `login`. |
| `--auto-refresh` | `false` | Try background headless-Chrome refresh every 25 min. **Often blocked by Cloudflare** — falls back to credfile values automatically. |
| `--port` | `3000` | TCP port to listen on. |
| `--host` | `127.0.0.1` | Bind interface. **Don't use `0.0.0.0`.** |
| `--model` | `deepseek-chat` | Default model when the client omits one. |
| `--thinking` | `false` | Enable DeepSeek's reasoning trace (R1 mode). |
| `--search` | `false` | Enable DeepSeek's web search. |
| `--timeout` | `5m` | Per-request timeout. |
| `--log-level` | `info` | `debug` / `info` / `warn` / `error`. |
| `--log-format` | `text` | `text` for TTY, `json` for log ingestion. |

### Manual mode (no Chrome)

If you can't install Chrome on the host, copy the three values from your browser's DevTools (Network → any `/api/v0/` request → Request Headers) and set them as env vars instead of running `login`:

```bash
export DEEPPROXY_TOKEN='1xxZOYZ6...'
export DEEPPROXY_COOKIE='cf_clearance=...; ds_session_id=...; ...'
export DEEPPROXY_HIF_LEIM='nbozzU8sShY3...=.JS9L54j...'
deep-proxy start
```

### Model mapping

| OpenAI client requests | Sent to DeepSeek as |
|---|---|
| `deepseek-chat`, `deepseek-v3` | `default` |
| `deepseek-reasoner`, `deepseek-r1` | `expert` (R1 reasoning mode) |

### Commands

| Command | Purpose |
|---|---|
| `deep-proxy login` | Capture token + cookie + `x-hif-leim` from a real Chrome session into the credfile. |
| `deep-proxy start` | Run the proxy server, reading credentials from flag/env/credfile. |
| `deep-proxy version` | Print version + build metadata. |

---

## How it works

1. Client sends an OpenAI-shaped `POST /v1/chat/completions`.
2. DeepProxy:
   - Calls `POST /api/v0/chat_session/create` → fresh `chat_session_id`.
   - Calls `POST /api/v0/chat/create_pow_challenge` → challenge + difficulty + signature.
   - Runs `DeepSeekHashV1` (SHA-3) inside [wazero](https://github.com/tetratelabs/wazero) — same WASM binary the browser loads ([`internal/upstream/wasm/sha3_wasm_bg.wasm`](internal/upstream/wasm/sha3_wasm_bg.wasm)).
   - Sends `POST /api/v0/chat/completion` with the solved PoW header, current Cookie, current `x-hif-leim`, plus the static `x-client-*` and `Sec-*` headers a real browser sends.
   - Reads DeepSeek's SSE response (a JSON-Patch operational stream).
   - Forwards as either OpenAI SSE chunks (`stream:true`) or one accumulated `chat.completion` object (`stream:false`).
3. On `code 40301 INVALID_POW_RESPONSE` → fetches a fresh challenge and retries once.
4. `usage.completion_tokens` is populated from DeepSeek's `accumulated_token_usage` event.

**Performance:**
- **Compiler mode (amd64/arm64):** ~100 ms PoW + ~200 ms upstream = ~500 ms overhead before generation starts.
- **Interpreter mode (i386/etc):** PoW takes 3–8 s. Use a 64-bit binary in production.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `code 40300 MISSING_HEADER` | Cookie or `x-hif-leim` missing/wrong | `deep-proxy login` |
| `code 40301 INVALID_POW_RESPONSE` after retry | Cookie, hif-leim, or token mismatch | `deep-proxy login` |
| `code 40003` | Bearer token expired or invalidated by another login | `deep-proxy login` |
| HTML / Cloudflare challenge in response body | `cf_clearance` cookie expired | `deep-proxy login` |
| `auto-refresh disabled — falling back` | Headless Chrome blocked by Cloudflare (expected) | Ignore — credfile fallback handles it |
| Empty response content | DeepSeek returned no RESPONSE fragments (rare) | Run with `--log-level debug` and file an issue with the chunks |

---

## Development

```bash
make build              # build the binary
make test               # go test ./...
make lint               # golangci-lint
make snapshot           # local goreleaser snapshot (requires goreleaser)
go test -bench=BenchmarkSolve ./internal/upstream/   # PoW perf benchmark
```

Layout:
```
cmd/deep-proxy/        entrypoint
internal/
├── auth/              HeaderStore, Refresher, chromedp integration, credfile
├── cli/               cobra commands (start, login, version)
├── config/            typed config with defaults
├── observability/     slog setup
├── openai/            OpenAI wire types + SSE writer
├── proxy/             HTTP server + handlers + DeepSeek SSE → OpenAI translation
└── upstream/          DeepSeek client + PoW solver (WASM via wazero)
```

The release pipeline is `git tag vX.Y.Z && git push --tags`. GitHub Actions builds binaries for linux/macOS/windows × amd64/arm64, generates checksums, and signs them with sigstore cosign. See [`.goreleaser.yaml`](.goreleaser.yaml).

For pending work see [BLUEPRINT.md](BLUEPRINT.md).

---

## Acknowledgments

- **[xtekky/deepseek4free](https://github.com/xtekky/deepseek4free)** — first public reverse-engineering of DeepSeek's PoW. The bundled `sha3_wasm_bg.wasm` was extracted from chat.deepseek.com's web bundle by that project; we redistribute the same file unmodified.
- **[tetratelabs/wazero](https://github.com/tetratelabs/wazero)** — pure-Go WebAssembly runtime that runs DeepSeek's solver without CGO.
- **[chromedp/chromedp](https://github.com/chromedp/chromedp)** — Chrome DevTools Protocol bindings used by the credential capture flow.

## License

MIT — see [LICENSE](LICENSE).

The embedded `sha3_wasm_bg.wasm` is owned by DeepSeek and redistributed as-is for interoperability.
