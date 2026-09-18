package papertrader

import (
	"context"
	"errors"
	"sync"
)

type DecisionProvider interface {
	Evaluate(context.Context, Snapshot) (Evaluation, error)
}

type OrderSink interface {
	Submit(context.Context, OrderIntent) error
}

type PaperOrderSink struct {
	mu      sync.Mutex
	Intents []OrderIntent
}

func (p *PaperOrderSink) Submit(_ context.Context, intent OrderIntent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Intents = append(p.Intents, intent)
	return nil
}

// Runner serializes one-minute decisions. Serial processing prevents an old
// TypeSafe response from overtaking a newer candle.
type Runner struct {
	Provider   DecisionProvider
	Risk       RiskGate
	Orders     OrderSink
	OnDecision func(Evaluation, *OrderIntent, error)
	OnError    func(error)

	mu             sync.Mutex
	lastSnapshotID string
}

func (r *Runner) Process(ctx context.Context, snapshot Snapshot) (Evaluation, *OrderIntent, error) {
	finish := func(e Evaluation, i *OrderIntent, err error) (Evaluation, *OrderIntent, error) {
		if r.OnDecision != nil {
			r.OnDecision(e, i, err)
		}
		return e, i, err
	}
	if r.Provider == nil {
		return finish(Evaluation{}, nil, errors.New("decision provider is required"))
	}
	if err := snapshot.Validate(); err != nil {
		return finish(Evaluation{}, nil, err)
	}
	r.mu.Lock()
	if snapshot.SnapshotID == r.lastSnapshotID {
		r.mu.Unlock()
		return finish(Evaluation{}, nil, errors.New("duplicate snapshot"))
	}
	r.lastSnapshotID = snapshot.SnapshotID
	r.mu.Unlock()

	evaluation, err := r.Provider.Evaluate(ctx, snapshot)
	if err != nil {
		// Preserve provider latency and the observation that failed so the audit
		// log can distinguish a fast validation failure from a slow timeout.
		if evaluation.SnapshotID == "" {
			evaluation.SnapshotID = snapshot.SnapshotID
		}
		if evaluation.AsOf.IsZero() {
			evaluation.AsOf = snapshot.Session.AsOf
			if evaluation.AsOf.IsZero() {
				evaluation.AsOf = snapshot.CurrentBar.Timestamp
			}
		}
		evaluation.Price = snapshot.Quote.LastPrice
		evaluation.Quote = snapshot.Quote
		evaluation.CurrentBar = snapshot.CurrentBar
		evaluation.StateBytes = snapshot.StateBytes
		evaluation.HistoryOneMinuteBars = len(snapshot.History.OneMinute)
		evaluation.HistoryFiveMinuteBars = len(snapshot.History.FiveMinute)
		evaluation.HistoryDailyBars = len(snapshot.History.Daily)
		return finish(evaluation, nil, err)
	}
	if evaluation.SnapshotID == "" {
		evaluation.SnapshotID = snapshot.SnapshotID
	}
	if evaluation.AsOf.IsZero() {
		evaluation.AsOf = snapshot.Session.AsOf
		if evaluation.AsOf.IsZero() {
			evaluation.AsOf = snapshot.CurrentBar.Timestamp
		}
	}
	evaluation.Price = snapshot.Quote.LastPrice
	evaluation.Quote = snapshot.Quote
	evaluation.CurrentBar = snapshot.CurrentBar
	evaluation.StateBytes = snapshot.StateBytes
	evaluation.HistoryOneMinuteBars = len(snapshot.History.OneMinute)
	evaluation.HistoryFiveMinuteBars = len(snapshot.History.FiveMinute)
	evaluation.HistoryDailyBars = len(snapshot.History.Daily)
	intent, err := r.Risk.Authorize(snapshot, evaluation)
	if err != nil {
		return finish(evaluation, nil, err)
	}
	if intent.Action == ActionNoAction {
		return finish(evaluation, &intent, nil)
	}
	if r.Orders != nil {
		if err := r.Orders.Submit(ctx, intent); err != nil {
			return finish(evaluation, &intent, err)
		}
	}
	return finish(evaluation, &intent, nil)
}

// Run consumes snapshots at the caller's chosen one-minute cadence. The
// channel should contain completed candles only.
func (r *Runner) Run(ctx context.Context, snapshots <-chan Snapshot) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case snapshot, ok := <-snapshots:
			if !ok {
				return nil
			}
			if _, _, err := r.Process(ctx, snapshot); err != nil {
				if r.OnError != nil {
					r.OnError(err)
				}
				// A rejected trade or failed AI request must not stop the market
				// data loop.
				continue
			}
		}
	}
}
