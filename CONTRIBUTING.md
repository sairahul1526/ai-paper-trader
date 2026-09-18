# Contributing

Thanks for helping improve AI Paper Trader.

## Before opening a pull request

1. Keep the project paper-only. Do not add a live order path, hidden fallback,
   or credential logging.
2. Keep provider-specific code under `internal/broker/<provider>` and expose
   broker-neutral state through the root package.
3. Add or update offline tests for adapter behavior. Tests must not require a
   real broker, market-data account, or TypeSafe key.
4. Run the complete local checks:

   ```bash
   gofmt -w $(rg --files -g '*.go')
   go test ./...
   go test -race ./...
   go vet ./...
   node --check ui/app.js
   git diff --check
   ```

5. Do not commit `.env` files, API keys, access tokens, run logs, or account
   data. The repository ignores those paths, but review `git diff --cached`
   before publishing.

For provider changes, document feed coverage, latency/delay behavior, rate
limits, authentication, and any paid-tier assumptions in the README.
