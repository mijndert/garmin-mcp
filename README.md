# garmin-mcp

Ask Claude about your own Garmin data.

> "How did I sleep this week?" · "Compare my last five runs." · "What's my HRV trend this month?" · "Download yesterday's ride and look at the power curve."

garmin-mcp is a small local [MCP](https://modelcontextprotocol.io) server for the
[Garmin Connect Personal API](https://developer.garmin.com/gc-developer-program/personal/).
It works with Claude Code, Claude Desktop, Cursor and any other MCP client.
Single binary, no dependencies, read-only, your data never leaves your machine
except to talk to Garmin.

## 1. Get your Garmin API key

1. Sign in to [Garmin Connect](https://connect.garmin.com) on the web.
2. Go to **Settings → Personal API Key → Create Key**.
3. Accept the terms, pick how long it should last, and copy the key.
   It's shown only once, so treat it like a password.

## 2. Install

### Claude Code

```sh
curl -fsSL https://raw.githubusercontent.com/mijndert/garmin-mcp/main/install.sh | sh
claude mcp add garmin -s user -e GARMIN_PERSONAL_API_KEY=your-key -- ~/.local/bin/garmin-mcp
```

If `GARMIN_PERSONAL_API_KEY` is already set in your shell, the first command
registers the server for you and you can skip the second.

Prefer Go? `go install github.com/mijndert/garmin-mcp@latest` does the same
as the installer.

### Claude Desktop

1. Download **garmin-mcp.mcpb** from the
   [latest release](https://github.com/mijndert/garmin-mcp/releases/latest).
2. Double-click it (or drag it onto Claude Desktop) and choose **Install**.
3. Paste your API key when asked. It's stored in your system keychain.

Works on macOS (Intel and Apple silicon) and Windows.

### Cursor, Windsurf, Zed and others

Install the binary with the script above, then add this to your client's MCP
config (see [`examples/`](examples/) for ready-made files):

```json
{
  "mcpServers": {
    "garmin": {
      "command": "garmin-mcp",
      "env": { "GARMIN_PERSONAL_API_KEY": "your-key" }
    }
  }
}
```

### Windows

Download the `windows_amd64.zip` from the
[releases page](https://github.com/mijndert/garmin-mcp/releases/latest), unzip
it somewhere on your `PATH`, and use the config snippet above. Claude Desktop
users should use the `.mcpb` bundle instead.

## 3. Try it

Start a chat and ask something. Claude will pick the right tools:

- "How did I sleep last night?" → sleep stages, score and overnight HRV
- "List my runs in October and compare average pace" → activity search + details
- "Has my resting heart rate changed this year?" → daily summaries over a range
- "Plot my weight since January" → weight entries
- "Is my Body Battery recovering overnight?" → Body Battery reports

If something fails with an authentication error, ask Claude to run
`get_auth_status`. It tells you whether the key works and when the current
token expires, without revealing either.

## What Claude can see

Everything is **your** data, and everything is **read-only**. The one exception
is `download_activity_file`, which saves a FIT/GPX file to
`~/Downloads/garmin-mcp` when you ask for it.

| Area | Tools |
| --- | --- |
| Activities | search with filters, full details, workout steps, original file download, auto-detected wellness activities |
| Daily health | daily summary, steps, stress, hydration, heart rate timeline, HR zones, Body Battery, SpO2, Fitness Age |
| Sleep & recovery | nightly sleep detail, sleep summaries, HRV per night and over time |
| Body | weigh-ins with body composition, weight summaries, weight goals, nutrition logs |
| Women's health | cycle summary and day view |
| Profile | units, time zone, birth date, VO2max and other settings |

Dates are `YYYY-MM-DD` in your local time zone. Garmin caps some ranges (28
days for steps and stress, 31 for sleep, hydration and Body Battery, one year
for daily summaries); the server tells Claude when to split a request.

## Options

You rarely need these. Everything has a sensible default.

| Setting | How | Default |
| --- | --- | --- |
| API key | `GARMIN_PERSONAL_API_KEY`, `--api-key-file`, or `~/.config/garmin/connect-personal-api-key` | |
| Garmin China | `--base-url https://personal.connectapi.garmin.cn` | global host |
| Download folder | `--download-dir` or `GARMIN_MCP_DOWNLOAD_DIR` | `~/Downloads/garmin-mcp` |
| Token cache | `--token-cache` path, or `--no-token-cache` | `~/.config/garmin/connect-personal-api-bearer-token.json` |
| HTTP instead of stdio | `--http 127.0.0.1:8787` | stdio |
| Logging | `--log-level debug` (stderr) | `warn` |

Key file and token cache locations match Garmin's official
[Python client](https://github.com/garmin/connect-personal-api-python), so the
two share credentials if you use both.

## Privacy

- Your key is sent only to Garmin's token endpoint, over HTTPS, to obtain a
  24-hour access token. The token is cached on disk with owner-only
  permissions and never logged or shown to Claude.
- The server talks to `personal.connectapi.garmin.com` and nothing else.
- Query strings are stripped from error messages before they reach the model.
- Downloaded archives are extracted with flattened file names so they can't
  write outside the folder you chose.

## Updating and uninstalling

Re-run the install command (or `go install ...@latest`) to update. To remove:

```sh
claude mcp remove -s user garmin
rm ~/.local/bin/garmin-mcp ~/.config/garmin/connect-personal-api-bearer-token.json
```

Then delete the key in Garmin Connect under **Settings → Personal API Key**.

## Contributing

Go 1.25+. `make test` runs the suite (a fake Garmin API in-process, driven
through a real MCP client); no key needed. `make build` produces
`./garmin-mcp`; `mcpb/build.sh <version>` produces the Claude Desktop bundle.
Tools follow Garmin's
[Swagger docs](https://developer.garmin.com/gc-developer-program/personal/swagger-docs/):
one file per tag under `internal/tools/`.

MIT licensed. Not affiliated with or endorsed by Garmin Ltd.
