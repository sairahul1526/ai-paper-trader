package papertrader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testSnapshot() Snapshot {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	return Snapshot{
		SnapshotID: "PAPER-test-1",
		Instrument: InstrumentState{Exchange: ExchangeNSE, TradingSymbol: DefaultTradingSymbol},
		Quote:      Quote{LastPrice: 100, Bid: 99.9, Ask: 100.1, Timestamp: now},
		CurrentBar: Bar{Timestamp: now, Open: 99, High: 101, Low: 98, Close: 100, Volume: 1000},
		Session:    SessionState{AsOf: now},
		Position:   PositionState{Side: "flat"},
		Account:    AccountState{Capital: 10000},
	}
}

func TestTypeSafeClientBuildsAndParsesTypedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing authorization header")
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "jev-latest" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"action":{"type":"choice","choice":"buy","probabilities":{"buy":0.8,"hold":0.1,"exit":0.05,"no_action":0.05},"confidence":0.8},"buy_support":{"type":"noul","noul":0.81},"market_regime":{"type":"choice","choice":"trending_up","probabilities":{"trending_up":0.9},"confidence":0.9},"setup_quality":{"type":"score","score":3.4,"probabilities":{"0":0.01,"1":0.04,"2":0.1,"3":0.4,"4":0.45},"confidence":0.8},"holding_horizon":{"type":"choice","choice":"intraday","probabilities":{"intraday":1},"confidence":1}},"usage":{"input_tokens":10,"output_tokens":20}}`))
	}))
	defer server.Close()

	client := NewTypeSafeClient("test-key")
	client.Endpoint = server.URL
	evaluation, err := client.Evaluate(context.Background(), testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Action != ActionBuy || evaluation.BuySupport != 0.81 || evaluation.SnapshotID != "PAPER-test-1" {
		t.Fatalf("unexpected evaluation: %#v", evaluation)
	}
	if evaluation.Usage.InputTokens != 10 || evaluation.Usage.OutputTokens != 20 || evaluation.Latency <= 0 {
		t.Fatalf("usage or latency was not captured: %#v", evaluation)
	}
	cost, known := EstimateTypeSafeCostUSD(evaluation.Usage)
	if !known || cost < 0.000000419 || cost > 0.000000421 {
		t.Fatalf("unexpected cost estimate: %.12f known=%v", cost, known)
	}
}

func TestEstimateTypeSafeCostRequiresInputUsage(t *testing.T) {
	if cost, known := EstimateTypeSafeCostUSD(Usage{}); known || cost != 0 {
		t.Fatalf("zero usage should have unknown cost: %.12f known=%v", cost, known)
	}
}
