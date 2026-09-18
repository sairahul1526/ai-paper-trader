package papertrader

import (
	"encoding/json"
	"sync"
)

// HistoryStore keeps a bounded, oldest-first view for each timeframe.
type HistoryStore struct {
	mu sync.RWMutex

	maxOneMinute  int
	maxFiveMinute int
	maxDaily      int
	oneMinute     []Bar
	fiveMinute    []Bar
	daily         []Bar
}

func NewHistoryStore(maxOneMinute, maxFiveMinute, maxDaily int) *HistoryStore {
	if maxOneMinute < 0 {
		maxOneMinute = 0
	}
	if maxFiveMinute < 0 {
		maxFiveMinute = 0
	}
	if maxDaily < 0 {
		maxDaily = 0
	}
	return &HistoryStore{
		maxOneMinute: maxOneMinute, maxFiveMinute: maxFiveMinute, maxDaily: maxDaily,
		oneMinute:  make([]Bar, 0, maxOneMinute),
		fiveMinute: make([]Bar, 0, maxFiveMinute),
		daily:      make([]Bar, 0, maxDaily),
	}
}

func (h *HistoryStore) Seed(oneMinute, fiveMinute, daily []Bar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.oneMinute = trimCopy(oneMinute, h.maxOneMinute)
	h.fiveMinute = trimCopy(fiveMinute, h.maxFiveMinute)
	h.daily = trimCopy(daily, h.maxDaily)
}

func (h *HistoryStore) AddOneMinute(bar Bar)  { h.add(&h.oneMinute, h.maxOneMinute, bar) }
func (h *HistoryStore) AddFiveMinute(bar Bar) { h.add(&h.fiveMinute, h.maxFiveMinute, bar) }
func (h *HistoryStore) AddDaily(bar Bar)      { h.add(&h.daily, h.maxDaily, bar) }

func (h *HistoryStore) add(target *[]Bar, max int, bar Bar) {
	if max == 0 || !bar.Valid() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(*target) - 1; i >= 0; i-- {
		if (*target)[i].Timestamp.Equal(bar.Timestamp) {
			(*target)[i] = bar
			return
		}
		if (*target)[i].Timestamp.Before(bar.Timestamp) {
			break
		}
	}
	*target = append(*target, bar)
	if len(*target) > max {
		*target = (*target)[len(*target)-max:]
	}
}

func (h *HistoryStore) Snapshot() HistoricalContext {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HistoricalContext{
		OneMinute:  append([]Bar(nil), h.oneMinute...),
		FiveMinute: append([]Bar(nil), h.fiveMinute...),
		Daily:      append([]Bar(nil), h.daily...),
	}
}

// TrimHistoricalContext keeps the newest bars while respecting a serialized
// state budget. TypeSafe's request token budget includes both state and
// questions, so a byte cap prevents oversized high-history requests.
func TrimHistoricalContext(history HistoricalContext, maxBytes int) HistoricalContext {
	if maxBytes <= 0 {
		return history
	}
	history.OneMinute = append([]Bar(nil), history.OneMinute...)
	history.FiveMinute = append([]Bar(nil), history.FiveMinute...)
	history.Daily = append([]Bar(nil), history.Daily...)
	for historySize(history) > maxBytes {
		switch {
		case len(history.OneMinute) >= len(history.FiveMinute) && len(history.OneMinute) >= len(history.Daily) && len(history.OneMinute) > 0:
			history.OneMinute = history.OneMinute[1:]
		case len(history.FiveMinute) >= len(history.Daily) && len(history.FiveMinute) > 0:
			history.FiveMinute = history.FiveMinute[1:]
		case len(history.Daily) > 0:
			history.Daily = history.Daily[1:]
		default:
			return history
		}
	}
	return history
}

func historySize(history HistoricalContext) int {
	encoded, err := json.Marshal(history)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func trimCopy(values []Bar, max int) []Bar {
	if max == 0 || len(values) == 0 {
		return nil
	}
	if len(values) > max {
		values = values[len(values)-max:]
	}
	return append([]Bar(nil), values...)
}
