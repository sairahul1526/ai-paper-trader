// Package alpaca adapts Alpaca's free/basic US market-data feeds to the
// broker-neutral paper-trader interfaces. It intentionally has no live-order
// implementation: this project can observe and simulate, never route orders.
package alpaca

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"ai-paper-trader/internal/broker"
	"github.com/gorilla/websocket"
)

const (
	defaultDataURL   = "https://data.alpaca.markets"
	defaultStreamURL = "wss://stream.data.alpaca.markets/v2/iex"
)

// Client is an Alpaca market-data client. The free/basic plan uses IEX for
// real-time US equity quotes; the feed choice is explicit in the URL so a
// future paid feed can be added without changing the runner.
type Client struct {
	APIKey    string
	APISecret string
	DataURL   string
	StreamURL string
	Feed      string
	HTTP      *http.Client

	mu          sync.Mutex
	connected   bool
	conn        *websocket.Conn
	stop        chan struct{}
	wg          sync.WaitGroup
	symbols     map[uint32]string
	subscribed  map[uint32]struct{}
	tickChannel chan broker.Tick
}

var _ broker.Broker = (*Client)(nil)

func NewClient(apiKey, apiSecret string) *Client {
	return &Client{
		APIKey: apiKey, APISecret: apiSecret, DataURL: defaultDataURL,
		StreamURL: defaultStreamURL, Feed: "iex", HTTP: &http.Client{Timeout: 15 * time.Second},
		symbols: make(map[uint32]string), subscribed: make(map[uint32]struct{}),
	}
}

// NewClientFromEnv applies optional feed-endpoint overrides while keeping the
// free IEX path as the safe default. Paid/consolidated feeds can be enabled by
// a deployment without changing the runner or paper ledger.
func NewClientFromEnv(apiKey, apiSecret string) *Client {
	client := NewClient(apiKey, apiSecret)
	if value := strings.TrimSpace(os.Getenv("ALPACA_DATA_URL")); value != "" {
		client.DataURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ALPACA_STREAM_URL")); value != "" {
		client.StreamURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ALPACA_FEED")); value != "" {
		client.Feed = strings.ToLower(value)
	}
	return client
}

// InstrumentToken is a stable local identifier for a US symbol. Alpaca's
// stock feed uses symbols rather than numeric instrument tokens.
func InstrumentToken(symbol string) uint32 {
	digest := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(symbol))))
	token := binary.BigEndian.Uint32(digest[:4])
	if token == 0 {
		return 1
	}
	return token
}

func (c *Client) Connect(ctx context.Context) error {
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.APISecret) == "" {
		return errors.New("Alpaca API key and secret are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()
	return nil
}

func (c *Client) Disconnect() error {
	c.mu.Lock()
	if !c.connected && c.conn == nil {
		c.mu.Unlock()
		return nil
	}
	c.connected = false
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	c.wg.Wait()
	c.mu.Lock()
	if c.tickChannel != nil {
		close(c.tickChannel)
		c.tickChannel = nil
	}
	c.mu.Unlock()
	return nil
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// ResolveEquity records a symbol and returns the broker-neutral instrument.
func (c *Client) ResolveEquity(_ context.Context, exchange, symbol string) (broker.Instrument, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return broker.Instrument{}, errors.New("US symbol is required")
	}
	if strings.ContainsAny(symbol, " ./") {
		return broker.Instrument{}, fmt.Errorf("invalid US symbol %q", symbol)
	}
	token := InstrumentToken(symbol)
	c.mu.Lock()
	c.symbols[token] = symbol
	c.mu.Unlock()
	if exchange == "" {
		exchange = "US"
	}
	return broker.Instrument{InstrumentToken: token, TradingSymbol: symbol, Name: symbol, Exchange: strings.ToUpper(exchange), Segment: "US-EQ", InstrumentType: "EQ", LotSize: 1, TickSize: 0.01}, nil
}

func (c *Client) SubscribeTicks(ctx context.Context, tokens []uint32) (<-chan broker.Tick, error) {
	if len(tokens) == 0 {
		return nil, errors.New("at least one US instrument token is required")
	}
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, errors.New("Alpaca client is not connected")
	}
	if c.tickChannel != nil {
		channel := c.tickChannel
		c.mu.Unlock()
		return channel, nil
	}
	for _, token := range tokens {
		if _, ok := c.symbols[token]; !ok {
			c.mu.Unlock()
			return nil, fmt.Errorf("US instrument token %d was not resolved", token)
		}
		c.subscribed[token] = struct{}{}
	}
	channel := make(chan broker.Tick, 512)
	stop := make(chan struct{})
	c.tickChannel, c.stop = channel, stop
	c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.StreamURL, http.Header{})
	if err != nil {
		c.mu.Lock()
		c.tickChannel, c.stop = nil, nil
		c.mu.Unlock()
		return nil, fmt.Errorf("connect Alpaca stock stream: %w", err)
	}
	if err := c.authenticateAndSubscribe(conn, tokens); err != nil {
		_ = conn.Close()
		c.mu.Lock()
		c.tickChannel, c.stop = nil, nil
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.wg.Add(1)
	go c.readLoop(conn, stop, channel)
	return channel, nil
}

func (c *Client) authenticateAndSubscribe(conn *websocket.Conn, tokens []uint32) error {
	if err := conn.WriteJSON(map[string]string{"action": "auth", "key": c.APIKey, "secret": c.APISecret}); err != nil {
		return fmt.Errorf("authenticate Alpaca stream: %w", err)
	}
	// Alpaca requires the subscription message after the auth acknowledgement.
	// Read and discard that acknowledgement here; the long-lived read loop only
	// receives market messages after this handshake is complete.
	_, acknowledgement, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read Alpaca auth acknowledgement: %w", err)
	}
	var acknowledgements []struct {
		Type    string `json:"T"`
		Message string `json:"msg"`
	}
	if json.Unmarshal(acknowledgement, &acknowledgements) == nil {
		for _, item := range acknowledgements {
			if item.Type == "error" {
				return fmt.Errorf("Alpaca stream authentication failed: %s", item.Message)
			}
		}
	}
	if err := conn.WriteJSON(map[string]interface{}{"action": "subscribe", "trades": c.symbolList(tokens), "quotes": c.symbolList(tokens)}); err != nil {
		return fmt.Errorf("subscribe Alpaca stream: %w", err)
	}
	return nil
}

func (c *Client) symbolList(tokens []uint32) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, c.symbols[token])
	}
	return result
}

type streamMessage struct {
	Type      string    `json:"T"`
	Symbol    string    `json:"S"`
	Price     float64   `json:"p"`
	Size      int64     `json:"s"`
	BidPrice  float64   `json:"bp"`
	AskPrice  float64   `json:"ap"`
	BidSize   int64     `json:"bs"`
	AskSize   int64     `json:"as"`
	Timestamp time.Time `json:"t"`
}

func (c *Client) readLoop(conn *websocket.Conn, stop <-chan struct{}, output chan<- broker.Tick) {
	defer c.wg.Done()
	defer conn.Close()
	prices := make(map[string]float64)
	volumes := make(map[string]int64)
	quotes := make(map[string]quoteSnapshot)
	for {
		_, body, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var messages []streamMessage
		if json.Unmarshal(body, &messages) != nil {
			continue // auth/subscription acknowledgements are not market ticks.
		}
		for _, message := range messages {
			if message.Symbol == "" {
				continue
			}
			if message.Timestamp.IsZero() {
				message.Timestamp = time.Now().UTC()
			}
			if message.Type == "t" {
				if message.Price > 0 {
					prices[message.Symbol] = message.Price
				}
				if message.Size > 0 {
					volumes[message.Symbol] += message.Size
				}
			}
			if message.Type == "q" {
				quotes[message.Symbol] = quoteSnapshot{Bid: message.BidPrice, Ask: message.AskPrice, BuySize: message.BidSize, SellSize: message.AskSize}
			}
			price := prices[message.Symbol]
			if price <= 0 {
				continue
			}
			quote := quotes[message.Symbol]
			tick := broker.Tick{InstrumentToken: InstrumentToken(message.Symbol), IsTradable: true, Timestamp: message.Timestamp, LastPrice: price, LastQuantity: message.Size, Volume: volumes[message.Symbol], BuyQuantity: quote.BuySize, SellQuantity: quote.SellSize, Depth: broker.MarketDepth{Buy: []broker.DepthItem{{Price: quote.Bid, Quantity: quote.BuySize}}, Sell: []broker.DepthItem{{Price: quote.Ask, Quantity: quote.SellSize}}}}
			select {
			case output <- tick:
			case <-stop:
				return
			}
		}
	}
}

type quoteSnapshot struct {
	Bid, Ask          float64
	BuySize, SellSize int64
}

func (c *Client) UnsubscribeTicks(tokens []uint32) error { return nil }

func (c *Client) GetQuote(ctx context.Context, instruments []string) (map[string]broker.Quote, error) {
	result := make(map[string]broker.Quote, len(instruments))
	for _, symbol := range instruments {
		bars, err := c.loadBars(ctx, symbol, "1Min", time.Now().Add(-10*time.Minute), time.Now())
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			continue
		}
		bar := bars[len(bars)-1]
		result[symbol] = broker.Quote{InstrumentToken: InstrumentToken(symbol), Timestamp: bar.Timestamp, LastPrice: bar.Close, Volume: bar.Volume, OHLC: broker.OHLC{Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close}}
	}
	return result, nil
}

func (c *Client) GetInstruments(_ context.Context, exchange string) ([]broker.Instrument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]broker.Instrument, 0, len(c.symbols))
	for token, symbol := range c.symbols {
		result = append(result, broker.Instrument{InstrumentToken: token, TradingSymbol: symbol, Name: symbol, Exchange: exchange, Segment: "US-EQ", InstrumentType: "EQ", LotSize: 1, TickSize: 0.01})
	}
	return result, nil
}

// Orders are intentionally unavailable. A paper runner must never silently
// turn a simulated intent into a broker order.
func (c *Client) PlaceOrder(context.Context, broker.OrderRequest) (string, error) {
	return "", errors.New("Alpaca order routing is disabled: paper-only mode")
}
func (c *Client) ModifyOrder(context.Context, string, broker.OrderModification) error {
	return errors.New("Alpaca order routing is disabled: paper-only mode")
}
func (c *Client) CancelOrder(context.Context, string) error {
	return errors.New("Alpaca order routing is disabled: paper-only mode")
}
func (c *Client) GetOrders(context.Context) ([]broker.Order, error) { return []broker.Order{}, nil }
func (c *Client) GetOrderHistory(context.Context, string) ([]broker.Order, error) {
	return []broker.Order{}, nil
}
func (c *Client) GetPositions(context.Context) ([]broker.Position, error) {
	return []broker.Position{}, nil
}

// HistoricalProvider loads IEX bars through Alpaca's historical data API.
type HistoricalProvider struct{ Client *Client }

func (p HistoricalProvider) Load(ctx context.Context, token uint32, interval string, from, to time.Time) ([]Bar, error) {
	if p.Client == nil {
		return nil, errors.New("Alpaca historical client is required")
	}
	p.Client.mu.Lock()
	symbol := p.Client.symbols[token]
	p.Client.mu.Unlock()
	if symbol == "" {
		return nil, fmt.Errorf("unknown Alpaca instrument token %d", token)
	}
	timeframe := map[string]string{"minute": "1Min", "5minute": "5Min", "day": "1Day"}[interval]
	if timeframe == "" {
		return nil, fmt.Errorf("unsupported Alpaca timeframe %q", interval)
	}
	return p.Client.loadBars(ctx, symbol, timeframe, from, to)
}

// Bar is converted by the command package to avoid importing the root
// package into this internal adapter and creating an import cycle.
type Bar struct {
	Timestamp              time.Time
	Open, High, Low, Close float64
	Volume                 int64
}

type barsResponse struct {
	Bars []struct {
		Timestamp time.Time `json:"t"`
		Open      float64   `json:"o"`
		High      float64   `json:"h"`
		Low       float64   `json:"l"`
		Close     float64   `json:"c"`
		Volume    int64     `json:"v"`
	} `json:"bars"`
	NextPageToken string `json:"next_page_token"`
}

func (c *Client) loadBars(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]Bar, error) {
	base := strings.TrimRight(c.DataURL, "/") + "/v2/stocks/" + url.PathEscape(strings.ToUpper(symbol)) + "/bars"
	page := ""
	result := make([]Bar, 0)
	for {
		query := url.Values{}
		query.Set("timeframe", timeframe)
		query.Set("start", from.UTC().Format(time.RFC3339))
		query.Set("end", to.UTC().Format(time.RFC3339))
		query.Set("adjustment", "raw")
		feed := strings.ToLower(strings.TrimSpace(c.Feed))
		if feed == "" {
			feed = "iex"
		}
		query.Set("feed", feed)
		query.Set("limit", "10000")
		if page != "" {
			query.Set("page_token", page)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("APCA-API-KEY-ID", c.APIKey)
		req.Header.Set("APCA-API-SECRET-KEY", c.APISecret)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Alpaca historical request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("Alpaca historical HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var decoded barsResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, fmt.Errorf("decode Alpaca historical response: %w", err)
		}
		for _, bar := range decoded.Bars {
			result = append(result, Bar{Timestamp: bar.Timestamp, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume})
		}
		if decoded.NextPageToken == "" {
			return result, nil
		}
		page = decoded.NextPageToken
	}
}
