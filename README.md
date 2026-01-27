# gh-rate-limit-status

A GitHub CLI extension that shows your GitHub API rate-limit status in a user-friendly table.

## Installation

```sh
gh extension install gh-tui-tools/gh-rate-limit-status
```

## Usage

```sh
gh rate-limit-status             # Show your rate limits
gh rate-limit-status -a          # Show all limits (including low-volume ones)
gh rate-limit-status --json      # Output as processed JSON (see below)
gh rate-limit-status -w          # Watch mode: continuously update display
gh rate-limit-status --warn 20   # Exit with code 1 if any limit is below 20%
gh rate-limit-status -h          # Show help
```

## Example output

```
        Resource              Remaining               Meter            Resets in
  --------------------   -------------------   --------------------   ----------
  graphql                    3517/5000   70%   ██████████████░░░░░░      15m 50s
  core                       4603/5000   92%   ██████████████████░░      15m 22s
  code_scanning_upload       4603/5000   92%   ██████████████████░░      15m 22s
```

Features:
- **Visual progress bars** let you see your remaining quota at a glance
- **Percentage remaining** shows you the exact percentage alongside the numbers
- **Color-coded by urgency** — green → yellow → red as your limits deplete
- **Sorted by percentage remaining** — your most critical limits appear at top
- **Relative reset times** tell you `15m 30s` rather than absolute timestamps
- **Filters noise** by default — hides low-volume and enterprise-only limits (use `-a` to see all)

## About GitHub API rate limits

GitHub gives you separate rate limits for different API resources:

| Resource | Limit | Description |
|----------|-------|-------------|
| `core` | 5,000/hour | REST API requests |
| `graphql` | 5,000/hour | GraphQL API requests (separate pool from REST) |
| `search` | 30/minute | Search API requests |
| `code_search` | 10/minute | Code search requests |

Your `core` and `graphql` limits are independent — you can exhaust one while having plenty of the other remaining. Tools that heavily use GraphQL (like many `gh` extensions) may exhaust your GraphQL limit while `core` remains high.

### GitHub documentation

- [REST API endpoints for rate limits](https://docs.github.com/en/rest/rate-limit/rate-limit) — the API endpoint this tool queries (`GET /rate_limit`)
- [Rate limits for the REST API](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api) — details on how your REST API limits are calculated
- [Rate limits and query limits for the GraphQL API](https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api) — your GraphQL-specific limits (which use a separate quota from REST)

## JSON output

You can already get JSON from `gh api rate_limit` directly. The `--json` flag here gives you *processed* JSON that's more useful for scripting:

| Feature | `gh api rate_limit` | `gh rate-limit-status --json` |
|---------|---------------------|-------------------------------|
| Structure | Nested `resources` object | Flat array |
| Sorting | Unsorted | By percentage remaining (critical first) |
| Filtering | All limits | High-volume only (or all with `-a`) |
| Percentage | Not included | `percent_remaining` field |
| Reset time | Unix timestamp only | Both timestamp and human-readable `reset_in` |

Example output:

```json
[
  {
    "resource": "graphql",
    "remaining": 3517,
    "limit": 5000,
    "reset_timestamp": 1234567890,
    "reset_in": "15m 50s",
    "percent_remaining": 70.3
  }
]
```

## See also

**[quotidian-ennui/gh-rate-limit](https://github.com/quotidian-ennui/gh-rate-limit)** — another `gh` extension for viewing rate limits.

Differences from `gh-rate-limit`:

| | gh-rate-limit-status | gh-rate-limit |
|---|---|---|
| **Reset time format** | Relative (`15m 30s`) | Absolute (`14:46:20`) |
| **Visual indicator** | Color-coded progress bars | None |
| **Sorting** | By percentage remaining (critical first) | Unsorted |
| **Filtering** | Hides low-volume limits by default | Shows all |
| **JSON output** | Processed (sorted, filtered, with percentages) | None |
| **Watch mode** | Yes (`-w`) | No |
| **Warning threshold** | Yes (`--warn`) | No |
| **Dependencies** | Python (no extras) | Shell + jq |

Choose `gh-rate-limit-status` if you want visual progress bars, watch mode, or scripting features. Choose `gh-rate-limit` if you prefer absolute reset times or want a shell-only solution — or if you just want something simpler.
