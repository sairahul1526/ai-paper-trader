package papertrader

import (
	"testing"
	"time"
)

func TestRiskGateSizesBuyAndCreatesStop(t *testing.T) {
	s := testSnapshot()
	now := s.Session.AsOf
	s.Session.MarketOpen = now.Add(-time.Hour)
	s.Session.MarketClose = now.Add(time.Hour)
	s.Session.SquareOffAt = now.Add(30 * time.Minute)
	e := Evaluation{SnapshotID: s.SnapshotID, Action: ActionBuy, ActionConfidence: 0.8, BuySupport: 0.9}
	g := RiskGate{Limits: RiskLimits{Capital: 10000, MaxPositionValuePercent: 25, StopLossPercent: 1, MaxDailyLossPercent: 1, MaxTradesPerDay: 5, MinActionConfidence: 0.6, MinBuySupport: 0.6}}
	intent, err := g.Authorize(s, e)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Quantity != 25 || intent.StopLoss != 99 {
		t.Fatalf("unexpected intent: %#v", intent)
	}
}

func TestRiskGateRejectsStaleQuote(t *testing.T) {
	s := testSnapshot()
	s.Session.AsOf = s.Session.AsOf.Add(time.Minute)
	g := RiskGate{Limits: RiskLimits{Capital: 10000, MaxPositionValuePercent: 25, MaxDataAge: 5 * time.Second}}
	_, err := g.Authorize(s, Evaluation{SnapshotID: s.SnapshotID, Action: ActionBuy, ActionConfidence: 1, BuySupport: 1})
	if err == nil {
		t.Fatal("expected stale quote rejection")
	}
}
