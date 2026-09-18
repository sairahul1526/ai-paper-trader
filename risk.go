package papertrader

import (
	"errors"
	"fmt"
	"time"
)

// RiskLimits are the only local decision constraints in the AI-first design.
type RiskLimits struct {
	Capital                 float64
	MaxPositionValuePercent float64
	MaxDailyLossPercent     float64
	MaxTradesPerDay         int
	StopLossPercent         float64
	MaxSpreadPercent        float64
	MaxDataAge              time.Duration
	MinActionConfidence     float64
	MinBuySupport           float64
	AllowShort              bool
}

type RiskGate struct {
	Limits RiskLimits
}

func (g RiskGate) Authorize(snapshot Snapshot, evaluation Evaluation) (OrderIntent, error) {
	if err := snapshot.Validate(); err != nil {
		return OrderIntent{}, err
	}
	if evaluation.SnapshotID != "" && evaluation.SnapshotID != snapshot.SnapshotID {
		return OrderIntent{}, errors.New("stale typesafe response: snapshot_id mismatch")
	}

	now := snapshot.Session.AsOf
	if now.IsZero() {
		now = snapshot.CurrentBar.Timestamp
	}
	if !snapshot.Session.MarketOpen.IsZero() && now.Before(snapshot.Session.MarketOpen) {
		return OrderIntent{}, errors.New("market is not open")
	}
	if !snapshot.Session.SquareOffAt.IsZero() && !now.Before(snapshot.Session.SquareOffAt) {
		return OrderIntent{}, errors.New("square-off window has started")
	}
	if !snapshot.Session.MarketClose.IsZero() && now.After(snapshot.Session.MarketClose) {
		return OrderIntent{}, errors.New("market is closed")
	}

	if g.Limits.MaxDataAge > 0 && !snapshot.Quote.Timestamp.IsZero() {
		age := now.Sub(snapshot.Quote.Timestamp)
		if age < 0 {
			age = 0
		}
		if age > g.Limits.MaxDataAge {
			return OrderIntent{}, fmt.Errorf("quote is stale: %s", age.Round(time.Millisecond))
		}
	}
	if g.Limits.MaxSpreadPercent > 0 && snapshot.Quote.Bid > 0 && snapshot.Quote.Ask >= snapshot.Quote.Bid {
		spreadPercent := ((snapshot.Quote.Ask - snapshot.Quote.Bid) / snapshot.Quote.LastPrice) * 100
		if spreadPercent > g.Limits.MaxSpreadPercent {
			return OrderIntent{}, fmt.Errorf("spread %.4f%% exceeds limit %.4f%%", spreadPercent, g.Limits.MaxSpreadPercent)
		}
	}
	switch evaluation.Action {
	case ActionBuy:
		if g.Limits.MaxDailyLossPercent > 0 && snapshot.Account.DailyPnL <= -(g.Limits.Capital*g.Limits.MaxDailyLossPercent/100) {
			return OrderIntent{}, errors.New("daily loss limit reached")
		}
		if g.Limits.MaxTradesPerDay > 0 && snapshot.Account.TradesToday >= g.Limits.MaxTradesPerDay {
			return OrderIntent{}, errors.New("maximum trades per day reached")
		}
		return g.authorizeBuy(snapshot, evaluation)
	case ActionExit:
		if snapshot.Position.Side != "long" || snapshot.Position.Quantity <= 0 {
			return OrderIntent{Action: ActionNoAction}, nil
		}
		return OrderIntent{
			Action: ActionExit, Exchange: snapshot.Instrument.Exchange, Symbol: snapshot.Instrument.TradingSymbol,
			Side: "sell", Quantity: snapshot.Position.Quantity, Price: snapshot.Quote.LastPrice,
			Tag: "paper-trader-exit",
		}, nil
	case ActionShort:
		if !g.Limits.AllowShort {
			return OrderIntent{Action: ActionNoAction}, errors.New("short selling is disabled")
		}
		return OrderIntent{Action: ActionNoAction}, errors.New("short authorization is not implemented")
	default:
		return OrderIntent{Action: ActionNoAction}, nil
	}
}

func (g RiskGate) authorizeBuy(snapshot Snapshot, evaluation Evaluation) (OrderIntent, error) {
	if snapshot.Position.Side != "flat" || snapshot.Position.Quantity != 0 {
		return OrderIntent{Action: ActionNoAction}, errors.New("position already exists; pyramiding is disabled")
	}
	if g.Limits.MinActionConfidence > 0 && evaluation.ActionConfidence < g.Limits.MinActionConfidence {
		return OrderIntent{Action: ActionNoAction}, fmt.Errorf("action confidence %.4f below %.4f", evaluation.ActionConfidence, g.Limits.MinActionConfidence)
	}
	if g.Limits.MinBuySupport > 0 && evaluation.BuySupport < g.Limits.MinBuySupport {
		return OrderIntent{Action: ActionNoAction}, fmt.Errorf("buy support %.4f below %.4f", evaluation.BuySupport, g.Limits.MinBuySupport)
	}
	price := snapshot.Quote.LastPrice
	if price <= 0 {
		return OrderIntent{Action: ActionNoAction}, errors.New("cannot size position without price")
	}
	if g.Limits.Capital <= 0 || g.Limits.MaxPositionValuePercent <= 0 {
		return OrderIntent{Action: ActionNoAction}, errors.New("position sizing limits are not configured")
	}
	quantity := int((g.Limits.Capital * g.Limits.MaxPositionValuePercent / 100) / price)
	if quantity < 1 {
		return OrderIntent{Action: ActionNoAction}, errors.New("position sizing produces less than one share")
	}
	stopLoss := 0.0
	if g.Limits.StopLossPercent > 0 {
		stopLoss = price * (1 - g.Limits.StopLossPercent/100)
	}
	return OrderIntent{
		Action: ActionBuy, Exchange: snapshot.Instrument.Exchange, Symbol: snapshot.Instrument.TradingSymbol,
		Side: "buy", Quantity: quantity, Price: price, StopLoss: stopLoss, Tag: "paper-trader-entry",
	}, nil
}
