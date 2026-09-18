package papertrader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTypeSafeURL = "https://api.typesafe.ai/v1/systemone"

// TypeSafe publishes Jev's current price as $0.042 per million input tokens;
// output tokens are free. Keep this as an explicit telemetry constant so the
// dashboard can show an auditable estimate without pretending the broker or
// model returned a dollar amount.
const TypeSafeInputCostPerMillionUSD = 0.042

type Question struct {
	Type         string      `json:"type"`
	Instructions string      `json:"instructions"`
	Criteria     interface{} `json:"criteria,omitempty"`
}

type requestBody struct {
	State     Snapshot            `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// EstimateTypeSafeCostUSD converts the documented Jev input-token price into
// a per-request estimate. The boolean is false when the API did not return
// usage, so callers can display "unknown" rather than a misleading zero.
func EstimateTypeSafeCostUSD(usage Usage) (float64, bool) {
	if usage.InputTokens <= 0 {
		return 0, false
	}
	return float64(usage.InputTokens) * TypeSafeInputCostPerMillionUSD / 1_000_000, true
}

type responseBody struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Evaluation is the typed, application-facing result. Raw generated prose is
// intentionally not part of the contract.
type Evaluation struct {
	Model                 string
	SnapshotID            string
	AsOf                  time.Time
	Action                Action
	ActionProbabilities   map[string]float64
	ActionConfidence      float64
	BuySupport            float64
	MarketRegime          string
	RegimeProbabilities   map[string]float64
	RegimeConfidence      float64
	SetupQuality          float64
	SetupConfidence       float64
	HoldingHorizon        string
	HorizonProbabilities  map[string]float64
	Usage                 Usage
	Latency               time.Duration
	Price                 float64
	Quote                 Quote
	CurrentBar            Bar
	StateBytes            int
	HistoryOneMinuteBars  int
	HistoryFiveMinuteBars int
	HistoryDailyBars      int
}

// TypeSafeClient is a small stdlib HTTP client for the System One API.
type TypeSafeClient struct {
	APIKey         string
	Model          string
	Endpoint       string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	MaxRetries     int
}

func NewTypeSafeClient(apiKey string) *TypeSafeClient {
	return &TypeSafeClient{
		APIKey: apiKey, Model: "jev-latest", Endpoint: defaultTypeSafeURL,
		HTTPClient: &http.Client{}, RequestTimeout: 8 * time.Second, MaxRetries: 1,
	}
}

func DefaultQuestions(tradingSymbol string) map[string]Question {
	tradingSymbol = strings.TrimSpace(tradingSymbol)
	if tradingSymbol == "" {
		tradingSymbol = DefaultTradingSymbol
	}
	return map[string]Question{
		"action": {
			Type:         "choice",
			Instructions: fmt.Sprintf("Using the current %s state and the supplied historical candles, choose the single best next action for a long-only intraday trader. Buy only when the evidence supports opening a new long position now. Exit only when an existing long should be closed. Hold means keep an existing long open. No_action means there is insufficient evidence or no actionable opportunity.", tradingSymbol),
			Criteria: map[string]string{
				"buy":       "Open a new long intraday position now.",
				"hold":      "Keep an existing long position open.",
				"exit":      "Close an existing long position now.",
				"no_action": "Do not open or close a position because evidence is insufficient or no opportunity is present.",
			},
		},
		"buy_support": {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does the supplied market state support opening a new long %s intraday position at the current price?", tradingSymbol),
			Criteria: map[string]string{
				"true":  "The current and historical evidence supports a timely long entry.",
				"false": "The evidence does not support a timely long entry.",
			},
		},
		"market_regime": {
			Type:         "choice",
			Instructions: fmt.Sprintf("What is the current %s market regime based on the supplied current and historical candles?", tradingSymbol),
			Criteria: map[string]string{
				"trending_up":       "Persistent upward movement with continuation evidence.",
				"trending_down":     "Persistent downward movement; not suitable for a long entry.",
				"range":             "Oscillating without a clear directional edge.",
				"unstable":          "Abnormal or conflicting movement, gaps, or unreliable context.",
				"insufficient_data": "Not enough reliable data to classify the regime.",
			},
		},
		"setup_quality": {
			Type:         "score",
			Instructions: "Rate the quality of the possible intraday long setup in the supplied state.",
			Criteria: []string{
				"No usable setup or insufficient data",
				"Weak setup with substantial conflict or uncertainty",
				"Mixed setup with some supporting evidence",
				"Good setup with aligned evidence",
				"Exceptional setup with unusually strong and consistent evidence",
			},
		},
		"holding_horizon": {
			Type:         "choice",
			Instructions: "If a long position is opened, what holding horizon is supported by the supplied state?",
			Criteria: map[string]string{
				"minutes":           "The expected opportunity is very short-lived.",
				"intraday":          "The opportunity is expected to remain relevant during today's session.",
				"insufficient_data": "The horizon cannot be estimated reliably.",
			},
		},
	}
}

func (c *TypeSafeClient) Evaluate(ctx context.Context, snapshot Snapshot) (Evaluation, error) {
	evaluation := Evaluation{SnapshotID: snapshot.SnapshotID, AsOf: snapshot.Session.AsOf}
	if err := snapshot.Validate(); err != nil {
		return evaluation, err
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return evaluation, errors.New("typesafe API key is required")
	}
	if c.Model == "" {
		c.Model = "jev-latest"
	}
	if c.Endpoint == "" {
		c.Endpoint = defaultTypeSafeURL
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}

	requestStarted := time.Now()
	body, err := json.Marshal(requestBody{State: snapshot, Model: c.Model, Questions: DefaultQuestions(snapshot.Instrument.TradingSymbol)})
	if err != nil {
		evaluation.Latency = time.Since(requestStarted)
		return evaluation, fmt.Errorf("marshal typesafe request: %w", err)
	}

	retries := c.MaxRetries
	if retries < 0 {
		retries = 0
	}
	for attempt := 0; ; attempt++ {
		requestCtx := ctx
		cancel := func() {}
		if c.RequestTimeout > 0 {
			requestCtx, cancel = context.WithTimeout(ctx, c.RequestTimeout)
		}
		result, status, requestErr := c.doRequest(requestCtx, body)
		cancel()
		if requestErr == nil {
			decoded, decodeErr := decodeEvaluation(snapshot.SnapshotID, result)
			if decodeErr != nil {
				evaluation.Latency = time.Since(requestStarted)
				return evaluation, decodeErr
			}
			evaluation = decoded
			evaluation.Latency = time.Since(requestStarted)
			return evaluation, nil
		}
		if (status != http.StatusTooManyRequests && status != 529) || attempt >= retries {
			evaluation.Latency = time.Since(requestStarted)
			return evaluation, requestErr
		}
		select {
		case <-ctx.Done():
			evaluation.Latency = time.Since(requestStarted)
			return evaluation, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
}

func (c *TypeSafeClient) doRequest(ctx context.Context, body []byte) (responseBody, int, error) {
	var result responseBody
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return result, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return result, 0, err
	}
	defer resp.Body.Close()
	responseBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return result, resp.StatusCode, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, resp.StatusCode, fmt.Errorf("typesafe HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBytes)))
	}
	if err := json.Unmarshal(responseBytes, &result); err != nil {
		return result, resp.StatusCode, fmt.Errorf("decode typesafe response: %w", err)
	}
	return result, resp.StatusCode, nil
}

func decodeEvaluation(snapshotID string, response responseBody) (Evaluation, error) {
	action, ok := response.Answers["action"]
	if !ok || action.Type != "choice" {
		return Evaluation{}, errors.New("typesafe response missing choice answer: action")
	}
	if action.Choice != string(ActionBuy) && action.Choice != string(ActionHold) && action.Choice != string(ActionExit) && action.Choice != string(ActionNoAction) {
		return Evaluation{}, fmt.Errorf("typesafe returned unsupported action %q", action.Choice)
	}
	regime, ok := response.Answers["market_regime"]
	if !ok || regime.Type != "choice" {
		return Evaluation{}, errors.New("typesafe response missing choice answer: market_regime")
	}
	buySupport, ok := response.Answers["buy_support"]
	if !ok || buySupport.Type != "noul" {
		return Evaluation{}, errors.New("typesafe response missing noul answer: buy_support")
	}
	quality := response.Answers["setup_quality"]
	horizon := response.Answers["holding_horizon"]

	return Evaluation{
		Model: response.Model, SnapshotID: snapshotID,
		Action: Action(action.Choice), ActionProbabilities: action.Probabilities, ActionConfidence: action.Confidence,
		BuySupport:   buySupport.Noul,
		MarketRegime: regime.Choice, RegimeProbabilities: regime.Probabilities, RegimeConfidence: regime.Confidence,
		SetupQuality: quality.Score, SetupConfidence: quality.Confidence,
		HoldingHorizon: horizon.Choice, HorizonProbabilities: horizon.Probabilities,
		Usage: response.Usage,
	}, nil
}
