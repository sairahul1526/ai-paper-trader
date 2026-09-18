package papertrader

import (
	"errors"
	"sync"
	"time"
)

// MinuteCandleBuilder converts Kite's cumulative volume into per-candle
// volume. It does not synthesize candles during gaps.
type MinuteCandleBuilder struct {
	mu             sync.RWMutex
	current        *Bar
	volumeBaseline int64
}

func NewMinuteCandleBuilder() *MinuteCandleBuilder {
	return &MinuteCandleBuilder{}
}

// Update returns a completed bar when a tick starts a later minute.
func (b *MinuteCandleBuilder) Update(t Tick) (*Bar, error) {
	if t.Timestamp.IsZero() {
		return nil, errors.New("tick timestamp is required")
	}
	if t.LastPrice <= 0 {
		return nil, errors.New("tick last_price must be positive")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	start := t.Timestamp.Truncate(time.Minute)
	if b.current == nil {
		b.start(start, t)
		return nil, nil
	}

	if start.After(b.current.Timestamp) {
		completed := *b.current
		b.start(start, t)
		return &completed, nil
	}
	if start.Before(b.current.Timestamp) {
		// Out-of-order ticks must not mutate the current bar.
		return nil, nil
	}

	if t.LastPrice > b.current.High {
		b.current.High = t.LastPrice
	}
	if t.LastPrice < b.current.Low {
		b.current.Low = t.LastPrice
	}
	b.current.Close = t.LastPrice
	b.current.Volume = b.deltaVolume(t)
	return nil, nil
}

func (b *MinuteCandleBuilder) start(start time.Time, t Tick) {
	firstTrade := t.LastQuantity
	if firstTrade < 0 {
		firstTrade = 0
	}
	b.volumeBaseline = t.Volume - firstTrade
	b.current = &Bar{
		Timestamp: start,
		Open:      t.LastPrice,
		High:      t.LastPrice,
		Low:       t.LastPrice,
		Close:     t.LastPrice,
		Volume:    firstTrade,
	}
}

func (b *MinuteCandleBuilder) deltaVolume(t Tick) int64 {
	delta := t.Volume - b.volumeBaseline
	if delta < 0 {
		// A broker volume reset is possible. LastQuantity is safer than a
		// negative bar volume and is still observable by the model.
		if t.LastQuantity > 0 {
			return t.LastQuantity
		}
		return 0
	}
	return delta
}

func (b *MinuteCandleBuilder) Current() *Bar {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.current == nil {
		return nil
	}
	c := *b.current
	return &c
}
