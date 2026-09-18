package alpaca

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInstrumentTokenIsStable(t *testing.T) {
	first := InstrumentToken("aapl")
	second := InstrumentToken("AAPL")
	if first == 0 || first != second {
		t.Fatalf("expected a stable non-zero token, got %d and %d", first, second)
	}
}

func TestHistoricalProviderLoadsIEXBars(t *testing.T) {
	var gotURL string
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotKey = r.Header.Get("APCA-API-KEY-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bars":[{"t":"2026-09-18T13:30:00Z","o":100,"h":102,"l":99,"c":101,"v":2500}],"next_page_token":""}`))
	}))
	defer server.Close()

	client := NewClient("public-key", "secret")
	client.DataURL = server.URL
	instrument, err := client.ResolveEquity(context.Background(), "NASDAQ", "AAPL")
	if err != nil {
		t.Fatal(err)
	}
	provider := HistoricalProvider{Client: client}
	bars, err := provider.Load(context.Background(), instrument.InstrumentToken, "minute", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 1 || bars[0].Close != 101 || bars[0].Volume != 2500 {
		t.Fatalf("unexpected bars: %#v", bars)
	}
	if gotKey != "public-key" || !strings.Contains(gotURL, "feed=iex") || !strings.Contains(gotURL, "timeframe=1Min") {
		t.Fatalf("request did not use authenticated IEX minute feed: url=%q key=%q", gotURL, gotKey)
	}
}
