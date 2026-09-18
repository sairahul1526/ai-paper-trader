package main

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	papertrader "ai-paper-trader"
)

// The dashboard is deliberately stdlib-only so it can be run beside the
// paper process without adding a frontend build or a second service.
//
//go:embed index.html styles.css app.js
var dashboardFS embed.FS

const (
	defaultAddr       = "127.0.0.1:8787"
	maxEventLines     = 600
	maxMarketLines    = 600
	externalGrace     = 90 * time.Second
	defaultCapital    = 100000.0
	defaultHistory1m  = 1000
	defaultHistory5m  = 500
	defaultHistory1d  = 180
	defaultStateBytes = 32000
)

type runRequest struct {
	Mode       string  `json:"mode"`
	Market     string  `json:"market"`
	Exchange   string  `json:"exchange"`
	Symbol     string  `json:"symbol"`
	Capital    float64 `json:"capital"`
	History1m  int     `json:"history_1m"`
	History5m  int     `json:"history_5m"`
	History1d  int     `json:"history_1d"`
	StateBytes int     `json:"max_history_bytes"`

	Model      string `json:"model"`
	TimeoutMs  int    `json:"timeout_ms"`
	MaxRetries int    `json:"max_retries"`

	MaxPositionValuePercent float64 `json:"max_position_value_percent"`
	MaxDailyLossPercent     float64 `json:"max_daily_loss_percent"`
	MaxTradesPerDay         int     `json:"max_trades_per_day"`
	StopLossPercent         float64 `json:"stop_loss_percent"`
	MaxSpreadPercent        float64 `json:"max_spread_percent"`
	MinActionConfidence     float64 `json:"min_action_confidence"`
	MinBuySupport           float64 `json:"min_buy_support"`

	KiteAPIKey      string `json:"kite_api_key"`
	KiteAPISecret   string `json:"kite_api_secret"`
	KiteAccessToken string `json:"kite_access_token"`
	AlpacaAPIKey    string `json:"alpaca_api_key"`
	AlpacaAPISecret string `json:"alpaca_api_secret"`
	TypeSafeAPIKey  string `json:"typesafe_api_key"`
}

type configView struct {
	Mode                    string                          `json:"mode"`
	Market                  string                          `json:"market"`
	Exchange                string                          `json:"exchange"`
	Symbol                  string                          `json:"symbol"`
	PaperOnly               bool                            `json:"paper_only"`
	Capital                 float64                         `json:"capital"`
	History1m               int                             `json:"history_1m"`
	History5m               int                             `json:"history_5m"`
	History1d               int                             `json:"history_1d"`
	MaxHistoryBytes         int                             `json:"max_history_bytes"`
	Model                   string                          `json:"model"`
	TimeoutMs               int                             `json:"timeout_ms"`
	MaxRetries              int                             `json:"max_retries"`
	MaxPositionValuePercent float64                         `json:"max_position_value_percent"`
	MaxDailyLossPercent     float64                         `json:"max_daily_loss_percent"`
	MaxTradesPerDay         int                             `json:"max_trades_per_day"`
	StopLossPercent         float64                         `json:"stop_loss_percent"`
	MaxSpreadPercent        float64                         `json:"max_spread_percent"`
	MinActionConfidence     float64                         `json:"min_action_confidence"`
	MinBuySupport           float64                         `json:"min_buy_support"`
	KiteAPIKeySet           bool                            `json:"kite_api_key_set"`
	KiteAPISecretSet        bool                            `json:"kite_api_secret_set"`
	KiteAccessTokenSet      bool                            `json:"kite_access_token_set"`
	AlpacaAPIKeySet         bool                            `json:"alpaca_api_key_set"`
	AlpacaAPISecretSet      bool                            `json:"alpaca_api_secret_set"`
	TypeSafeAPIKeySet       bool                            `json:"typesafe_api_key_set"`
	CredentialDiagnostics   map[string]credentialDiagnostic `json:"credential_diagnostics"`
}

// credentialDiagnostic is deliberately non-secret. It lets the local
// dashboard prove which values reached the runner without putting credentials
// in API responses, console logs, or run files.
type credentialDiagnostic struct {
	Set          bool   `json:"set"`
	Length       int    `json:"length"`
	SHA256Prefix string `json:"sha256_12,omitempty"`
}

func credentialDiagnosticFor(value string) credentialDiagnostic {
	value = strings.TrimSpace(value)
	if value == "" {
		return credentialDiagnostic{}
	}
	digest := sha256.Sum256([]byte(value))
	return credentialDiagnostic{Set: true, Length: len(value), SHA256Prefix: hex.EncodeToString(digest[:])[:12]}
}

func credentialDiagnosticsFor(r runRequest) map[string]credentialDiagnostic {
	return map[string]credentialDiagnostic{
		"kite_api_key":      credentialDiagnosticFor(r.KiteAPIKey),
		"kite_api_secret":   credentialDiagnosticFor(r.KiteAPISecret),
		"kite_access_token": credentialDiagnosticFor(r.KiteAccessToken),
		"alpaca_api_key":    credentialDiagnosticFor(r.AlpacaAPIKey),
		"alpaca_api_secret": credentialDiagnosticFor(r.AlpacaAPISecret),
		"typesafe_api_key":  credentialDiagnosticFor(r.TypeSafeAPIKey),
	}
}

func defaultRunRequest() runRequest {
	return runRequest{
		Mode: "paper", Market: papertrader.SupportedMarketUS, Exchange: "NASDAQ", Symbol: papertrader.DefaultTradingSymbol, Capital: defaultCapital,
		History1m: defaultHistory1m, History5m: defaultHistory5m, History1d: defaultHistory1d,
		StateBytes: defaultStateBytes, Model: "jev-latest", TimeoutMs: 8000, MaxRetries: 1,
		MaxPositionValuePercent: 25, MaxDailyLossPercent: 1, MaxTradesPerDay: 5,
		StopLossPercent: 1, MaxSpreadPercent: 0.15, MinActionConfidence: 0.60, MinBuySupport: 0.60,
	}
}

func (r runRequest) view() configView {
	return configView{
		Mode: r.Mode, Market: r.Market, Exchange: r.Exchange, Symbol: r.Symbol, PaperOnly: true, Capital: r.Capital,
		History1m: r.History1m, History5m: r.History5m, History1d: r.History1d,
		MaxHistoryBytes: r.StateBytes, Model: r.Model, TimeoutMs: r.TimeoutMs, MaxRetries: r.MaxRetries,
		MaxPositionValuePercent: r.MaxPositionValuePercent, MaxDailyLossPercent: r.MaxDailyLossPercent,
		MaxTradesPerDay: r.MaxTradesPerDay, StopLossPercent: r.StopLossPercent,
		MaxSpreadPercent: r.MaxSpreadPercent, MinActionConfidence: r.MinActionConfidence,
		MinBuySupport: r.MinBuySupport,
		KiteAPIKeySet: r.KiteAPIKey != "", KiteAPISecretSet: r.KiteAPISecret != "",
		KiteAccessTokenSet: r.KiteAccessToken != "", AlpacaAPIKeySet: r.AlpacaAPIKey != "", AlpacaAPISecretSet: r.AlpacaAPISecret != "", TypeSafeAPIKeySet: r.TypeSafeAPIKey != "",
		CredentialDiagnostics: credentialDiagnosticsFor(r),
	}
}

type runnerStatus struct {
	Running      bool      `json:"running"`
	Mode         string    `json:"mode,omitempty"`
	Owned        bool      `json:"owned"`
	PID          int       `json:"pid,omitempty"`
	ExternalPIDs []int     `json:"external_pids,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	LastExit     string    `json:"last_exit,omitempty"`
	ConsoleLog   string    `json:"console_log,omitempty"`
}

type controlManager struct {
	mu       sync.Mutex
	root     string
	runDir   string
	cmd      *exec.Cmd
	started  time.Time
	mode     string
	config   configView
	console  string
	lastExit string
}

func (m *controlManager) start(req runRequest) error {
	if err := validateRunRequest(&req); err != nil {
		return err
	}
	m.mu.Lock()
	if m.cmd != nil && m.cmd.ProcessState == nil {
		m.mu.Unlock()
		return errors.New("a runner is already active")
	}
	m.mu.Unlock()

	binary, err := buildRunner(m.root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.runDir, 0755); err != nil {
		return err
	}
	consolePath := filepath.Join(m.runDir, "control-"+time.Now().UTC().Format("20060102T150405.000000000Z")+"-console.log")
	console, err := os.OpenFile(consolePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	args := runnerArgs(req, m.runDir)
	cmd := exec.Command(binary, args...)
	cmd.Dir = m.root
	cmd.Env = runnerEnv(req)
	cmd.Stdout = console
	cmd.Stderr = console
	if err := cmd.Start(); err != nil {
		_ = console.Close()
		return fmt.Errorf("start paper runner: %w", err)
	}

	m.mu.Lock()
	m.cmd, m.started, m.mode, m.config, m.console = cmd, time.Now(), req.Mode, req.view(), consolePath
	m.lastExit = ""
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		_ = console.Close()
		m.mu.Lock()
		if m.cmd == cmd {
			m.lastExit = exitString(err)
			m.cmd = nil
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *controlManager) stop() []int {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil && cmd.ProcessState == nil {
		_ = cmd.Process.Signal(os.Interrupt)
		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			<-timer.C
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.cmd == cmd && cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
		}()
	}
	return stopExternalPaperProcesses(m.root)
}

func (m *controlManager) status() runnerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := runnerStatus{Mode: m.mode, Owned: false, StartedAt: m.started, LastExit: m.lastExit, ConsoleLog: m.console}
	if m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil {
		status.Running, status.Owned, status.PID = true, true, m.cmd.Process.Pid
	}
	return status
}

func (m *controlManager) configSnapshot() configView {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.Mode == "" {
		return defaultRunRequest().view()
	}
	return m.config
}

func validateRunRequest(r *runRequest) error {
	if r.Mode == "" {
		r.Mode = "paper"
	}
	if r.Mode != "paper" {
		return errors.New("only paper mode is available; live trading is disabled")
	}
	r.Market = strings.ToLower(strings.TrimSpace(r.Market))
	if r.Market != papertrader.SupportedMarketIndia && r.Market != papertrader.SupportedMarketUS {
		return errors.New("market must be india or us")
	}
	r.Exchange = strings.ToUpper(strings.TrimSpace(r.Exchange))
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	if r.Exchange == "" || r.Symbol == "" {
		return errors.New("exchange/venue and symbol are required")
	}
	if r.Capital <= 0 || r.Capital > 1e12 {
		return errors.New("capital must be greater than zero")
	}
	if r.History1m < 0 || r.History1m > 10000 || r.History5m < 0 || r.History5m > 10000 || r.History1d < 0 || r.History1d > 5000 {
		return errors.New("history windows are outside the safe dashboard range")
	}
	if r.StateBytes < 0 || r.StateBytes > 1<<20 {
		return errors.New("max history bytes is outside the safe dashboard range")
	}
	if r.Model == "" {
		r.Model = "jev-latest"
	}
	if r.TimeoutMs <= 0 || r.TimeoutMs > 120000 {
		return errors.New("TypeSafe timeout must be between 1ms and 120000ms")
	}
	if r.MaxRetries < 0 || r.MaxRetries > 5 {
		return errors.New("max retries must be between 0 and 5")
	}
	if r.TypeSafeAPIKey == "" {
		return errors.New("paper mode needs a TypeSafe API key")
	}
	if r.Market == papertrader.SupportedMarketIndia && (r.KiteAPIKey == "" || r.KiteAPISecret == "" || r.KiteAccessToken == "") {
		return errors.New("India paper mode needs Kite API key, API secret, and access token")
	}
	if r.Market == papertrader.SupportedMarketUS && (r.AlpacaAPIKey == "" || r.AlpacaAPISecret == "") {
		return errors.New("US paper mode needs Alpaca API key and secret")
	}
	return nil
}

func runnerArgs(r runRequest, runDir string) []string {
	args := []string{"-paper=true", "-market=" + r.Market, "-exchange=" + r.Exchange, "-symbol=" + r.Symbol, "-capital=" + strconv.FormatFloat(r.Capital, 'f', -1, 64), "-log-dir=" + runDir,
		"-history-1m=" + strconv.Itoa(r.History1m), "-history-5m=" + strconv.Itoa(r.History5m), "-history-1d=" + strconv.Itoa(r.History1d),
		"-max-history-bytes=" + strconv.Itoa(r.StateBytes), "-typesafe-model=" + r.Model,
		"-typesafe-timeout=" + strconv.Itoa(r.TimeoutMs) + "ms", "-typesafe-retries=" + strconv.Itoa(r.MaxRetries),
		"-max-position-value-percent=" + strconv.FormatFloat(r.MaxPositionValuePercent, 'f', -1, 64),
		"-max-daily-loss-percent=" + strconv.FormatFloat(r.MaxDailyLossPercent, 'f', -1, 64),
		"-max-trades-per-day=" + strconv.Itoa(r.MaxTradesPerDay), "-stop-loss-percent=" + strconv.FormatFloat(r.StopLossPercent, 'f', -1, 64),
		"-max-spread-percent=" + strconv.FormatFloat(r.MaxSpreadPercent, 'f', -1, 64),
		"-min-action-confidence=" + strconv.FormatFloat(r.MinActionConfidence, 'f', -1, 64),
		"-min-buy-support=" + strconv.FormatFloat(r.MinBuySupport, 'f', -1, 64)}
	return args
}

func runnerEnv(r runRequest) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "KITE_API_") || strings.HasPrefix(item, "ALPACA_API_") || strings.HasPrefix(item, "TYPESAFE_API_KEY=") {
			continue
		}
		env = append(env, item)
	}
	if r.Mode == "paper" {
		env = append(env, "KITE_API_KEY="+r.KiteAPIKey, "KITE_API_SECRET="+r.KiteAPISecret, "KITE_ACCESS_TOKEN="+r.KiteAccessToken, "ALPACA_API_KEY="+r.AlpacaAPIKey, "ALPACA_API_SECRET="+r.AlpacaAPISecret, "TYPESAFE_API_KEY="+r.TypeSafeAPIKey)
	}
	return env
}

func buildRunner(root string) (string, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ai-paper-trader-%d", os.Getpid()))
	build := exec.Command("go", "build", "-o", path, "./cmd/paper-trader")
	build.Dir = root
	output, err := build.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build paper runner: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return path, nil
}

func exitString(err error) string {
	if err == nil {
		return "exited cleanly"
	}
	return err.Error()
}

func stopExternalPaperProcesses(_ string) []int {
	output, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var stopped []int
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == os.Getpid() {
			continue
		}
		command := strings.TrimSpace(line[len(fields[0]):])
		// The runner is often launched with a relative `go run` command, so its
		// argv does not contain the absolute working directory. Match the
		// isolated module and its explicit paper flag instead of relying on cwd.
		if !strings.Contains(command, "cmd/paper-trader") || !strings.Contains(command, "-paper=true") {
			continue
		}
		if process, err := os.FindProcess(pid); err == nil {
			if process.Signal(os.Interrupt) == nil {
				stopped = append(stopped, pid)
			}
		}
	}
	return stopped
}

type dashboardServer struct {
	root    string
	runDir  string
	control *controlManager
}

func (s *dashboardServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.static)
	mux.HandleFunc("/styles.css", s.static)
	mux.HandleFunc("/app.js", s.static)
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/control/start", s.start)
	mux.HandleFunc("/api/control/stop", s.stop)
	mux.HandleFunc("/api/kite-url", s.kiteURL)
	mux.HandleFunc("/api/kite-token", s.kiteToken)
	return noStore(mux)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *dashboardServer) static(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	data, err := dashboardFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else if strings.HasSuffix(name, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = w.Write(data)
}

func (s *dashboardServer) start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var request runRequest
	defaults := defaultRunRequest()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	normalizeRunCredentials(&request)
	mergeRunDefaults(&request, defaults)
	if err := s.control.start(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "status": s.control.status(), "credential_diagnostics": request.view().CredentialDiagnostics})
}

func normalizeRunCredentials(r *runRequest) {
	r.KiteAPIKey = strings.TrimSpace(r.KiteAPIKey)
	r.KiteAPISecret = strings.TrimSpace(r.KiteAPISecret)
	r.KiteAccessToken = strings.TrimSpace(r.KiteAccessToken)
	r.AlpacaAPIKey = strings.TrimSpace(r.AlpacaAPIKey)
	r.AlpacaAPISecret = strings.TrimSpace(r.AlpacaAPISecret)
	r.TypeSafeAPIKey = strings.TrimSpace(r.TypeSafeAPIKey)
}

func mergeRunDefaults(r *runRequest, d runRequest) {
	if r.Mode == "" {
		r.Mode = d.Mode
	}
	if r.Market == "" {
		r.Market = d.Market
	}
	if r.Exchange == "" {
		r.Exchange = d.Exchange
	}
	if r.Symbol == "" {
		r.Symbol = d.Symbol
	}
	if r.Capital == 0 {
		r.Capital = d.Capital
	}
	if r.Model == "" {
		r.Model = d.Model
	}
}

func (s *dashboardServer) stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "stopped_pids": s.control.stop()})
}

func (s *dashboardServer) kiteURL(w http.ResponseWriter, r *http.Request) {
	apiKey := strings.TrimSpace(r.URL.Query().Get("api_key"))
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}
	writeJSON(w, map[string]string{"url": "https://kite.zerodha.com/connect/login?api_key=" + url.QueryEscape(apiKey) + "&v=3"})
}

// kiteToken exchanges Kite's one-time request_token for the daily access_token.
// The secret is accepted only for this in-memory request and is never logged
// or written to a run file.
func (s *dashboardServer) kiteToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		APIKey       string `json:"api_key"`
		APISecret    string `json:"api_secret"`
		RequestToken string `json:"request_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	input.APIKey, input.APISecret, input.RequestToken = strings.TrimSpace(input.APIKey), strings.TrimSpace(input.APISecret), strings.TrimSpace(input.RequestToken)
	if input.APIKey == "" || input.APISecret == "" || input.RequestToken == "" {
		writeError(w, http.StatusBadRequest, "API key, API secret, and fresh request token are required")
		return
	}
	hash := sha256.Sum256([]byte(input.APIKey + input.RequestToken + input.APISecret))
	form := url.Values{}
	form.Set("api_key", input.APIKey)
	form.Set("request_token", input.RequestToken)
	form.Set("checksum", hex.EncodeToString(hash[:]))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://api.kite.trade/session/token", strings.NewReader(form.Encode()))
	if err != nil {
		writeError(w, http.StatusBadGateway, "build Kite token request: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Kite token exchange failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "read Kite token response: "+err.Error())
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var kiteError struct {
			Message string `json:"message"`
		}
		message := "Kite rejected the token exchange"
		if json.Unmarshal(body, &kiteError) == nil && kiteError.Message != "" {
			message = kiteError.Message
		}
		writeError(w, http.StatusBadGateway, message)
		return
	}
	var result struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Data.AccessToken == "" {
		writeError(w, http.StatusBadGateway, "Kite returned no access token")
		return
	}
	writeJSON(w, map[string]string{"access_token": result.Data.AccessToken})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]interface{}{"ok": false, "error": message})
}

func writeJSON(w http.ResponseWriter, value interface{}) { writeJSONStatus(w, http.StatusOK, value) }
func writeJSONStatus(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type stateResponse struct {
	PaperOnly    bool                        `json:"paper_only"`
	ServerTime   time.Time                   `json:"server_time"`
	UpdatedAt    time.Time                   `json:"updated_at,omitempty"`
	RunFile      string                      `json:"run_file,omitempty"`
	MarketFile   string                      `json:"market_file,omitempty"`
	Latest       *papertrader.DecisionEvent  `json:"latest,omitempty"`
	MarketLatest *papertrader.MarketEvent    `json:"market_latest,omitempty"`
	Events       []papertrader.DecisionEvent `json:"events"`
	Market       []papertrader.MarketEvent   `json:"market"`
	Trades       []papertrader.DecisionEvent `json:"trades"`
	Runs         []map[string]interface{}    `json:"runs"`
	Console      []string                    `json:"console"`
	Metrics      stateMetrics                `json:"metrics"`
	Runner       runnerStatus                `json:"runner"`
	Config       configView                  `json:"config"`
}

type stateMetrics struct {
	Decisions     int       `json:"decisions"`
	Errors        int       `json:"errors"`
	Trades        int       `json:"trades"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	AICostUSD     float64   `json:"ai_cost_usd"`
	CostKnown     bool      `json:"ai_cost_known"`
	DailyPnL      float64   `json:"daily_pnl"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	Position      string    `json:"position"`
	Quantity      int       `json:"quantity"`
	WinRate       float64   `json:"win_rate"`
	LastPrice     float64   `json:"last_price"`
	Bid           float64   `json:"bid"`
	Ask           float64   `json:"ask"`
	LastQuantity  int64     `json:"last_quantity"`
	TickTimestamp time.Time `json:"tick_timestamp,omitempty"`
	LatencyMs     float64   `json:"latency_ms"`
	StateBytes    int       `json:"state_bytes"`
	History1m     int       `json:"history_1m"`
	History5m     int       `json:"history_5m"`
	History1d     int       `json:"history_1d"`
}

func (s *dashboardServer) state(w http.ResponseWriter, r *http.Request) {
	logLines := 80
	if raw := r.URL.Query().Get("log_lines"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			logLines = parsed
		}
	}
	if logLines < 20 {
		logLines = 20
	}
	if logLines > 1000 {
		logLines = 1000
	}
	response := s.readState(logLines)
	writeJSON(w, response)
}

func (s *dashboardServer) readState(logLineLimit ...int) stateResponse {
	maxLogLines := 80
	if len(logLineLimit) > 0 && logLineLimit[0] > 0 {
		maxLogLines = logLineLimit[0]
	}
	response := stateResponse{PaperOnly: true, ServerTime: time.Now(), Events: []papertrader.DecisionEvent{}, Market: []papertrader.MarketEvent{}, Trades: []papertrader.DecisionEvent{}, Runs: []map[string]interface{}{}, Console: []string{}, Config: configView{PaperOnly: true}}
	response.Runner = s.control.status()
	response.Config = s.control.configSnapshot()
	response.Console = s.readConsole(response.Runner.ConsoleLog, maxLogLines)

	eventPath, eventMod := newestFile(s.runDir, "paper-*-events.jsonl")
	if eventPath != "" {
		response.RunFile = filepath.Base(eventPath)
		response.UpdatedAt = eventMod
		response.Events = readEvents(eventPath)
		if len(response.Events) > 0 {
			latest := response.Events[len(response.Events)-1]
			response.Latest = &latest
			response.Metrics = metricsFromEvents(response.Events)
		}
	}
	marketPath, marketMod := newestFile(s.runDir, "*-market.jsonl")
	if marketPath != "" {
		response.MarketFile = filepath.Base(marketPath)
		response.Market = readMarket(marketPath)
		if len(response.Market) > 0 {
			latestMarket := response.Market[len(response.Market)-1]
			response.MarketLatest = &latestMarket
			if latestMarket.Tick.LastPrice > 0 {
				response.Metrics.LastPrice = latestMarket.Tick.LastPrice
			}
			response.Metrics.Position = latestMarket.Position.Side
			response.Metrics.Quantity = latestMarket.Position.Quantity
			response.Metrics.DailyPnL = latestMarket.Account.DailyPnL
			response.Metrics.UnrealizedPnL = latestMarket.Position.Unrealized
			response.Metrics.Bid = latestMarket.Tick.Bid
			response.Metrics.Ask = latestMarket.Tick.Ask
			response.Metrics.LastQuantity = latestMarket.Tick.LastQuantity
			response.Metrics.TickTimestamp = latestMarket.Timestamp
		}
		if marketMod.After(response.UpdatedAt) {
			response.UpdatedAt = marketMod
		}
	}
	for _, event := range response.Events {
		if event.Intent != nil && event.Intent.Action != papertrader.ActionNoAction {
			response.Trades = append(response.Trades, event)
		}
	}
	response.Runs = readSummaries(s.runDir)
	if len(response.Runs) > 0 && response.Config.Mode == "" {
		response.Config.Mode = "paper"
	}
	if response.UpdatedAt.IsZero() {
		response.UpdatedAt = response.ServerTime
	}
	if response.Runner.Running == false && response.UpdatedAt.After(time.Now().Add(-externalGrace)) {
		pids := externalPaperPIDs(s.root)
		if len(pids) > 0 {
			response.Runner.Running = true
			response.Runner.Mode = "paper"
			response.Runner.ExternalPIDs = pids
		}
	}
	return response
}

func metricsFromEvents(events []papertrader.DecisionEvent) stateMetrics {
	var m stateMetrics
	var wins int
	for _, event := range events {
		m.Decisions++
		if event.Error != "" {
			m.Errors++
		}
		if event.Intent != nil && event.Intent.Action != papertrader.ActionNoAction {
			m.Trades++
		}
		if event.Intent != nil && event.Intent.Action == papertrader.ActionExit && event.DailyPnL >= 0 {
			wins++
		}
		m.InputTokens += event.InputTokens
		m.OutputTokens += event.OutputTokens
		if event.AICostUSD != nil {
			m.AICostUSD += *event.AICostUSD
			m.CostKnown = true
		}
		m.DailyPnL, m.Position, m.Quantity, m.LastPrice = event.DailyPnL, event.PositionSide, event.PositionQuantity, event.Price
		m.LatencyMs, m.StateBytes = event.LatencyMilliseconds, event.StateBytes
		m.History1m, m.History5m, m.History1d = event.HistoryOneMinuteBars, event.HistoryFiveMinuteBars, event.HistoryDailyBars
		if m.LastPrice == 0 {
			m.LastPrice = event.CurrentBar.Close
		}
	}
	if m.Trades > 0 {
		m.WinRate = float64(wins) / float64(m.Trades)
	}
	return m
}

func readEvents(path string) []papertrader.DecisionEvent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	events := make([]papertrader.DecisionEvent, 0, maxEventLines)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 32<<10), 2<<20)
	for scanner.Scan() {
		var event papertrader.DecisionEvent
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			events = append(events, event)
			if len(events) > maxEventLines {
				events = events[1:]
			}
		}
	}
	return events
}

func readMarket(path string) []papertrader.MarketEvent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	// Market logs can run for an entire session at tick frequency. Seek near
	// the tail before scanning so the one-second dashboard poll does not reread
	// the entire file forever. Keep a generous byte window for full-depth ticks.
	if info, statErr := file.Stat(); statErr == nil && info.Size() > 16<<20 {
		if _, seekErr := file.Seek(info.Size()-(16<<20), io.SeekStart); seekErr == nil {
			_, _ = bufio.NewReader(file).ReadString('\n')
		}
	}
	events := make([]papertrader.MarketEvent, 0, maxMarketLines)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 32<<10), 2<<20)
	for scanner.Scan() {
		var event papertrader.MarketEvent
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			events = append(events, event)
			if len(events) > maxMarketLines {
				events = events[1:]
			}
		}
	}
	return events
}

func readSummaries(dir string) []map[string]interface{} {
	paths, _ := filepath.Glob(filepath.Join(dir, "paper-*-summary.json"))
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			items = append(items, item{path, info.ModTime()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	if len(items) > 20 {
		items = items[:20]
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		data, err := os.ReadFile(item.path)
		if err != nil {
			continue
		}
		var value map[string]interface{}
		if json.Unmarshal(data, &value) == nil {
			if _, ok := value["mode"]; !ok {
				value["mode"] = "paper"
			}
			value["file"] = filepath.Base(item.path)
			value["updated_at"] = item.mod
			result = append(result, value)
		}
	}
	return result
}

func newestFile(dir, pattern string) (string, time.Time) {
	paths, _ := filepath.Glob(filepath.Join(dir, pattern))
	var newest string
	var latest time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.ModTime().After(latest) {
			newest, latest = path, info.ModTime()
		}
	}
	return newest, latest
}

func (s *dashboardServer) readConsole(preferred string, maxLines int) []string {
	path := preferred
	if path == "" {
		path, _ = newestFile(s.runDir, "*-console.log")
	}
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if maxLines <= 0 {
		maxLines = 80
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func externalPaperPIDs(_ string) []int {
	output, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == os.Getpid() {
			continue
		}
		command := strings.TrimSpace(line[len(fields[0]):])
		if strings.Contains(command, "cmd/paper-trader") && strings.Contains(command, "-paper=true") {
			pids = append(pids, pid)
		}
	}
	return pids
}

func main() {
	addr := defaultAddr
	runDir := "runs"
	root, _ := os.Getwd()
	if value := os.Getenv("PAPER_TRADER_ADDR"); value != "" {
		addr = value
	}
	if value := os.Getenv("PAPER_TRADER_RUN_DIR"); value != "" {
		runDir = value
	}
	if !filepath.IsAbs(runDir) {
		runDir = filepath.Join(root, runDir)
	}
	if err := os.MkdirAll(runDir, 0755); err != nil {
		panic(err)
	}
	server := &dashboardServer{root: root, runDir: runDir}
	server.control = &controlManager{root: root, runDir: runDir}
	fmt.Printf("AI paper trader dashboard: http://%s\n", addr)
	if err := http.ListenAndServe(addr, server.routes()); err != nil {
		panic(err)
	}
}
