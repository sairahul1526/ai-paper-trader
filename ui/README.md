# Local paper-trader dashboard

The dashboard is a stdlib-only single-page UI embedded into the Go server. It
has no build step or third-party frontend runtime. Start it from the repository
root:

```bash
go run ./ui
```

Open <http://127.0.0.1:8787>. The top market tabs switch between the India/Kite
and US/Alpaca setup flows; the ticker and venue are sent to the runner. The
configuration guide, credentials, request-token exchange, history budget, risk
rules, run/stop controls, real-time market charts, TypeSafe decisions, paper
trades, console logs, and saved-run summaries are all on this page.

The browser polls the local state API every second. Charts support hover
tooltips, wheel zoom, drag-to-pan, quick windows, and date filters; timestamps
are displayed in IST. Credentials are never persisted while typing. The
explicit save action stores them in local storage on the current browser only,
and the server exposes only non-secret diagnostics (set, length, hash prefix).

Live order execution is not available through this console.
