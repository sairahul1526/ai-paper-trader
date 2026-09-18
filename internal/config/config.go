// Package config contains the small set of broker-agnostic connection
// settings shared by market-data adapters. Trading policy lives in the root
// papertrader package; this package deliberately does not contain strategy
// or options-specific configuration.
package config

import "time"

// TickerConfig controls reconnect behavior for a streaming market-data feed.
type TickerConfig struct {
	AutoReconnect       bool
	MaxReconnectDelay   time.Duration
	ReconnectMaxRetries int
}
