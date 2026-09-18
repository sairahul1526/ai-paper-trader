# AI paper trader

This is a standalone, paper-only AI equity trader. The default example uses
`NSE:RELIANCE`, but the runner accepts any cash-equity exchange and symbol.

The module's intended loop is:

1. Resolve the configured cash-equity instrument from Kite's daily instrument dump.
2. Subscribe to Kite WebSocket full-mode ticks.
3. Convert ticks into completed one-minute candles and retain bounded 1m/5m/1d history.
4. Write every broker tick to a paper market log while sending one TypeSafe
   System One request per completed minute.
5. Let the typed AI action pass through the local risk gate.
6. Submit an order only when the risk gate authorizes it.

There are no local entry signals in this module. Local code handles data
integrity, position sizing, exposure limits, stale-data checks, stops, and
square-off safety.

The TypeSafe request uses `POST https://api.typesafe.ai/v1/systemone` with the
`jev-latest` model. Set `TYPESAFE_API_KEY` server-side; never put it in a
checked-in config file.

Each decision event records measured end-to-end TypeSafe latency plus input and
output token usage. The dashboard estimates AI cost using TypeSafe's current
Jev price of $0.042 per million input tokens (output tokens are free); it shows
unknown rather than inventing a cost when a response has no usage metadata.

This package is paper-only and has no live-order sink.

## Kite paper mode

Paper mode requires `KITE_API_KEY`, `KITE_API_SECRET`, `KITE_ACCESS_TOKEN`, and
`TYPESAFE_API_KEY`. The command refuses `-paper=false`; there is no synthetic
trading mode. Kite access tokens are API-key-specific and generally expire
daily, so generate a fresh token for the same API key before each session when
Kite rejects the token.

## Run

From this repository's root:

```bash
go run ./ui
```

The command-line runner is also available for a configured symbol:

```bash
go run ./cmd/paper-trader -paper=true -exchange=NSE -symbol=RELIANCE
```
