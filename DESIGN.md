# Design: gh-rate-limit-status

## Overview

gh-rate-limit-status is a GitHub CLI extension that displays your GitHub API rate-limit status in a user-friendly table with visual progress bars.

## Goals

1. **At-a-glance status**: Show remaining API quota with visual indicators so you can quickly assess your situation
2. **Highlight urgency**: Sort by percentage remaining and color-code so critical limits appear first and stand out
3. **Relative time**: Show reset times as `15m 30s` rather than absolute timestamps — you care about "how long until reset" not "what time does it reset"
4. **Single binary**: a precompiled Go binary with no runtime dependencies
5. **Sensible defaults**: Hide low-volume limits (code_search, dependency_snapshots, etc.) that you probably don’t care about

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   CLI Parser    │────▶│  Data Fetching   │────▶│  Table Renderer │
│  (sys.argv)     │     │  (go-gh REST)    │     │  (stdout)       │
└─────────────────┘     └──────────────────┘     └─────────────────┘
```

### Data flow

1. **Parse arguments**: Check for flags (`-h`, `-a`, `--json`, `-w`, `--warn`)
2. **Fetch rate limits**: Call `GET /rate_limit` via the go-gh REST client and parse the JSON response
3. **Filter limits**: By default, show only limits with > 100 quota (hides low-volume endpoints)
4. **Sort by urgency**: Order by percentage remaining (lowest first)
5. **Render output**: Print table (default) or JSON (`--json`)

## GitHub API

The tool queries a single endpoint:

```
GET /rate_limit
```

This returns all rate limit categories in one response:

```json
{
  "resources": {
    "core": {"limit": 5000, "remaining": 4500, "reset": 1234567890},
    "graphql": {"limit": 5000, "remaining": 3000, "reset": 1234567890},
    "search": {"limit": 30, "remaining": 30, "reset": 1234567890},
    ...
  }
}
```

The `reset` field is a Unix timestamp indicating when the limit resets.

## Design decisions

### Relative reset times

**Problem**: Absolute timestamps (`14:46:20`) require mental math to determine how long until reset.

**Solution**: Show relative times (`15m 30s`) that directly answer your question.

```python
def format_reset_time(timestamp):
    delta = datetime.fromtimestamp(timestamp) - datetime.now()
    minutes = int(delta.total_seconds() // 60)
    seconds = int(delta.total_seconds() % 60)
    return f"{minutes}m {seconds:02d}s"
```

Seconds are zero-padded (`02d`) to maintain consistent column alignment.

### Visual progress bars

**Problem**: Numbers like `3517/5000` require calculation to understand severity.

**Solution**: 20-character progress bars show remaining quota at a glance:

```
██████████████░░░░░░  (70% remaining)
████░░░░░░░░░░░░░░░░  (20% remaining)
```

### Why both percentage numbers and visual bars?

The output shows percentage in two forms: as a number (`70%`) and as a bar (`██████████████░░░░░░`). Each serves a different purpose:

**Bars are for scanning**: When you glance at the table, the bars let you instantly spot which limits are low without reading any numbers. Your eye is drawn to short bars. This is useful for the common case: “Is anything critically low?”

**Numbers are for precision**: When you need to make a decision ("Can I run one more report?"), you need exact values. `70% remaining` tells you more than a bar that looks "mostly full."

**Comparing across rows**: Bars make relative comparison easy — you can see at a glance that `graphql` is lower than `core` without comparing `3517/5000` to `4603/5000`.

Together, they support both quick scanning and detailed analysis without requiring you to switch tools or modes.

### Color coding

Three urgency levels based on percentage remaining:

| Percentage | Color | Meaning |
|------------|-------|---------|
| > 50% | Green | Healthy |
| 20-50% | Yellow | Caution |
| < 20% | Red | Critical |

Colors are applied to both the resource name and the progress bar.

Color output is disabled automatically when stdout is not a terminal — for example when piping or redirecting — or when the [`NO_COLOR`](https://no-color.org/) environment variable is set. This detection comes from the GitHub CLI’s `term` package, so it follows the same conventions as `gh` itself.

### Sorting by urgency

Limits are sorted by percentage remaining (ascending), so the most critical limits appear at the top of the table. This means you see problems first without scrolling.

### Filtering

By default, two categories of limits are hidden:

**Low-volume limits** (≤ 100 quota):
- `code_search` (10/minute)
- `dependency_snapshots` (100/hour)
- etc.

**Enterprise/admin limits** that most users never hit:
- `integration_manifest` — GitHub App manifest creation
- `actions_runner_registration` — Self-hosted runner registration
- `scim` — Enterprise SSO user provisioning
- `audit_log` — Enterprise audit logs

These limits rarely matter to you and clutter the output. The `-a` flag shows all limits for completeness.

### Column alignment

The table uses dynamic column widths based on the longest resource name:

```python
name_width = max(len(item["name"]) for item in limits)
```

ANSI color codes are applied *after* padding to avoid alignment issues — the padding is calculated on the plain text, then colors wrap the padded string.

### No runtime dependencies

The tool is a single precompiled Go binary. At build time it depends only on `github.com/cli/go-gh/v2` (the official GitHub CLI library) for the REST client and terminal/color detection. At runtime it needs nothing installed beyond `gh` itself — not even a language runtime — which is what makes it work identically on Linux, macOS, and Windows.

### Distribution

The extension is distributed as a **precompiled** `gh` extension. A GitHub Actions workflow (`cli/gh-extension-precompile`) cross-compiles per-platform binaries on each `v*` tag and attaches them to the release as `gh-rate-limit-status-<os>-<arch>[.exe]`. `gh extension install` and `gh extension upgrade` then download the binary matching the user’s platform, including Windows — unlike an interpreted script extension, which has no reliable shebang mechanism on Windows.

### JSON output

**Why not just use `gh api rate_limit`?** You can, but the raw API response requires post-processing for most scripting tasks. The `--json` flag gives you processed output that's ready to use:

1. **Flat array** instead of nested `resources` object — easier to iterate
2. **Sorted by urgency** — critical limits appear first
3. **Filtered** — only high-volume limits by default (use `-a` for all)
4. **Calculated fields** — `percent_remaining` and human-readable `reset_in` are pre-computed

```json
[
  {
    "resource": "graphql",
    "remaining": 3517,
    "limit": 5000,
    "reset_timestamp": 1234567890,
    "reset_in": "15m 50s",
    "percent_remaining": 70.3
  },
  ...
]
```

This saves you from writing jq transformations or Python scripts to extract the same information.

### Watch mode

The `-w`/`--watch` flag continuously refreshes the display every 5 seconds. Useful for monitoring during long-running operations. Press Ctrl+C to stop.

### Warning threshold

The `--warn <pct>` flag exits with code 1 if any limit falls below the specified percentage. Useful in scripts:

```sh
gh rate-limit-status --warn 10 || echo "Rate limit critically low!"
```

## Output format

```
           Resource                  Remaining               Meter            Resets in
  ---------------------------   -------------------   --------------------   ----------
  graphql                           3517/5000   70%   ██████████████░░░░░░      15m 50s
  core                              4603/5000   92%   ██████████████████░░      15m 22s
  integration_manifest              5000/5000  100%   ████████████████████      59m 59s
```

The Remaining column shows `remaining/limit` followed by the percentage remaining, right-aligned so percentages line up:
- Two-digit percentages: `4603/5000   92%` (three spaces before)
- Three-digit percentages: `5000/5000  100%` (two spaces before)

Column widths:
- **Resource**: Dynamic (max resource name length)
- **Remaining**: 19 characters, right-aligned (fits `NNNNNN/NNNNNN  NNN%`)
- **Meter**: 20 characters fixed
- **Resets in**: 10 characters, right-aligned

## Not implemented

### Secondary rate limits (abuse detection)

GitHub enforces secondary rate limits separately from the primary limits shown here. These are not exposed by the `/rate_limit` API endpoint — you only discover them when you hit them (via 403 responses with `Retry-After` headers). There's no way to proactively display your secondary rate limit status.

## Future improvements

- **`--quiet`** — Only output when limits are below a threshold (pairs with `--warn` for silent monitoring scripts)
- **`-w SECONDS`** — Custom refresh interval for watch mode (currently hardcoded to 5 seconds)
- **`--resource graphql,core`** — Filter output to specific resources
- **`--compact`** — Single-line output suitable for shell prompts or quick checks
- **`--until <pct>`** — Block until a resource recovers above a threshold (useful before running rate-limit-heavy scripts)
- **Depletion estimates** — If you're actively consuming quota, estimate when you'll hit zero based on recent usage patterns
