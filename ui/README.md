# AI paper console

This is a local, stdlib-only single-page dashboard for the AI paper runner. It
reads paper-only JSONL events and summaries from `runs`,
shows live paper ticks, TypeSafe decisions, candles, paper intents, risk errors,
console output, and saved runs, and can start or stop the paper-only runner.
The market stream refreshes every second; TypeSafe remains deliberately at one
request per completed minute. Charts support hover details in IST, wheel zoom,
drag-to-pan, quick windows, and date filters over the loaded tick window.
Decision rows include measured request latency, token usage, and the current
Jev input-token cost estimate.

Run settings are persisted in browser local storage as they change. Credentials
are never persisted while typing; use the explicit “Save credentials” button to
store them in this browser. Stored credentials are not encrypted, so use that
option only on a trusted machine. “Clear saved values” removes the local
dashboard state.

Run it from the repository root:

```bash
go run ./ui
```

Open <http://127.0.0.1:8787>. The UI accepts credentials only in memory while
starting a paper process, clears unsaved credential fields after submission, and
never returns their values from the dashboard API. The Kite controls can also
exchange a fresh one-time request token for the daily access token. The log
panel lets you select how many trailing lines to display. Live order execution
is not available through this console.
