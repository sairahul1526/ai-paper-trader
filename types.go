// Package papertrader contains the broker-neutral AI-first equity paper-trading
// workflow shared by every supported market-data provider.
package papertrader

import (
	"errors"
	"fmt"
	"time"
)

const (
	// SupportedMarketIndia and SupportedMarketUS are the dashboard/runner
	// market selectors. Providers may expose additional venues within a market.
	SupportedMarketIndia = "india"
	SupportedMarketUS    = "us"
	DefaultMarket        = SupportedMarketIndia
	DefaultExchangeIndia = "NSE"
	DefaultTradingSymbol = "AAPL"
	ExchangeNSE          = DefaultExchangeIndia // compatibility alias for callers using the India default

	ActionBuy      Action = "buy"
	ActionHold     Action = "hold"
	ActionExit     Action = "exit"
	ActionNoAction Action = "no_action"
	ActionShort    Action = "sell_short"
)

// Action is the typed action returned by TypeSafe. The local risk gate decides
// whether an action is executable.
type Action string

// Bar is a completed OHLCV candle. Volume is the volume traded during the bar,
// not Kite's cumulative session volume.
type Bar struct {
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    int64     `json:"volume"`
	OI        int64     `json:"open_interest,omitempty"`
}

func (b Bar) Valid() bool {
	return !b.Timestamp.IsZero() && b.Open > 0 && b.High >= b.Low && b.Low > 0 && b.Close > 0
}

// DepthLevel is a single market-depth level.
type DepthLevel struct {
	Price    float64 `json:"price"`
	Quantity int64   `json:"quantity"`
	Orders   int     `json:"orders,omitempty"`
}

// Tick is the small broker-neutral tick shape used by the new runtime.
type Tick struct {
	InstrumentToken uint32       `json:"instrument_token"`
	Timestamp       time.Time    `json:"timestamp"`
	LastPrice       float64      `json:"last_price"`
	LastQuantity    int64        `json:"last_quantity"`
	Volume          int64        `json:"volume"`
	BuyQuantity     int64        `json:"buy_quantity,omitempty"`
	SellQuantity    int64        `json:"sell_quantity,omitempty"`
	Bid             float64      `json:"bid,omitempty"`
	Ask             float64      `json:"ask,omitempty"`
	BuyDepth        []DepthLevel `json:"buy_depth,omitempty"`
	SellDepth       []DepthLevel `json:"sell_depth,omitempty"`
}

// MarketEvent is written for the dashboard on every broker tick. It is kept
// separate from DecisionEvent because TypeSafe decisions intentionally stay
// at the completed-one-minute cadence.
type MarketEvent struct {
	Timestamp time.Time     `json:"timestamp"`
	Tick      Tick          `json:"tick"`
	Position  PositionState `json:"position"`
	Account   AccountState  `json:"account"`
}

// DecisionEvent is the paper audit record emitted once per completed minute.
// It contains the typed TypeSafe result, measured request telemetry, and the
// local risk-gate outcome.
type DecisionEvent struct {
	Timestamp             time.Time    `json:"timestamp"`
	SnapshotID            string       `json:"snapshot_id"`
	Action                Action       `json:"action"`
	ActionConfidence      float64      `json:"action_confidence"`
	BuySupport            float64      `json:"buy_support"`
	MarketRegime          string       `json:"market_regime"`
	LatencyMilliseconds   float64      `json:"latency_ms"`
	InputTokens           int          `json:"input_tokens,omitempty"`
	OutputTokens          int          `json:"output_tokens,omitempty"`
	AICostUSD             *float64     `json:"ai_cost_usd,omitempty"`
	PositionSide          string       `json:"position_side,omitempty"`
	PositionQuantity      int          `json:"position_quantity,omitempty"`
	DailyPnL              float64      `json:"daily_pnl"`
	TradesToday           int          `json:"trades_today"`
	Price                 float64      `json:"price,omitempty"`
	Quote                 Quote        `json:"quote,omitempty"`
	CurrentBar            Bar          `json:"current_bar,omitempty"`
	StateBytes            int          `json:"state_bytes,omitempty"`
	HistoryOneMinuteBars  int          `json:"history_1m_bars,omitempty"`
	HistoryFiveMinuteBars int          `json:"history_5m_bars,omitempty"`
	HistoryDailyBars      int          `json:"history_1d_bars,omitempty"`
	Intent                *OrderIntent `json:"intent,omitempty"`
	Error                 string       `json:"error,omitempty"`
}

// Quote is the most recent quote accompanying a snapshot.
type Quote struct {
	LastPrice    float64      `json:"last_price"`
	Bid          float64      `json:"bid,omitempty"`
	Ask          float64      `json:"ask,omitempty"`
	BuyQuantity  int64        `json:"buy_quantity,omitempty"`
	SellQuantity int64        `json:"sell_quantity,omitempty"`
	Timestamp    time.Time    `json:"timestamp"`
	BuyDepth     []DepthLevel `json:"buy_depth,omitempty"`
	SellDepth    []DepthLevel `json:"sell_depth,omitempty"`
}

// HistoricalContext is bounded intentionally. Older data is retained locally
// for replay, while only these windows are sent on each TypeSafe request.
type HistoricalContext struct {
	OneMinute  []Bar `json:"1m,omitempty"`
	FiveMinute []Bar `json:"5m,omitempty"`
	Daily      []Bar `json:"1d,omitempty"`
}

type InstrumentState struct {
	Market          string `json:"market,omitempty"`
	Exchange        string `json:"exchange"`
	TradingSymbol   string `json:"trading_symbol"`
	InstrumentToken uint32 `json:"instrument_token"`
}

type SessionState struct {
	AsOf          time.Time `json:"as_of"`
	MarketOpen    time.Time `json:"market_open,omitempty"`
	MarketClose   time.Time `json:"market_close,omitempty"`
	SquareOffAt   time.Time `json:"square_off_at,omitempty"`
	MarketPhase   string    `json:"market_phase,omitempty"`
	MinutesOpened int       `json:"minutes_since_open,omitempty"`
}

type PositionState struct {
	Side       string  `json:"side"` // flat, long, short
	Quantity   int     `json:"quantity"`
	EntryPrice float64 `json:"entry_price,omitempty"`
	StopLoss   float64 `json:"stop_loss,omitempty"`
	Unrealized float64 `json:"unrealized_pnl,omitempty"`
}

type AccountState struct {
	Capital       float64 `json:"capital"`
	AvailableCash float64 `json:"available_cash,omitempty"`
	DailyPnL      float64 `json:"daily_pnl"`
	TradesToday   int     `json:"trades_today"`
}

// DecisionConfig is the non-secret runtime context supplied to TypeSafe.
// Local risk checks remain authoritative; this gives the model the same
// operating envelope when it ranks an action.
type DecisionConfig struct {
	Mode                    string  `json:"mode"`
	PaperOnly               bool    `json:"paper_only"`
	EvaluationCadence       string  `json:"evaluation_cadence"`
	Model                   string  `json:"model"`
	HistoryOneMinuteLimit   int     `json:"history_1m_limit"`
	HistoryFiveMinuteLimit  int     `json:"history_5m_limit"`
	HistoryDailyLimit       int     `json:"history_1d_limit"`
	MaxHistoryBytes         int     `json:"max_history_bytes"`
	MaxPositionValuePercent float64 `json:"max_position_value_percent"`
	MaxDailyLossPercent     float64 `json:"max_daily_loss_percent"`
	MaxTradesPerDay         int     `json:"max_trades_per_day"`
	StopLossPercent         float64 `json:"stop_loss_percent"`
	MaxSpreadPercent        float64 `json:"max_spread_percent"`
	MinActionConfidence     float64 `json:"min_action_confidence"`
	MinBuySupport           float64 `json:"min_buy_support"`
	AllowShort              bool    `json:"allow_short"`
}

// Snapshot is the complete state sent to TypeSafe at a one-minute boundary.
// It contains observations only; the model's judgments are returned separately.
type Snapshot struct {
	SnapshotID string            `json:"snapshot_id"`
	Instrument InstrumentState   `json:"instrument"`
	Quote      Quote             `json:"quote"`
	CurrentBar Bar               `json:"current_bar"`
	History    HistoricalContext `json:"history"`
	Session    SessionState      `json:"session"`
	Position   PositionState     `json:"position"`
	Account    AccountState      `json:"account"`
	Config     DecisionConfig    `json:"config"`
	// StateBytes is measured after history trimming and is telemetry for the
	// dashboard; it is intentionally omitted from the TypeSafe payload.
	StateBytes int `json:"-"`
}

func (s Snapshot) Validate() error {
	if s.SnapshotID == "" {
		return errors.New("snapshot_id is required")
	}
	if s.Instrument.Exchange == "" || s.Instrument.TradingSymbol == "" {
		return errors.New("instrument exchange and trading_symbol are required")
	}
	if !s.CurrentBar.Valid() {
		return errors.New("current_bar is invalid")
	}
	if s.Quote.LastPrice <= 0 {
		return errors.New("quote.last_price must be positive")
	}
	return nil
}

// OrderIntent is the only output that may reach an order sink after risk
// authorization. It deliberately contains broker-neutral fields.
type OrderIntent struct {
	Action   Action  `json:"action"`
	Exchange string  `json:"exchange"`
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	StopLoss float64 `json:"stop_loss,omitempty"`
	Tag      string  `json:"tag"`
}

func (i OrderIntent) Validate() error {
	if i.Action == ActionNoAction {
		return nil
	}
	if i.Exchange == "" || i.Symbol == "" || i.Quantity <= 0 || i.Price <= 0 {
		return errors.New("order intent is incomplete")
	}
	if i.Side != "buy" && i.Side != "sell" {
		return fmt.Errorf("unsupported order side %q", i.Side)
	}
	return nil
}
