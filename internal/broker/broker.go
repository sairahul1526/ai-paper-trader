package broker

import (
	"context"
	"time"
)

// Broker defines the interface for all broker operations
type Broker interface {
	// Connection management
	Connect(ctx context.Context) error
	Disconnect() error
	IsConnected() bool

	// Market data
	SubscribeTicks(ctx context.Context, tokens []uint32) (<-chan Tick, error)
	UnsubscribeTicks(tokens []uint32) error
	GetQuote(ctx context.Context, instruments []string) (map[string]Quote, error)
	GetInstruments(ctx context.Context, exchange string) ([]Instrument, error)

	// Orders
	PlaceOrder(ctx context.Context, order OrderRequest) (string, error)
	ModifyOrder(ctx context.Context, orderID string, changes OrderModification) error
	CancelOrder(ctx context.Context, orderID string) error
	GetOrders(ctx context.Context) ([]Order, error)
	GetOrderHistory(ctx context.Context, orderID string) ([]Order, error)

	// Positions
	GetPositions(ctx context.Context) ([]Position, error)
}

// TickerHealthProvider is an optional interface for brokers that provide ticker health monitoring
type TickerHealthProvider interface {
	GetTickerHealth() interface{}
}

// Tick represents real-time market tick data
type Tick struct {
	InstrumentToken uint32
	IsTradable      bool
	Timestamp       time.Time
	LastPrice       float64
	LastQuantity    int64
	AveragePrice    float64
	Volume          int64
	BuyQuantity     int64
	SellQuantity    int64
	OpenInterest    int64
	OHLC            OHLC
	Depth           MarketDepth
}

// OHLC represents Open, High, Low, Close data
type OHLC struct {
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// MarketDepth represents order book depth
type MarketDepth struct {
	Buy  []DepthItem
	Sell []DepthItem
}

// DepthItem represents a single level in the order book
type DepthItem struct {
	Price    float64
	Quantity int64
	Orders   int
}

// Quote represents a market quote
type Quote struct {
	InstrumentToken uint32
	Timestamp       time.Time
	LastPrice       float64
	Volume          int64
	BuyQuantity     int64
	SellQuantity    int64
	OpenInterest    int64
	OHLC            OHLC
}

// Instrument represents a tradable instrument
type Instrument struct {
	InstrumentToken uint32
	ExchangeToken   uint32
	TradingSymbol   string
	Name            string
	Exchange        string
	Segment         string
	InstrumentType  string
	Expiry          time.Time
	Strike          float64
	LotSize         int
	TickSize        float64
}

// OrderRequest for placing new orders
type OrderRequest struct {
	Exchange        string
	TradingSymbol   string
	TransactionType TransactionType
	Quantity        int
	Product         Product
	OrderType       OrderType
	Price           float64
	TriggerPrice    float64
	Validity        string
	Tag             string
}

// OrderModification for modifying existing orders
type OrderModification struct {
	Quantity     int
	Price        float64
	TriggerPrice float64
	OrderType    OrderType
}

// Order represents an order in the system
type Order struct {
	OrderID         string
	ExchangeOrderID string
	ParentOrderID   string
	Status          OrderStatus
	StatusMessage   string
	TradingSymbol   string
	Exchange        string
	TransactionType TransactionType
	OrderType       OrderType
	Product         Product
	Quantity        int
	FilledQuantity  int
	PendingQuantity int
	Price           float64
	TriggerPrice    float64
	AveragePrice    float64
	PlacedAt        time.Time
	ExchangeTime    time.Time
	Tag             string
}

// Position represents a trading position
type Position struct {
	TradingSymbol string
	Exchange      string
	Product       Product
	Quantity      int
	OvernightQty  int
	DayQuantity   int
	BuyQuantity   int
	SellQuantity  int
	AveragePrice  float64
	BuyPrice      float64
	SellPrice     float64
	LastPrice     float64
	PnL           float64
	DayPnL        float64
	Multiplier    int
	Value         float64
}

// TransactionType represents buy or sell
type TransactionType string

const (
	TransactionBuy  TransactionType = "BUY"
	TransactionSell TransactionType = "SELL"
)

// OrderType represents the type of order
type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeSL     OrderType = "SL"
	OrderTypeSLM    OrderType = "SL-M"
)

// Product represents the product type
type Product string

const (
	ProductMIS  Product = "MIS"  // Intraday
	ProductCNC  Product = "CNC"  // Delivery
	ProductNRML Product = "NRML" // Normal F&O
)

// OrderStatus represents the status of an order
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "PENDING"
	OrderStatusOpen      OrderStatus = "OPEN"
	OrderStatusComplete  OrderStatus = "COMPLETE"
	OrderStatusCancelled OrderStatus = "CANCELLED"
	OrderStatusRejected  OrderStatus = "REJECTED"
	OrderStatusModified  OrderStatus = "MODIFIED"
)

// Direction represents trade direction
type Direction int

const (
	DirectionNone Direction = iota
	DirectionBullish
	DirectionBearish
)

// String returns string representation of Direction
func (d Direction) String() string {
	switch d {
	case DirectionBullish:
		return "BULLISH"
	case DirectionBearish:
		return "BEARISH"
	default:
		return "NONE"
	}
}

// OptionType returns CE or PE based on direction
func (d Direction) OptionType() string {
	switch d {
	case DirectionBullish:
		return "CE"
	case DirectionBearish:
		return "PE"
	default:
		return ""
	}
}
