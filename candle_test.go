package papertrader

import (
	"testing"
	"time"
)

func TestMinuteCandleBuilderUsesVolumeDelta(t *testing.T) {
	b := NewMinuteCandleBuilder()
	base := time.Date(2026, 9, 18, 9, 15, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	if _, err := b.Update(Tick{Timestamp: base, LastPrice: 100, LastQuantity: 10, Volume: 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Update(Tick{Timestamp: base.Add(20 * time.Second), LastPrice: 101, LastQuantity: 20, Volume: 1030}); err != nil {
		t.Fatal(err)
	}
	completed, err := b.Update(Tick{Timestamp: base.Add(time.Minute), LastPrice: 102, LastQuantity: 10, Volume: 1040})
	if err != nil {
		t.Fatal(err)
	}
	if completed == nil || completed.Volume != 40 {
		t.Fatalf("expected 40 shares in completed candle, got %#v", completed)
	}
	if completed.Close != 101 {
		t.Fatalf("expected close 101, got %v", completed.Close)
	}
}

func TestHistoryStoreTrimsOldestBars(t *testing.T) {
	h := NewHistoryStore(2, 1, 1)
	for i := 0; i < 3; i++ {
		h.AddOneMinute(Bar{Timestamp: time.Unix(int64(i), 0), Open: 1, High: 1, Low: 1, Close: float64(i + 1)})
	}
	got := h.Snapshot().OneMinute
	if len(got) != 2 || got[0].Close != 2 || got[1].Close != 3 {
		t.Fatalf("unexpected history: %#v", got)
	}
}

func TestHistoryStoreReplacesDuplicateTimestamp(t *testing.T) {
	h := NewHistoryStore(10, 10, 10)
	ts := time.Unix(100, 0)
	h.AddOneMinute(Bar{Timestamp: ts, Open: 1, High: 1, Low: 1, Close: 1})
	h.AddOneMinute(Bar{Timestamp: ts, Open: 1, High: 2, Low: 1, Close: 2})
	got := h.Snapshot().OneMinute
	if len(got) != 1 || got[0].Close != 2 {
		t.Fatalf("duplicate timestamp was not replaced: %#v", got)
	}
}

func TestTrimHistoricalContextKeepsNewestBarsWithinBudget(t *testing.T) {
	history := HistoricalContext{}
	for i := 0; i < 100; i++ {
		history.OneMinute = append(history.OneMinute, Bar{Timestamp: time.Unix(int64(i), 0), Open: 1, High: 2, Low: 1, Close: 1, Volume: 1})
	}
	trimmed := TrimHistoricalContext(history, 1000)
	if len(trimmed.OneMinute) == 0 || len(trimmed.OneMinute) >= len(history.OneMinute) {
		t.Fatalf("expected history to be trimmed: %d", len(trimmed.OneMinute))
	}
	if !trimmed.OneMinute[len(trimmed.OneMinute)-1].Timestamp.Equal(history.OneMinute[len(history.OneMinute)-1].Timestamp) {
		t.Fatal("trim should preserve newest bar")
	}
}
