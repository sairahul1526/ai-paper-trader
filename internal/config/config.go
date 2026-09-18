package config

import (
	"errors"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	Broker     BrokerConfig              `yaml:"broker"`
	Ticker     TickerConfig               `yaml:"ticker"`
	Trading    TradingConfig              `yaml:"trading"`
	Risk       RiskConfig                 `yaml:"risk"`
	Signals    SignalsConfig              `yaml:"signals"`
	Logging    LoggingConfig              `yaml:"logging"`
	SignalFusion SignalFusionConfig        `yaml:"signal_fusion"`    // NEW: from research
	Performance PerformanceTargetsConfig  `yaml:"performance"`      // NEW: from research
	Fusion      FusionConfig               `yaml:"fusion"`            // NEW: multi-signal fusion
}

// BrokerConfig holds Zerodha Kite API settings
type BrokerConfig struct {
	APIKey    string `yaml:"api_key"`
	APISecret string `yaml:"api_secret"`
	Debug     bool   `yaml:"debug"`
}

// TickerConfig holds WebSocket ticker connection settings
type TickerConfig struct {
	AutoReconnect      bool          `yaml:"auto_reconnect"`
	MaxReconnectDelay  time.Duration `yaml:"max_reconnect_delay"`
	ReconnectMaxRetries int          `yaml:"reconnect_max_retries"`
	StaleTickThreshold time.Duration `yaml:"stale_tick_threshold"`
}

// TradingConfig holds trading parameters
type TradingConfig struct {
	PaperMode           bool   `yaml:"paper_mode"`
	Underlying          string `yaml:"underlying"`
	Exchange            string `yaml:"exchange"`
	SpotToken           uint32 `yaml:"spot_token"`           // Spot instrument token for price feed
	ExpiryPreference    string `yaml:"expiry_preference"`    // WEEKLY or MONTHLY
	StrikeGap           int    `yaml:"strike_gap"`
	LotSize             int    `yaml:"lot_size"`
	EvalIntervalSeconds int    `yaml:"eval_interval_seconds"`
	MarketOpenTime      string `yaml:"market_open_time"`
	ORBEndTime          string `yaml:"orb_end_time"`
	SquareOffTime       string `yaml:"square_off_time"`
	MarketCloseTime     string `yaml:"market_close_time"`
	ActiveHoursStart    string `yaml:"active_hours_start"`    // Start of active trading hours
	ActiveHoursEnd      string `yaml:"active_hours_end"`      // End of active trading hours
}

// RiskConfig holds risk management parameters
type RiskConfig struct {
	Capital             float64 `yaml:"capital"`
	MaxDailyLossPercent float64 `yaml:"max_daily_loss_percent"`
	MaxTradesPerDay     int     `yaml:"max_trades_per_day"`
	MaxConcurrentTrades int     `yaml:"max_concurrent_trades"`
	DefaultSLPercent    float64 `yaml:"default_sl_percent"`
	DefaultTPPercent    float64 `yaml:"default_tp_percent"`
	MinRiskRewardRatio  float64 `yaml:"min_risk_reward_ratio"`
	ExpirySLPercent     float64 `yaml:"expiry_day_sl_percent"`
	ExpiryTPPercent     float64 `yaml:"expiry_day_tp_percent"`

	// NEW: Position Sizing (from research)
	PositionSizingMethod string  `yaml:"position_sizing_method"` // "kelly" or "fixed"
	KellyFraction        float64 `yaml:"kelly_fraction"`         // 0.5 for half-Kelly
	MaxPositionPercent   float64 `yaml:"max_position_percent"`   // 0.25 for 25%
	MinEdgeThreshold     float64 `yaml:"min_edge_threshold"`     // 0.04 for 4%
	FixedPositionPercent float64 `yaml:"fixed_position_percent"` // 0.01 for 1%

	// NEW: Expected Value Validation (from research)
	MinExpectedValue  float64 `yaml:"min_expected_value"`  // 0.02 for 2%
	RequirePositiveEV bool    `yaml:"require_positive_ev"` // true
}

// SignalsConfig holds configuration for all signal detectors
type SignalsConfig struct {
	ORB             ORBConfig             `yaml:"orb"`
	VWAPBounce      VWAPBounceConfig      `yaml:"vwap_bounce"`
	RSIMACD         RSIMACDConfig         `yaml:"rsi_macd"`
	CandleMomentum  CandleMomentumConfig  `yaml:"candle_momentum"`
	OIBreakout      OIBreakoutConfig      `yaml:"oi_breakout"`
	VolatilityBurst VolatilityBurstConfig `yaml:"volatility_burst"`
}

// ORBConfig for Opening Range Breakout signal
type ORBConfig struct {
	Enabled         bool    `yaml:"enabled"`
	VolumeMultiple  float64 `yaml:"volume_multiple"`
	BreakoutBuffer  float64 `yaml:"breakout_buffer"`
}

// VWAPBounceConfig for VWAP Bounce signal
type VWAPBounceConfig struct {
	Enabled         bool    `yaml:"enabled"`
	BounceThreshold float64 `yaml:"bounce_threshold"`
}

// RSIMACDConfig for RSI + MACD Momentum signal
type RSIMACDConfig struct {
	Enabled       bool    `yaml:"enabled"`
	RSIPeriod     int     `yaml:"rsi_period"`
	RSIOversold   float64 `yaml:"rsi_oversold"`
	RSIOverbought float64 `yaml:"rsi_overbought"`
	MACDFast      int     `yaml:"macd_fast"`
	MACDSlow      int     `yaml:"macd_slow"`
	MACDSignal    int     `yaml:"macd_signal"`
}

// CandleMomentumConfig for Candle Momentum signal
type CandleMomentumConfig struct {
	Enabled          bool    `yaml:"enabled"`
	MinBodyRatio     float64 `yaml:"min_body_ratio"`
	RSIThresholdUp   float64 `yaml:"rsi_threshold_up"`
	RSIThresholdDown float64 `yaml:"rsi_threshold_down"`
}

// OIBreakoutConfig for OI Breakout signal
type OIBreakoutConfig struct {
	Enabled           bool    `yaml:"enabled"`
	OIChangeThreshold float64 `yaml:"oi_change_threshold"`
}

// VolatilityBurstConfig for Volatility Burst signal
type VolatilityBurstConfig struct {
	Enabled          bool    `yaml:"enabled"`
	BBPeriod         int     `yaml:"bb_period"`
	BBStdDev         float64 `yaml:"bb_std_dev"`
	SqueezeThreshold float64 `yaml:"squeeze_threshold"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	OutputPath string `yaml:"output_path"`
}

// NEW: SignalFusionConfig holds temporal signal fusion settings (from research)
type SignalFusionConfig struct {
	EnableTemporalDecay bool               `yaml:"enable_temporal_decay"`
	MinWeightThreshold  float64            `yaml:"min_weight_threshold"` // 0.1 = 10% minimum
	SignalHalfLives     map[string]float64 `yaml:"signal_half_lives"`
}

// NEW: PerformanceTargetsConfig holds performance benchmark targets (from research)
type PerformanceTargetsConfig struct {
	MinSharpeRatio      float64 `yaml:"min_sharpe_ratio"`      // 2.0 (research: 2.14)
	MinProfitFactor     float64 `yaml:"min_profit_factor"`     // 1.5 (research: 1.84)
	MaxDrawdownPercent  float64 `yaml:"max_drawdown_percent"`  // 8.0 (research: 4.2)
	MinWinRate          float64 `yaml:"min_win_rate"`          // 0.65 (research: 0.684)
	MaxBrierScore       float64 `yaml:"max_brier_score"`       // 0.25 for calibration
	AlertOnDegradation  bool    `yaml:"alert_on_degradation"`
}

// NEW: FusionConfig holds multi-signal fusion settings (from research)
type FusionConfig struct {
	EnableFusion     bool    `yaml:"enable_fusion"`     // Enable multi-signal fusion
	MinSignals       int     `yaml:"min_signals"`       // Minimum signals for fusion
	MinConfidence    float64 `yaml:"min_confidence"`    // Minimum confidence threshold
	EnableConfluence bool    `yaml:"enable_confluence"`  // Enable confluence detection
}

// Load reads configuration from file and environment variables
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Override with environment variables
	cfg.loadFromEnv()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// loadFromEnv overrides config values with environment variables
func (c *Config) loadFromEnv() {
	if v := os.Getenv("KITE_API_KEY"); v != "" {
		c.Broker.APIKey = v
	}
	if v := os.Getenv("KITE_API_SECRET"); v != "" {
		c.Broker.APISecret = v
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if c.Broker.APIKey == "" {
		return errors.New("broker API key is required (set KITE_API_KEY env var)")
	}
	if c.Broker.APISecret == "" {
		return errors.New("broker API secret is required (set KITE_API_SECRET env var)")
	}
	if c.Risk.Capital <= 0 {
		return errors.New("capital must be positive")
	}
	if c.Risk.MaxDailyLossPercent <= 0 || c.Risk.MaxDailyLossPercent > 100 {
		return errors.New("max_daily_loss_percent must be between 0 and 100")
	}
	if c.Risk.MaxTradesPerDay <= 0 {
		return errors.New("max_trades_per_day must be positive")
	}
	if c.Trading.StrikeGap <= 0 {
		return errors.New("strike_gap must be positive")
	}
	if c.Trading.LotSize <= 0 {
		return errors.New("lot_size must be positive")
	}
	// Validate underlying
	validUnderlyings := map[string]bool{"NIFTY": true, "BANKNIFTY": true}
	if !validUnderlyings[c.Trading.Underlying] {
		return errors.New("underlying must be NIFTY or BANKNIFTY")
	}
	// Validate expiry preference
	if c.Trading.ExpiryPreference != "" {
		validPrefs := map[string]bool{"WEEKLY": true, "MONTHLY": true}
		if !validPrefs[c.Trading.ExpiryPreference] {
			return errors.New("expiry_preference must be WEEKLY or MONTHLY")
		}
	}
	return nil
}

// GetEvalInterval returns the evaluation interval as time.Duration
func (c *Config) GetEvalInterval() time.Duration {
	return time.Duration(c.Trading.EvalIntervalSeconds) * time.Second
}

// GetMarketOpenTime parses and returns market open time for today
func (c *Config) GetMarketOpenTime() (time.Time, error) {
	return parseTimeToday(c.Trading.MarketOpenTime)
}

// GetORBEndTime parses and returns ORB end time for today
func (c *Config) GetORBEndTime() (time.Time, error) {
	return parseTimeToday(c.Trading.ORBEndTime)
}

// GetSquareOffTime parses and returns square-off time for today
func (c *Config) GetSquareOffTime() (time.Time, error) {
	return parseTimeToday(c.Trading.SquareOffTime)
}

// GetMarketCloseTime parses and returns market close time for today
func (c *Config) GetMarketCloseTime() (time.Time, error) {
	return parseTimeToday(c.Trading.MarketCloseTime)
}

// IsExpiryDay is deprecated - use StrikeSelector.IsExpiryDay() instead
// This fallback checks if today is Thursday for backward compatibility
func (c *Config) IsExpiryDay() bool {
	return time.Now().Weekday() == time.Thursday
}

// GetSLPercent returns the appropriate SL percent based on whether it's expiry day
func (c *Config) GetSLPercent() float64 {
	// Note: Caller should use GetSLPercentForExpiry for accurate expiry check
	if c.IsExpiryDay() {
		return c.Risk.ExpirySLPercent
	}
	return c.Risk.DefaultSLPercent
}

// GetSLPercentForExpiry returns SL percent based on explicit expiry day flag
func (c *Config) GetSLPercentForExpiry(isExpiryDay bool) float64 {
	if isExpiryDay {
		return c.Risk.ExpirySLPercent
	}
	return c.Risk.DefaultSLPercent
}

// GetTPPercent returns the appropriate TP percent based on whether it's expiry day
func (c *Config) GetTPPercent() float64 {
	// Note: Caller should use GetTPPercentForExpiry for accurate expiry check
	if c.IsExpiryDay() {
		return c.Risk.ExpiryTPPercent
	}
	return c.Risk.DefaultTPPercent
}

// GetTPPercentForExpiry returns TP percent based on explicit expiry day flag
func (c *Config) GetTPPercentForExpiry(isExpiryDay bool) float64 {
	if isExpiryDay {
		return c.Risk.ExpiryTPPercent
	}
	return c.Risk.DefaultTPPercent
}

// MaxDailyLossAmount returns the maximum daily loss in currency
func (c *Config) MaxDailyLossAmount() float64 {
	return c.Risk.Capital * (c.Risk.MaxDailyLossPercent / 100)
}

// GetTickerConfig returns ticker configuration with defaults applied
func (c *Config) GetTickerConfig() TickerConfig {
	cfg := c.Ticker

	// Apply defaults if not set
	if cfg.MaxReconnectDelay == 0 {
		cfg.MaxReconnectDelay = 30 * time.Second // Reduced from library default 60s
	}
	if cfg.ReconnectMaxRetries == 0 {
		cfg.ReconnectMaxRetries = 50 // Reduced from library default 300
	}
	if cfg.StaleTickThreshold == 0 {
		cfg.StaleTickThreshold = 10 * time.Second // Warning threshold
	}
	cfg.AutoReconnect = true // Always enable auto-reconnect

	return cfg
}

// parseTimeToday parses a time string (HH:MM) and returns time.Time for today
func parseTimeToday(timeStr string) (time.Time, error) {
	now := time.Now()
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), nil
}

// IsActiveHours checks if current time is within active trading hours
// Returns true if active hours are not configured (allowing trading all day)
func (c *Config) IsActiveHours() bool {
	// If active hours not configured, allow trading all day
	if c.Trading.ActiveHoursStart == "" || c.Trading.ActiveHoursEnd == "" {
		return true
	}

	startTime, err := parseTimeToday(c.Trading.ActiveHoursStart)
	if err != nil {
		return true // If parsing fails, allow trading
	}

	endTime, err := parseTimeToday(c.Trading.ActiveHoursEnd)
	if err != nil {
		return true // If parsing fails, allow trading
	}

	now := time.Now()
	return now.After(startTime) && now.Before(endTime)
}

