package papertrader

import (
	"context"
	"testing"
	"time"
)

type fixedProvider struct{}

func (fixedProvider) Evaluate(_ context.Context, s Snapshot) (Evaluation, error) {
	return Evaluation{SnapshotID: s.SnapshotID, Action: ActionNoAction}, nil
}

func TestMinuteRuntimeOnlyEvaluatesCompletedMinutes(t *testing.T) {
	base := time.Date(2026, 9, 18, 9, 15, 0, 0, time.UTC)
	history := NewHistoryStore(10, 10, 10)
	orders := &PaperOrderSink{}
	runner := &Runner{Provider: fixedProvider{}, Risk: RiskGate{Limits: RiskLimits{Capital: 10000, MaxDailyLossPercent: 1, MaxTradesPerDay: 5}}, Orders: orders}
	runtime := MinuteRuntime{
		CandleBuilder: NewMinuteCandleBuilder(),
		History:       history,
		Runner:        runner,
		Snapshots:     SnapshotBuilder{Instrument: InstrumentState{Exchange: ExchangeNSE, TradingSymbol: DefaultTradingSymbol}, History: history},
	}
	processed, err := runtime.HandleTick(context.Background(), Tick{Timestamp: base, LastPrice: 100, LastQuantity: 10, Volume: 1000})
	if err != nil || processed {
		t.Fatalf("first tick should not process a minute: processed=%v err=%v", processed, err)
	}
	processed, err = runtime.HandleTick(context.Background(), Tick{Timestamp: base.Add(time.Minute), LastPrice: 101, LastQuantity: 10, Volume: 1010})
	if err != nil || !processed {
		t.Fatalf("second minute should process: processed=%v err=%v", processed, err)
	}
	if len(history.Snapshot().OneMinute) != 1 {
		t.Fatalf("expected one completed bar")
	}
}
