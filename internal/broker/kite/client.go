package kite

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ai-paper-trader/internal/broker"
	"ai-paper-trader/internal/config"
	"ai-paper-trader/pkg/logger"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
	kiteticker "github.com/zerodha/gokiteconnect/v4/ticker"
)

// TickerHealth tracks the health and statistics of the ticker connection
type TickerHealth struct {
	mu                      sync.RWMutex
	isConnected             bool
	lastTickTime            time.Time
	lastConnectTime         time.Time
	disconnectCount         int64
	reconnectCount          int64
	tickDropCount           int64
	currentReconnectAttempt int
}

// TickerHealthSnapshot is a thread-safe snapshot of ticker health
type TickerHealthSnapshot struct {
	IsConnected             bool
	LastTickTime            time.Time
	TimeSinceLastTick       time.Duration
	DisconnectCount         int64
	ReconnectCount          int64
	TickDropCount           int64
	CurrentReconnectAttempt int
}

// GetSnapshot returns a thread-safe snapshot of current ticker health
func (h *TickerHealth) GetSnapshot() TickerHealthSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return TickerHealthSnapshot{
		IsConnected:             h.isConnected,
		LastTickTime:            h.lastTickTime,
		TimeSinceLastTick:       time.Since(h.lastTickTime),
		DisconnectCount:         h.disconnectCount,
		ReconnectCount:          h.reconnectCount,
		TickDropCount:           h.tickDropCount,
		CurrentReconnectAttempt: h.currentReconnectAttempt,
	}
}

// RecordTick updates the last tick time
func (h *TickerHealth) RecordTick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastTickTime = time.Now()
}

// RecordConnection records a successful connection
func (h *TickerHealth) RecordConnection() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastConnectTime = time.Now()
}

// RecordDisconnect records a disconnection event
func (h *TickerHealth) RecordDisconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnectCount++
}

// RecordReconnect records a successful reconnection
func (h *TickerHealth) RecordReconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reconnectCount++
}

// RecordTickDrop records a dropped tick event
func (h *TickerHealth) RecordTickDrop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tickDropCount++
}

// SetConnected updates the connection state
func (h *TickerHealth) SetConnected(connected bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isConnected = connected
}

// SetReconnectAttempt sets the current reconnect attempt number
func (h *TickerHealth) SetReconnectAttempt(attempt int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.currentReconnectAttempt = attempt
}

// ResetReconnectAttempt resets the reconnect attempt counter
func (h *TickerHealth) ResetReconnectAttempt() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.currentReconnectAttempt = 0
}

// Client implements broker.Broker using Zerodha Kite Connect API
type Client struct {
	mu          sync.RWMutex
	kc          *kiteconnect.Client
	ticker      *kiteticker.Ticker
	apiKey      string
	apiSecret   string
	accessToken string
	connected   bool
	tickChan    chan broker.Tick
	log         *logger.Logger
	debug       bool

	// Health tracking
	health       *TickerHealth
	tickerCfg    config.TickerConfig
	tickerCtx    context.Context
	tickerCancel context.CancelFunc
	tickerWg     sync.WaitGroup
}

// NewClient creates a new Kite Connect client
func NewClient(apiKey, apiSecret string, log *logger.Logger, debug bool, tickerCfg config.TickerConfig) *Client {
	kc := kiteconnect.New(apiKey)
	if debug {
		kc.SetDebug(true)
	}

	return &Client{
		kc:        kc,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		tickChan:  make(chan broker.Tick, 1000),
		log:       log.Named("kite"),
		debug:     debug,
		tickerCfg: tickerCfg,
		health:    &TickerHealth{},
	}
}

// SetAccessToken sets the access token for authenticated requests
func (c *Client) SetAccessToken(accessToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = accessToken
	c.kc.SetAccessToken(accessToken)
}

// GetLoginURL returns the Kite Connect login URL
func (c *Client) GetLoginURL() string {
	return c.kc.GetLoginURL()
}

// GenerateSession generates a new session using the request token
func (c *Client) GenerateSession(requestToken string) (*kiteconnect.UserSession, error) {
	session, err := c.kc.GenerateSession(requestToken, c.apiSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session: %w", err)
	}

	c.SetAccessToken(session.AccessToken)
	return &session, nil
}

// Connect establishes connection to the broker
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken == "" {
		return fmt.Errorf("access token not set - call SetAccessToken or GenerateSession first")
	}

	c.log.Info("Connecting to Kite Connect")

	// Initialize context for ticker goroutines
	c.tickerCtx, c.tickerCancel = context.WithCancel(ctx)

	return nil
}

// Disconnect closes all connections
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Graceful shutdown: stop ticker first
	if c.tickerCancel != nil {
		c.tickerCancel()
	}

	// Wait for ticker goroutines to finish
	c.tickerWg.Wait()

	if c.ticker != nil {
		c.ticker.Close()
		c.ticker.Stop()
		c.ticker = nil
	}

	c.connected = false
	c.health.SetConnected(false)
	close(c.tickChan)
	c.log.Info("Disconnected from Kite Connect")
	return nil
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SubscribeTicks subscribes to real-time ticks for given instrument tokens
func (c *Client) SubscribeTicks(ctx context.Context, tokens []uint32) (<-chan broker.Tick, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create ticker if not exists
	if c.ticker == nil {
		c.ticker = kiteticker.New(c.apiKey, c.accessToken)

		// Apply ticker configuration
		c.ticker.SetAutoReconnect(c.tickerCfg.AutoReconnect)
		c.ticker.SetReconnectMaxDelay(c.tickerCfg.MaxReconnectDelay)
		c.ticker.SetReconnectMaxRetries(c.tickerCfg.ReconnectMaxRetries)

		// Set callbacks with health tracking
		c.ticker.OnConnect(func() {
			c.mu.Lock()
			c.connected = true
			c.health.SetConnected(true)
			c.health.RecordConnection()

			// Track reconnection
			if c.health.GetSnapshot().CurrentReconnectAttempt > 0 {
				c.health.RecordReconnect()
			}
			c.health.ResetReconnectAttempt()
			c.mu.Unlock()

			c.log.Infow("Ticker connected",
				"reconnect", c.health.GetSnapshot().CurrentReconnectAttempt > 0,
			)

			// Subscribe to tokens and set mode to full
			c.ticker.Subscribe(tokens)
			c.ticker.SetMode(kiteticker.ModeFull, tokens)
		})

		c.ticker.OnTick(func(tick kitemodels.Tick) {
			c.handleTick(tick)
			c.health.RecordTick()
		})

		c.ticker.OnError(func(err error) {
			c.log.Errorw("Ticker error",
				"error", err,
				"disconnect_count", c.health.GetSnapshot().DisconnectCount,
			)
		})

		c.ticker.OnClose(func(code int, reason string) {
			c.mu.Lock()
			c.connected = false
			c.health.SetConnected(false)
			c.health.RecordDisconnect()
			c.mu.Unlock()

			c.log.Warnw("Ticker closed",
				"code", code,
				"reason", reason,
				"total_disconnects", c.health.GetSnapshot().DisconnectCount,
			)
		})

		c.ticker.OnReconnect(func(attempt int, delay time.Duration) {
			c.health.SetReconnectAttempt(attempt)

			c.log.Infow("Ticker reconnecting",
				"attempt", attempt,
				"delay", delay,
				"max_delay", c.tickerCfg.MaxReconnectDelay,
			)
		})

		c.ticker.OnNoReconnect(func(attempt int) {
			c.log.Errorw("Ticker failed to reconnect after all attempts",
				"attempts", attempt,
				"max_retries", c.tickerCfg.ReconnectMaxRetries,
			)
		})

		// Start ticker in goroutine with graceful shutdown support
		c.tickerWg.Add(1)
		go func() {
			defer c.tickerWg.Done()

			done := make(chan struct{})
			go func() {
				c.ticker.Serve()
				close(done)
			}()

			select {
			case <-done:
				// Ticker Serve() returned naturally
			case <-c.tickerCtx.Done():
				// Our context was cancelled, stop the ticker
				c.ticker.Stop()
				<-done // Wait for Serve() to complete
			}
		}()
	} else {
		// Subscribe to additional tokens
		c.ticker.Subscribe(tokens)
		c.ticker.SetMode(kiteticker.ModeFull, tokens)
	}

	return c.tickChan, nil
}

// UnsubscribeTicks unsubscribes from ticks
func (c *Client) UnsubscribeTicks(tokens []uint32) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.ticker != nil {
		c.ticker.Unsubscribe(tokens)
	}
	return nil
}

// GetTickerHealth returns the current ticker health status
func (c *Client) GetTickerHealth() interface{} {
	return c.health.GetSnapshot()
}

// handleTick converts Kite tick to broker.Tick and sends to channel
func (c *Client) handleTick(kt kitemodels.Tick) {
	tick := broker.Tick{
		InstrumentToken: kt.InstrumentToken,
		IsTradable:      kt.IsTradable,
		Timestamp:       kt.Timestamp.Time,
		LastPrice:       kt.LastPrice,
		LastQuantity:    int64(kt.LastTradedQuantity),
		AveragePrice:    kt.AverageTradePrice,
		Volume:          int64(kt.VolumeTraded),
		BuyQuantity:     int64(kt.TotalBuyQuantity),
		SellQuantity:    int64(kt.TotalSellQuantity),
		OpenInterest:    int64(kt.OI),
		OHLC: broker.OHLC{
			Open:  kt.OHLC.Open,
			High:  kt.OHLC.High,
			Low:   kt.OHLC.Low,
			Close: kt.OHLC.Close,
		},
	}

	// Convert depth
	tick.Depth.Buy = make([]broker.DepthItem, len(kt.Depth.Buy))
	for i, d := range kt.Depth.Buy {
		tick.Depth.Buy[i] = broker.DepthItem{
			Price:    d.Price,
			Quantity: int64(d.Quantity),
			Orders:   int(d.Orders),
		}
	}
	tick.Depth.Sell = make([]broker.DepthItem, len(kt.Depth.Sell))
	for i, d := range kt.Depth.Sell {
		tick.Depth.Sell[i] = broker.DepthItem{
			Price:    d.Price,
			Quantity: int64(d.Quantity),
			Orders:   int(d.Orders),
		}
	}

	// Non-blocking send with metrics
	select {
	case c.tickChan <- tick:
	default:
		c.health.RecordTickDrop()
		c.log.Warnw("Tick channel full, dropping tick",
			"drop_count", c.health.GetSnapshot().TickDropCount,
		)
	}
}

// GetQuote gets quotes for given instruments
func (c *Client) GetQuote(ctx context.Context, instruments []string) (map[string]broker.Quote, error) {
	quotes, err := c.kc.GetQuote(instruments...)
	if err != nil {
		return nil, fmt.Errorf("failed to get quotes: %w", err)
	}

	result := make(map[string]broker.Quote)
	for key, q := range quotes {
		result[key] = broker.Quote{
			InstrumentToken: uint32(q.InstrumentToken),
			Timestamp:       q.Timestamp.Time,
			LastPrice:       q.LastPrice,
			Volume:          int64(q.Volume),
			BuyQuantity:     int64(q.BuyQuantity),
			SellQuantity:    int64(q.SellQuantity),
			OpenInterest:    int64(q.OI),
			OHLC: broker.OHLC{
				Open:  q.OHLC.Open,
				High:  q.OHLC.High,
				Low:   q.OHLC.Low,
				Close: q.OHLC.Close,
			},
		}
	}

	return result, nil
}

// GetInstruments gets all instruments for an exchange
func (c *Client) GetInstruments(ctx context.Context, exchange string) ([]broker.Instrument, error) {
	// Note: gokiteconnect v4 requires exchange to be passed
	instruments, err := c.kc.GetInstrumentsByExchange(exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to get instruments: %w", err)
	}

	result := make([]broker.Instrument, len(instruments))
	for i, inst := range instruments {
		result[i] = broker.Instrument{
			InstrumentToken: uint32(inst.InstrumentToken),
			ExchangeToken:   uint32(inst.ExchangeToken),
			TradingSymbol:   inst.Tradingsymbol,
			Name:            inst.Name,
			Exchange:        inst.Exchange,
			Segment:         inst.Segment,
			InstrumentType:  inst.InstrumentType,
			Expiry:          inst.Expiry.Time,
			Strike:          inst.StrikePrice,
			LotSize:         int(inst.LotSize),
			TickSize:        inst.TickSize,
		}
	}

	return result, nil
}

// PlaceOrder places a new order
func (c *Client) PlaceOrder(ctx context.Context, order broker.OrderRequest) (string, error) {
	orderParams := kiteconnect.OrderParams{
		Exchange:        order.Exchange,
		Tradingsymbol:   order.TradingSymbol,
		TransactionType: string(order.TransactionType),
		Quantity:        order.Quantity,
		Product:         string(order.Product),
		OrderType:       string(order.OrderType),
		Price:           order.Price,
		TriggerPrice:    order.TriggerPrice,
		Validity:        order.Validity,
		Tag:             order.Tag,
	}

	if orderParams.Validity == "" {
		orderParams.Validity = "DAY"
	}

	resp, err := c.kc.PlaceOrder(kiteconnect.VarietyRegular, orderParams)
	if err != nil {
		return "", fmt.Errorf("failed to place order: %w", err)
	}

	c.log.Order("placed",
		"order_id", resp.OrderID,
		"symbol", order.TradingSymbol,
		"type", order.TransactionType,
		"qty", order.Quantity,
	)

	return resp.OrderID, nil
}

// ModifyOrder modifies an existing order
func (c *Client) ModifyOrder(ctx context.Context, orderID string, changes broker.OrderModification) error {
	orderParams := kiteconnect.OrderParams{
		Quantity:     changes.Quantity,
		Price:        changes.Price,
		TriggerPrice: changes.TriggerPrice,
		OrderType:    string(changes.OrderType),
	}

	_, err := c.kc.ModifyOrder(kiteconnect.VarietyRegular, orderID, orderParams)
	if err != nil {
		return fmt.Errorf("failed to modify order: %w", err)
	}

	c.log.Order("modified", "order_id", orderID)
	return nil
}

// CancelOrder cancels an existing order
func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	_, err := c.kc.CancelOrder(kiteconnect.VarietyRegular, orderID, nil)
	if err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}

	c.log.Order("cancelled", "order_id", orderID)
	return nil
}

// GetOrders returns all orders for the day
func (c *Client) GetOrders(ctx context.Context) ([]broker.Order, error) {
	orders, err := c.kc.GetOrders()
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}

	result := make([]broker.Order, len(orders))
	for i, o := range orders {
		result[i] = convertOrder(o)
	}

	return result, nil
}

// GetOrderHistory returns history for a specific order
func (c *Client) GetOrderHistory(ctx context.Context, orderID string) ([]broker.Order, error) {
	orders, err := c.kc.GetOrderHistory(orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order history: %w", err)
	}

	result := make([]broker.Order, len(orders))
	for i, o := range orders {
		result[i] = convertOrder(o)
	}

	return result, nil
}

// GetPositions returns all positions
func (c *Client) GetPositions(ctx context.Context) ([]broker.Position, error) {
	positions, err := c.kc.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	result := make([]broker.Position, 0, len(positions.Net))
	for _, p := range positions.Net {
		result = append(result, broker.Position{
			TradingSymbol: p.Tradingsymbol,
			Exchange:      p.Exchange,
			Product:       broker.Product(p.Product),
			Quantity:      int(p.Quantity),
			OvernightQty:  int(p.OvernightQuantity),
			DayQuantity:   int(p.DayBuyQuantity - p.DaySellQuantity),
			BuyQuantity:   int(p.BuyQuantity),
			SellQuantity:  int(p.SellQuantity),
			AveragePrice:  p.AveragePrice,
			BuyPrice:      p.BuyPrice,
			SellPrice:     p.SellPrice,
			LastPrice:     p.LastPrice,
			PnL:           p.PnL,
			DayPnL:        p.M2M,
			Multiplier:    int(p.Multiplier),
			Value:         p.Value,
		})
	}

	return result, nil
}

// convertOrder converts Kite order to broker.Order
func convertOrder(o kiteconnect.Order) broker.Order {
	return broker.Order{
		OrderID:         o.OrderID,
		ExchangeOrderID: o.ExchangeOrderID,
		ParentOrderID:   o.ParentOrderID,
		Status:          broker.OrderStatus(o.Status),
		StatusMessage:   o.StatusMessage,
		TradingSymbol:   o.TradingSymbol,
		Exchange:        o.Exchange,
		TransactionType: broker.TransactionType(o.TransactionType),
		OrderType:       broker.OrderType(o.OrderType),
		Product:         broker.Product(o.Product),
		Quantity:        int(o.Quantity),
		FilledQuantity:  int(o.FilledQuantity),
		PendingQuantity: int(o.PendingQuantity),
		Price:           o.Price,
		TriggerPrice:    o.TriggerPrice,
		AveragePrice:    o.AveragePrice,
		PlacedAt:        o.OrderTimestamp.Time,
		ExchangeTime:    o.ExchangeTimestamp.Time,
		Tag:             o.Tag,
	}
}
