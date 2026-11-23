# Security policy

## Data handling

ParamLens analyzes statements that frequently contain sensitive data, so it is designed to keep
everything local:

- It connects only to the SQL Server instance the user configures. No telemetry, no update checks, no
  other network calls. The interface has no external dependencies (no CDN).
- The `.exe` serves its interface on `127.0.0.1` only, requires a random per-session token and checks the
  `Host` header (protection against DNS rebinding). Passwords stay in process memory only.
- Statements read by the `.exe` are written to a temporary file (for the per-execution samples) that is
  deleted when the program exits normally.
- `ParamLens.html` reads the exported file inside the browser; nothing is uploaded.
- The `.exe` shows its interface in a WebView2 window (the Edge engine of Windows) that only loads the
  local page; its browser profile (cache, preferences) lives in `%LOCALAPPDATA%\ParamLens\WebView2`.
- Exported HTML reports contain the analysis (including parameter values): share them as carefully as
  the capture itself. The same goes for exported parameters (script, CSV or the table they were inserted
  into), which hold the complete values of every call. Saved comparison baselines hold the shortened SQL
  text of the queries and their metrics, no parameter values.
- **Insert into a database** writes only to the server, database and table chosen in the dialog, on a
  connection opened by the user.

## Supported versions

Security fixes go into the latest release. Every published version stays downloadable on the
[Releases](../../releases) page, but please update to the latest one.

## Reporting a vulnerability

Please **do not open a public issue**. Use GitHub's private reporting instead: the **Security** tab of
the repository › **Report a vulnerability**. Include the version, what you observed and how to
reproduce it — with anonymized data only.

You will get an answer as soon as possible, and the fix will be credited in the changelog unless you
prefer otherwise.
