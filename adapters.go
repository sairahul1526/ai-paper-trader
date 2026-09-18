package papertrader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ai-paper-trader/internal/broker"
	kitconnect "github.com/zerodha/gokiteconnect/v4"
)

// ResolveEquity finds a cash-equity instrument from the broker's instrument dump.
// The token is never hardcoded because broker instrument tokens can change.
func ResolveEquity(ctx context.Context, b broker.Broker, exchange, symbol string) (broker.Instrument, error) {
	instruments, err := b.GetInstruments(ctx, exchange)
	if err != nil {
		return broker.Instrument{}, fmt.Errorf("load NSE instruments: %w", err)
	}
	for _, instrument := range instruments {
		if strings.EqualFold(instrument.Exchange, exchange) &&
			strings.EqualFold(instrument.TradingSymbol, symbol) &&
			strings.EqualFold(instrument.InstrumentType, "EQ") {
			return instrument, nil
		}
	}
	return broker.Instrument{}, fmt.Errorf("%s:%s equity instrument not found", exchange, symbol)
}

func TickFromBroker(t broker.Tick) Tick {
	result := Tick{
		InstrumentToken: t.InstrumentToken,
		Timestamp:       t.Timestamp,
		LastPrice:       t.LastPrice,
		LastQuantity:    t.LastQuantity,
		Volume:          t.Volume,
		BuyQuantity:     t.BuyQuantity,
		SellQuantity:    t.SellQuantity,
	}
	if len(t.Depth.Buy) > 0 {
		result.Bid = t.Depth.Buy[0].Price
		result.BuyDepth = make([]DepthLevel, len(t.Depth.Buy))
		for i, level := range t.Depth.Buy {
			result.BuyDepth[i] = DepthLevel{Price: level.Price, Quantity: level.Quantity, Orders: level.Orders}
		}
	}
	if len(t.Depth.Sell) > 0 {
		result.Ask = t.Depth.Sell[0].Price
		result.SellDepth = make([]DepthLevel, len(t.Depth.Sell))
		for i, level := range t.Depth.Sell {
			result.SellDepth[i] = DepthLevel{Price: level.Price, Quantity: level.Quantity, Orders: level.Orders}
		}
	}
	return result
}

// KiteHistoricalProvider is kept in this new module so the old broker
// interface does not need to change yet.
type KiteHistoricalProvider struct {
	Client *kitconnect.Client
}

func (p KiteHistoricalProvider) Load(ctx context.Context, token uint32, interval string, from, to time.Time) ([]Bar, error) {
	if p.Client == nil {
		return nil, fmt.Errorf("kite client is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := p.Client.GetHistoricalData(int(token), interval, from, to, false, false)
	if err != nil {
		return nil, fmt.Errorf("get %s historical data: %w", interval, err)
	}
	result := make([]Bar, 0, len(data))
	for _, candle := range data {
		result = append(result, Bar{Timestamp: candle.Date.Time, Open: candle.Open, High: candle.High, Low: candle.Low, Close: candle.Close, Volume: int64(candle.Volume), OI: int64(candle.OI)})
	}
	return result, nil
}
