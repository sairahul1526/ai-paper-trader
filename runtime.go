package papertrader

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type PositionProvider func(context.Context) (PositionState, error)
type AccountProvider func(context.Context) (AccountState, error)
type SessionProvider func(time.Time) SessionState

// SnapshotBuilder assembles broker observations and bounded local history into
// the state sent to TypeSafe. It contains no entry strategy.
type SnapshotBuilder struct {
	Instrument      InstrumentState
	History         *HistoryStore
	MaxHistoryBytes int
	Config          DecisionConfig
	Position        PositionProvider
	Account         AccountProvider
	Session         SessionProvider
}

func (b SnapshotBuilder) Build(ctx context.Context, bar Bar, tick Tick) (Snapshot, error) {
	if b.History == nil {
		return Snapshot{}, fmt.Errorf("history store is required")
	}
	if !bar.Valid() {
		return Snapshot{}, fmt.Errorf("completed bar is invalid")
	}
	if tick.LastPrice <= 0 {
		return Snapshot{}, fmt.Errorf("tick last_price must be positive")
	}

	position := PositionState{Side: "flat"}
	if b.Position != nil {
		var err error
		position, err = b.Position(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("position provider: %w", err)
		}
	}
	account := AccountState{}
	if b.Account != nil {
		var err error
		account, err = b.Account(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("account provider: %w", err)
		}
	}
	session := SessionState{AsOf: tick.Timestamp}
	if b.Session != nil {
		session = b.Session(tick.Timestamp)
		if session.AsOf.IsZero() {
			session.AsOf = tick.Timestamp
		}
	}

	historical := b.History.Snapshot()
	if b.MaxHistoryBytes > 0 {
		historical = TrimHistoricalContext(historical, b.MaxHistoryBytes)
	}
	snapshot := Snapshot{
		SnapshotID: fmt.Sprintf("%s-%d", b.Instrument.TradingSymbol, bar.Timestamp.UnixNano()),
		Instrument: b.Instrument,
		Quote: Quote{
			LastPrice: tick.LastPrice, Bid: tick.Bid, Ask: tick.Ask,
			BuyQuantity: tick.BuyQuantity, SellQuantity: tick.SellQuantity,
			Timestamp: tick.Timestamp, BuyDepth: append([]DepthLevel(nil), tick.BuyDepth...), SellDepth: append([]DepthLevel(nil), tick.SellDepth...),
		},
		CurrentBar: bar,
		History:    historical,
		Session:    session,
		Position:   position,
		Account:    account,
		Config:     b.Config,
	}
	if encoded, err := json.Marshal(snapshot); err == nil {
		snapshot.StateBytes = len(encoded)
	}
	return snapshot, snapshot.Validate()
}

// MinuteRuntime is the broker-to-TypeSafe bridge. It emits one decision only
// when a new completed minute is observed.
type MinuteRuntime struct {
	CandleBuilder *MinuteCandleBuilder
	History       *HistoryStore
	Snapshots     SnapshotBuilder
	Runner        *Runner
	OnTick        func(Tick)
	OnSnapshot    func(Snapshot)
}

func (r *MinuteRuntime) HandleTick(ctx context.Context, tick Tick) (bool, error) {
	if r.OnTick != nil {
		r.OnTick(tick)
	}
	if r.CandleBuilder == nil || r.History == nil || r.Runner == nil {
		return false, fmt.Errorf("candle builder, history, and runner are required")
	}
	completed, err := r.CandleBuilder.Update(tick)
	if err != nil {
		return false, err
	}
	if completed == nil {
		return false, nil
	}
	r.History.AddOneMinute(*completed)
	snapshot, err := r.Snapshots.Build(ctx, *completed, tick)
	if err != nil {
		return false, err
	}
	if r.OnSnapshot != nil {
		r.OnSnapshot(snapshot)
	}
	_, _, err = r.Runner.Process(ctx, snapshot)
	return true, err
}

func (r *MinuteRuntime) Run(ctx context.Context, ticks <-chan Tick) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tick, ok := <-ticks:
			if !ok {
				return nil
			}
			_, _ = r.HandleTick(ctx, tick)
		}
	}
}
