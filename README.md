# aiquokka

One command to see the usage limits of all your AI coding subscriptions —
Claude, Codex, Kimi, Copilot, Grok, Kiro, and Antigravity — reading the credentials each official CLI
already stores. No tokens to paste, no config.

![aiquokka demo](docs/demo.gif)

- **All in one place** — run `aiquokka` bare to see every provider at once.
- **Only what you use** — providers you aren't logged into are skipped silently.
- **Pace marker** — each bar shows where even, linear usage would put you right
  now, so you can tell at a glance if you're burning too fast.
- **Auto token refresh** — expired OAuth tokens are refreshed and written back.
- **Scriptable** — `--json` / `--yaml` for machine-readable output.
- **Live watch** — `--watch` / `-w` refreshes the view every 60 seconds.

## Install

```sh
go install github.com/McKean/aiquokka@latest
```

Requires Go (macOS: `brew install go`, Linux: use your package manager or https://go.dev/dl/).

Or build from source:

```sh
go build -o aiquokka .
```

## Usage

```sh
aiquokka           # all configured providers at once
aiquokka claude    # 5-hour, weekly, and weekly Fable limits
aiquokka codex     # weekly limit + remaining resets
aiquokka kimi      # 5-hour and weekly limits
aiquokka grok      # weekly usage limit + subscription tier
aiquokka copilot   # copilot chat/completions limits
aiquokka kiro      # Kiro CLI monthly credits and overage status
aiquokka agy       # daily antigravity limits

aiquokka --watch           # refresh all providers every 60s
aiquokka claude -w         # watch a single provider
```

```
$ aiquokka claude
Claude  (max/default_claude_max_5x)
───────────────────────────────────
  5h           [██████░░░░░░░░░░░░░░░▒░░]  24.0%   resets in 33m (Mon 19:00)
  Weekly       [███████████░░░▒░░░░░░░░░]  45.0%   resets in 2d20h (Thu 14:27)
  Weekly Fable [██████████████▓█░░░░░░░░]  67.0%   resets in 2d20h (Thu 14:27)
```

Fable draws from the same weekly pool and may take up to half of it, so it is
often the limit you hit first. The two bars move together: `Weekly Fable` at
100% puts `Weekly` at 50% or more.

Run with no subcommand to fetch every provider concurrently. In a terminal, a
fixed-order skeleton appears for the providers you use, then each section fills
in as its response arrives — you keep a stable order without waiting for the
slowest provider before seeing anything. Providers you aren't logged into are
skipped; a provider that *is* configured but errors is shown inline without
aborting the rest. Calling a provider directly (e.g. `aiquokka kimi`) always
tells you if it isn't set up.

### Watch mode

Pass `--watch` / `-w` on the root command or any provider subcommand to refresh
the view every 60 seconds until you hit Ctrl+C (or `q`). In a terminal a
pulsating status line shows the countdown to the next refresh; press **`r`** to
refresh immediately, or **`q`** (or Ctrl+C) to close. The previous frame is
cleared before each redraw. With `--json` / `--yaml` each tick emits a new
document (no status line).

### Machine-readable output

Add `--json` or `--yaml` (alias `--yml`) to any command. The aggregate view
keys the object by provider.

```
$ aiquokka grok --yml
provider: Grok
plan: XPremium
windows:
  - label: Weekly
    used_percent: 0
    resets_at: 2026-07-25T07:05:43.476608Z
extra:
  - label: Grok Code
    value: "yes"
```

## The pace marker

Every bar carries a bright-cyan marker cell at the point where **even, linear
usage** would put you at the current moment — the elapsed fraction of the
window. If the filled bar falls short of the marker you're under pace (headroom
to spare); if it's past the marker you're consuming faster than the window
refills.

```
  Weekly   [███████████▓███████████░]  94.0%   ← marker buried inside: way over pace
  5h       [████░░░░░░░░░░░░░░░░▒░░░]  16.0%   ← well behind the marker: plenty left
```

## How it works

aiquokka reuses the credentials each official CLI already stores on your
machine and queries the same usage endpoint that CLI uses.

| Command | Credentials | Endpoint |
| --- | --- | --- |
| `claude` | macOS Keychain, or `~/.claude/.credentials.json` (OAuth) | `api.anthropic.com/api/oauth/usage` |
| `codex`  | `~/.codex/auth.json` (ChatGPT OAuth) | `chatgpt.com/backend-api/wham/usage` |
| `kimi`   | `~/.kimi-code` / `~/.kimi` OAuth, or `$KIMI_API_KEY` | `api.kimi.com/coding/v1/usages` |
| `grok`   | `~/.grok/auth.json` (xAI OIDC) | `cli-chat-proxy.grok.com/v1/billing?format=credits` |
| `copilot`| `~/.config/github-copilot/{apps,hosts}.json` | `api.github.com/copilot_internal/user` |
| `kiro`   | Kiro CLI credential store (via `kiro-cli /usage`) | `q.<region>.amazonaws.com/getUsageLimits` |
| `agy`    | `~/.gemini/antigravity-cli/antigravity-oauth-token` | `daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota` |

Every provider that uses a short-lived OAuth access token (all except a static
Kimi key) **refreshes automatically** when the token has expired and writes the
new token back to the credential file.

### Claude Code on macOS

Claude Code stores its OAuth credential in the macOS Keychain. Aiquokka reads
the `Claude Code-credentials` item and, when a refresh is required, updates
that same item while preserving fields it does not own. If multiple matching
items exist, it uses the first one. This storage format is an implementation
detail of Claude Code and may change without notice.

Per-provider notes:

- **Codex** reports a weekly window plus your remaining *reset credits* ("amount
  of resets").
- **Kimi** limits are only on the Kimi Code coding subscription; the OAuth token
  is auto-detected from the CLI, or set `KIMI_API_KEY` to an `sk-kimi-…` key.
- **Grok** reports the weekly usage-limit window (the same figure the Grok CLI's
  `/usage` shows), plus subscription tier and Grok Code access. xAI rotates
  refresh tokens, so if both `grok` and `aiquokka` refresh the most recent one
  wins; a "run grok to re-login" message means the stored token was superseded.
- **Copilot** fetches usage across Chat, Completions, and Premium Interactions based on the IDE or GitHub CLI stored credential.
- **Kiro** runs the installed CLI’s built-in `/usage` command non-interactively, so Kiro retains ownership of credentials and token refresh. It reports monthly credits, plan, reset date, and overage status.
- **Antigravity** reads the OAuth token stored in `~/.gemini/antigravity-cli/antigravity-oauth-token` and impersonates the CLI's first-party OAuth client to access the restricted quota endpoint.

## Caveats

These are all **undocumented** endpoints used by the respective official CLIs;
they may change without notice. Please don't poll them aggressively — Claude's
endpoint in particular rate-limits hard. `--watch` refreshes every 60 seconds,
which is the floor you should use; avoid stacking extra watchers or shorter
custom loops on top of it.

## License

[MIT](LICENSE)
