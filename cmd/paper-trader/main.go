package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	papertrader "ai-paper-trader"
	"ai-paper-trader/internal/broker"
	"ai-paper-trader/internal/broker/alpaca"
	"ai-paper-trader/internal/broker/kite"
	"ai-paper-trader/internal/config"
	"ai-paper-trader/pkg/logger"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// This command is deliberately paper-first. It wires one configured market
// data provider to TypeSafe, but never places a live order.
func main() {
	capital := flag.Float64("capital", 10000, "paper account capital in the provider's currency")
	paper := flag.Bool("paper", true, "keep paper mode enabled")
	market := flag.String("market", papertrader.SupportedMarketUS, "market: india or us")
	exchange := flag.String("exchange", "NASDAQ", "cash-equity exchange or venue")
	symbol := flag.String("symbol", papertrader.DefaultTradingSymbol, "cash-equity trading symbol")
	logDir := flag.String("log-dir", "runs", "directory for JSONL run logs and summaries")
	history1m := flag.Int("history-1m", 1000, "paper-mode one-minute history bars sent to TypeSafe")
	history5m := flag.Int("history-5m", 500, "paper-mode five-minute history bars sent to TypeSafe")
	history1d := flag.Int("history-1d", 180, "paper-mode daily history bars sent to TypeSafe")
	maxHistoryBytes := flag.Int("max-history-bytes", 32000, "maximum serialized historical state bytes per TypeSafe request")
	typeSafeModel := flag.String("typesafe-model", "jev-latest", "TypeSafe model")
	typeSafeTimeout := flag.Duration("typesafe-timeout", 8*time.Second, "TypeSafe request timeout")
	typeSafeRetries := flag.Int("typesafe-retries", 1, "TypeSafe retry count for rate limits")
	maxPositionValuePercent := flag.Float64("max-position-value-percent", 25, "maximum position value as a percent of capital")
	maxDailyLossPercent := flag.Float64("max-daily-loss-percent", 1, "maximum daily loss as a percent of capital")
	maxTradesPerDay := flag.Int("max-trades-per-day", 5, "maximum paper entries per day")
	stopLossPercent := flag.Float64("stop-loss-percent", 1, "protective stop-loss percent")
	maxSpreadPercent := flag.Float64("max-spread-percent", 0.15, "maximum allowed bid/ask spread percent")
	minActionConfidence := flag.Float64("min-action-confidence", 0.60, "minimum TypeSafe action confidence")
	minBuySupport := flag.Float64("min-buy-support", 0.60, "minimum TypeSafe buy support")
	flag.Parse()
	if !*paper {
		fmt.Fprintln(os.Stderr, "live order execution is disabled; use -paper=true")
		os.Exit(2)
	}
	selectedMarket := strings.ToLower(strings.TrimSpace(*market))
	if selectedMarket != papertrader.SupportedMarketIndia && selectedMarket != papertrader.SupportedMarketUS {
		fmt.Fprintln(os.Stderr, "market must be india or us")
		os.Exit(2)
	}
	if strings.TrimSpace(*exchange) == "" || strings.TrimSpace(*symbol) == "" {
		fmt.Fprintln(os.Stderr, "exchange/venue and symbol are required")
		os.Exit(2)
	}

	kiteAPIKey := os.Getenv("KITE_API_KEY")
	kiteAPISecret := os.Getenv("KITE_API_SECRET")
	kiteAccessToken := os.Getenv("KITE_ACCESS_TOKEN")
	alpacaAPIKey := os.Getenv("ALPACA_API_KEY")
	alpacaAPISecret := os.Getenv("ALPACA_API_SECRET")
	typeSafeKey := os.Getenv("TYPESAFE_API_KEY")
	if typeSafeKey == "" {
		fmt.Fprintln(os.Stderr, "set TYPESAFE_API_KEY")
		os.Exit(2)
	}
	if selectedMarket == papertrader.SupportedMarketIndia && (kiteAPIKey == "" || kiteAPISecret == "" || kiteAccessToken == "") {
		fmt.Fprintln(os.Stderr, "India market requires KITE_API_KEY, KITE_API_SECRET, and KITE_ACCESS_TOKEN")
		os.Exit(2)
	}
	if selectedMarket == papertrader.SupportedMarketUS && (alpacaAPIKey == "" || alpacaAPISecret == "") {
		fmt.Fprintln(os.Stderr, "US market requires ALPACA_API_KEY and ALPACA_API_SECRET")
		os.Exit(2)
	}

	log, err := logger.New("info", "console", "stdout")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()
	log.Infow("credential diagnostics",
		"market", selectedMarket,
		"kite_api_key_set", kiteAPIKey != "", "kite_api_key_len", len(kiteAPIKey), "kite_api_key_sha256_12", credentialFingerprint(kiteAPIKey),
		"kite_api_secret_set", kiteAPISecret != "", "kite_api_secret_len", len(kiteAPISecret), "kite_api_secret_sha256_12", credentialFingerprint(kiteAPISecret),
		"kite_access_token_set", kiteAccessToken != "", "kite_access_token_len", len(kiteAccessToken), "kite_access_token_sha256_12", credentialFingerprint(kiteAccessToken),
		"alpaca_api_key_set", alpacaAPIKey != "", "alpaca_api_key_len", len(alpacaAPIKey), "alpaca_api_key_sha256_12", credentialFingerprint(alpacaAPIKey),
		"alpaca_api_secret_set", alpacaAPISecret != "", "alpaca_api_secret_len", len(alpacaAPISecret), "alpaca_api_secret_sha256_12", credentialFingerprint(alpacaAPISecret),
		"typesafe_api_key_set", typeSafeKey != "", "typesafe_api_key_len", len(typeSafeKey), "typesafe_api_key_sha256_12", credentialFingerprint(typeSafeKey),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var instrument broker.Instrument
	var ticks <-chan broker.Tick
	var historical papertrader.HistoricalProvider
	var disconnect func()
	if selectedMarket == papertrader.SupportedMarketIndia {
		tickerConfig := config.TickerConfig{AutoReconnect: true, MaxReconnectDelay: 30 * time.Second, ReconnectMaxRetries: 10}
		indiaClient := kite.NewClient(kiteAPIKey, kiteAPISecret, log, false, tickerConfig)
		indiaClient.SetAccessToken(kiteAccessToken)
		if err := indiaClient.Connect(ctx); err != nil {
			log.Fatalw("connect India market data", "error", err)
		}
		instrument, err = papertrader.ResolveEquity(ctx, indiaClient, *exchange, *symbol)
		if err != nil {
			log.Fatalw("resolve India equity", "exchange", *exchange, "symbol", *symbol, "error", err)
		}
		rawKite := kiteconnect.New(kiteAPIKey)
		rawKite.SetAccessToken(kiteAccessToken)
		if _, err := rawKite.GetUserProfile(); err != nil {
			if strings.Contains(err.Error(), "Incorrect `api_key` or `access_token`") {
				log.Fatalw("validate India credentials", "error", err, "hint", "Kite access tokens are API-key-specific and expire daily; generate a fresh token for the same API key")
			}
			log.Fatalw("validate India credentials", "error", err)
		}
		historical = papertrader.KiteHistoricalProvider{Client: rawKite}
		ticks, err = indiaClient.SubscribeTicks(ctx, []uint32{instrument.InstrumentToken})
		if err != nil {
			log.Fatalw("subscribe India equity ticks", "symbol", instrument.TradingSymbol, "error", err)
		}
		disconnect = func() { _ = indiaClient.Disconnect() }
	} else {
		usClient := alpaca.NewClientFromEnv(alpacaAPIKey, alpacaAPISecret)
		if err := usClient.Connect(ctx); err != nil {
			log.Fatalw("connect US market data", "error", err)
		}
		instrument, err = usClient.ResolveEquity(ctx, *exchange, *symbol)
		if err != nil {
			log.Fatalw("resolve US equity", "exchange", *exchange, "symbol", *symbol, "error", err)
		}
		historical = alpacaHistoricalProvider{provider: alpaca.HistoricalProvider{Client: usClient}}
		ticks, err = usClient.SubscribeTicks(ctx, []uint32{instrument.InstrumentToken})
		if err != nil {
			log.Fatalw("subscribe US equity ticks", "symbol", instrument.TradingSymbol, "error", err)
		}
		disconnect = func() { _ = usClient.Disconnect() }
	}
	defer disconnect()

	if *history1m < 0 || *history5m < 0 || *history1d < 0 {
		log.Fatal("history windows cannot be negative")
	}
	history := papertrader.NewHistoryStore(*history1m, *history5m, *history1d)
	now := time.Now()
	if err := seedHistory(ctx, historical, history, instrument.InstrumentToken, now, *history1m, *history5m, *history1d); err != nil {
		if selectedMarket == papertrader.SupportedMarketIndia && strings.Contains(err.Error(), "Incorrect `api_key` or `access_token`") {
			log.Fatalw("seed India historical data", "error", err, "hint", "Kite access tokens are API-key-specific and expire daily; generate a fresh token for the same API key")
		}
		log.Fatalw("seed historical data", "error", err)
	}

	book := newPaperBook(*capital)
	if err := os.MkdirAll(*logDir, 0755); err != nil {
		log.Fatalw("create log directory", "error", err)
	}
	runID := fmt.Sprintf("paper-%s", time.Now().UTC().Format("20060102T150405.000000000Z"))
	eventFile, err := os.Create(filepath.Join(*logDir, runID+"-events.jsonl"))
	if err != nil {
		log.Fatalw("create paper event log", "error", err)
	}
	eventEncoder := json.NewEncoder(eventFile)
	marketFile, err := os.Create(filepath.Join(*logDir, runID+"-market.jsonl"))
	if err != nil {
		log.Fatalw("create paper market log", "error", err)
	}
	marketWriter := bufio.NewWriterSize(marketFile, 64*1024)
	marketEncoder := json.NewEncoder(marketWriter)
	defer func() {
		_ = eventFile.Close()
		_ = marketWriter.Flush()
		_ = marketFile.Close()
		position, account := book.Snapshot()
		file, summaryErr := os.Create(filepath.Join(*logDir, runID+"-summary.json"))
		if summaryErr == nil {
			_ = json.NewEncoder(file).Encode(map[string]interface{}{
				"run_id": runID, "mode": "paper", "market": selectedMarket, "exchange": instrument.Exchange, "symbol": instrument.TradingSymbol,
				"position": position, "account": account,
			})
			_ = file.Close()
		}
	}()
	typeSafe := papertrader.NewTypeSafeClient(typeSafeKey)
	typeSafe.Model = *typeSafeModel
	typeSafe.RequestTimeout = *typeSafeTimeout
	typeSafe.MaxRetries = *typeSafeRetries
	decisionConfig := papertrader.DecisionConfig{
		Mode: "paper", PaperOnly: true, EvaluationCadence: "one completed 1m candle",
		Model: *typeSafeModel, HistoryOneMinuteLimit: *history1m, HistoryFiveMinuteLimit: *history5m,
		HistoryDailyLimit: *history1d, MaxHistoryBytes: *maxHistoryBytes,
		MaxPositionValuePercent: *maxPositionValuePercent, MaxDailyLossPercent: *maxDailyLossPercent,
		MaxTradesPerDay: *maxTradesPerDay, StopLossPercent: *stopLossPercent, MaxSpreadPercent: *maxSpreadPercent,
		MinActionConfidence: *minActionConfidence, MinBuySupport: *minBuySupport, AllowShort: false,
	}
	runner := &papertrader.Runner{
		Provider: typeSafe,
		Risk: papertrader.RiskGate{Limits: papertrader.RiskLimits{
			Capital: *capital, MaxPositionValuePercent: *maxPositionValuePercent, MaxDailyLossPercent: *maxDailyLossPercent,
			MaxTradesPerDay: *maxTradesPerDay, StopLossPercent: *stopLossPercent, MaxSpreadPercent: *maxSpreadPercent,
			MaxDataAge: 5 * time.Second, MinActionConfidence: *minActionConfidence, MinBuySupport: *minBuySupport,
		}},
		Orders: book,
		OnDecision: func(e papertrader.Evaluation, intent *papertrader.OrderIntent, err error) {
			position, account := book.Snapshot()
			log.Infow("paper AI decision", "symbol", instrument.TradingSymbol, "snapshot_id", e.SnapshotID, "action", e.Action, "action_confidence", e.ActionConfidence, "buy_support", e.BuySupport, "regime", e.MarketRegime, "intent", intent, "position", position.Side, "quantity", position.Quantity, "daily_pnl", account.DailyPnL, "trades_today", account.TradesToday, "error", err)
			timestamp := e.AsOf
			if timestamp.IsZero() {
				timestamp = time.Now().UTC()
			}
			cost, costKnown := papertrader.EstimateTypeSafeCostUSD(e.Usage)
			decisionEvent := papertrader.DecisionEvent{Timestamp: timestamp, SnapshotID: e.SnapshotID, Action: e.Action, ActionConfidence: e.ActionConfidence, BuySupport: e.BuySupport, MarketRegime: e.MarketRegime, LatencyMilliseconds: float64(e.Latency.Microseconds()) / 1000, InputTokens: e.Usage.InputTokens, OutputTokens: e.Usage.OutputTokens, PositionSide: position.Side, PositionQuantity: position.Quantity, DailyPnL: account.DailyPnL, TradesToday: account.TradesToday, Price: e.Price, Quote: e.Quote, CurrentBar: e.CurrentBar, StateBytes: e.StateBytes, HistoryOneMinuteBars: e.HistoryOneMinuteBars, HistoryFiveMinuteBars: e.HistoryFiveMinuteBars, HistoryDailyBars: e.HistoryDailyBars, Intent: intent, Error: errorString(err)}
			if costKnown {
				decisionEvent.AICostUSD = &cost
			}
			_ = eventEncoder.Encode(decisionEvent)
		},
		OnError: func(err error) {
			log.Warnw("paper minute evaluation rejected", "symbol", instrument.TradingSymbol, "error", err)
		},
	}

	runtime := &papertrader.MinuteRuntime{
		CandleBuilder: papertrader.NewMinuteCandleBuilder(),
		History:       history,
		Runner:        runner,
		Snapshots: papertrader.SnapshotBuilder{
			Instrument: papertrader.InstrumentState{Market: selectedMarket, Exchange: instrument.Exchange, TradingSymbol: instrument.TradingSymbol, InstrumentToken: instrument.InstrumentToken},
			History:    history,
			// Keep the requested windows high, but cap serialized state so the
			// TypeSafe request remains below its token budget.
			MaxHistoryBytes: *maxHistoryBytes,
			Config:          decisionConfig,
			Position:        book.Position,
			Account:         book.Account,
			Session:         func(ts time.Time) papertrader.SessionState { return sessionFor(selectedMarket, ts) },
		},
		OnTick: func(t papertrader.Tick) {
			if stop := book.CheckStop(t.LastPrice); stop != nil {
				log.Warnw("paper stop-loss", "symbol", instrument.TradingSymbol, "price", t.LastPrice, "daily_pnl", stop.DailyPnL)
			}
		},
	}

	log.Infow("paper AI runner started", "exchange", instrument.Exchange, "symbol", instrument.TradingSymbol, "token", instrument.InstrumentToken)
	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-ticks:
			if !ok {
				return
			}
			observed := papertrader.TickFromBroker(tick)
			if _, err := runtime.HandleTick(ctx, observed); err != nil {
				log.Warnw("process market tick", "symbol", instrument.TradingSymbol, "error", err)
			}
			position, account := book.Snapshot()
			if err := marketEncoder.Encode(papertrader.MarketEvent{Timestamp: observed.Timestamp, Tick: observed, Position: position, Account: account}); err != nil {
				log.Warnw("write market tick", "symbol", instrument.TradingSymbol, "error", err)
			} else if err := marketWriter.Flush(); err != nil {
				log.Warnw("flush market tick", "symbol", instrument.TradingSymbol, "error", err)
			}
		}
	}
}

// credentialFingerprint is a correlation aid only; it never returns a
// credential or a reversible encoding of one.
func credentialFingerprint(value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])[:12]
}

// alpacaHistoricalProvider converts the internal US adapter's small bar type
// into the root package's broker-neutral candle type without an import cycle.
type alpacaHistoricalProvider struct{ provider alpaca.HistoricalProvider }

func (p alpacaHistoricalProvider) Load(ctx context.Context, token uint32, interval string, from, to time.Time) ([]papertrader.Bar, error) {
	bars, err := p.provider.Load(ctx, token, interval, from, to)
	if err != nil {
		return nil, err
	}
	result := make([]papertrader.Bar, 0, len(bars))
	for _, bar := range bars {
		result = append(result, papertrader.Bar{Timestamp: bar.Timestamp, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume})
	}
	return result, nil
}

func seedHistory(ctx context.Context, provider papertrader.HistoricalProvider, store *papertrader.HistoryStore, token uint32, now time.Time, oneMinuteBars, fiveMinuteBars, dailyBars int) error {
	oneMinuteDays := 5
	if days := (oneMinuteBars+374)/375 + 2; days > oneMinuteDays {
		oneMinuteDays = days
	}
	fiveMinuteDays := 30
	if days := (fiveMinuteBars+74)/75 + 5; days > fiveMinuteDays {
		fiveMinuteDays = days
	}
	dailyDays := dailyBars + 30
	oneMinute, err := provider.Load(ctx, token, "minute", now.AddDate(0, 0, -oneMinuteDays), now)
	if err != nil {
		return err
	}
	fiveMinute, err := provider.Load(ctx, token, "5minute", now.AddDate(0, 0, -fiveMinuteDays), now)
	if err != nil {
		return err
	}
	daily, err := provider.Load(ctx, token, "day", now.AddDate(0, 0, -dailyDays), now)
	if err != nil {
		return err
	}
	store.Seed(oneMinute, fiveMinute, daily)
	return nil
}

func sessionFor(market string, ts time.Time) papertrader.SessionState {
	zone := "Asia/Kolkata"
	openHour, openMinute := 9, 15
	squareHour, squareMinute := 15, 15
	closeHour, closeMinute := 15, 30
	if market == papertrader.SupportedMarketUS {
		zone = "America/New_York"
		openHour, openMinute = 9, 30
		squareHour, squareMinute = 15, 55
		closeHour, closeMinute = 16, 0
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	local := ts.In(loc)
	open := time.Date(local.Year(), local.Month(), local.Day(), openHour, openMinute, 0, 0, loc)
	squareOff := time.Date(local.Year(), local.Month(), local.Day(), squareHour, squareMinute, 0, 0, loc)
	close := time.Date(local.Year(), local.Month(), local.Day(), closeHour, closeMinute, 0, 0, loc)
	phase := "closed"
	if !local.Before(open) && local.Before(squareOff) {
		phase = "open"
	} else if !local.Before(squareOff) && local.Before(close) {
		phase = "square_off"
	}
	minutes := 0
	if local.After(open) {
		minutes = int(local.Sub(open) / time.Minute)
	}
	return papertrader.SessionState{AsOf: ts, MarketOpen: open, SquareOffAt: squareOff, MarketClose: close, MarketPhase: phase, MinutesOpened: minutes}
}

type paperBook struct {
	mu       sync.Mutex
	capital  float64
	position papertrader.PositionState
	account  papertrader.AccountState
}

func newPaperBook(capital float64) *paperBook {
	return &paperBook{capital: capital, position: papertrader.PositionState{Side: "flat"}, account: papertrader.AccountState{Capital: capital, AvailableCash: capital}}
}

func (p *paperBook) Position(context.Context) (papertrader.PositionState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position, nil
}

func (p *paperBook) Account(context.Context) (papertrader.AccountState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.account, nil
}

func (p *paperBook) Snapshot() (papertrader.PositionState, papertrader.AccountState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position, p.account
}

func (p *paperBook) Submit(_ context.Context, intent papertrader.OrderIntent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch intent.Action {
	case papertrader.ActionBuy:
		p.position = papertrader.PositionState{Side: "long", Quantity: intent.Quantity, EntryPrice: intent.Price, StopLoss: intent.StopLoss}
		p.account.TradesToday++
	case papertrader.ActionExit:
		if p.position.Side == "long" {
			p.account.DailyPnL += (intent.Price - p.position.EntryPrice) * float64(p.position.Quantity)
		}
		p.position = papertrader.PositionState{Side: "flat"}
	}
	return nil
}

type paperStopResult struct {
	DailyPnL float64
}

func (p *paperBook) CheckStop(price float64) *paperStopResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.position.Side == "long" && price > 0 {
		p.position.Unrealized = (price - p.position.EntryPrice) * float64(p.position.Quantity)
	}
	if p.position.Side != "long" || p.position.StopLoss <= 0 || price > p.position.StopLoss {
		return nil
	}
	p.account.DailyPnL += (price - p.position.EntryPrice) * float64(p.position.Quantity)
	p.position = papertrader.PositionState{Side: "flat"}
	return &paperStopResult{DailyPnL: p.account.DailyPnL}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
