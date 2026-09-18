(() => {
  const $ = (id) => document.getElementById(id);
  let state = null;
  let hasLocalForm = false;
  let localMarketChosen = false;

  const html = (value) => String(value ?? '').replace(/[&<>'"]/g, (char) => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[char]));
  const number = (value, digits = 2) => Number.isFinite(Number(value)) ? Number(value).toLocaleString(String(document.querySelector('[name="market"]')?.value || 'india').toLowerCase() === 'us' ? 'en-US' : 'en-IN', { maximumFractionDigits: digits }) : '—';
  const inr = (value) => (String(state?.config?.market || document.querySelector('[name="market"]')?.value || 'india').toLowerCase() === 'us' ? `$${number(value, 2)}` : `₹${number(value, 2)}`);
  const pct = (value) => `${(Number(value || 0) * 100).toFixed(0)}%`;
  const istOptions = { timeZone: 'Asia/Kolkata', hour12: true };
  const time = (value) => {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    return `${date.toLocaleTimeString('en-IN', {...istOptions, hour: '2-digit', minute: '2-digit', second: '2-digit'})} IST`;
  };
  const dateTime = (value) => {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '—';
    return `${date.toLocaleString('en-IN', {...istOptions, month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit'})} IST`;
  };
  const actionClass = (action) => String(action || '').replace(/[^a-z_]/g, '');
  const formStorageKey = 'ai-paper-trader.form.v1';
  const credentialFields = ['kite_api_key', 'kite_api_secret', 'kite_access_token', 'alpaca_api_key', 'alpaca_api_secret', 'typesafe_api_key'];
  const credentialFieldsByMarket = {
    india: ['kite_api_key', 'kite_api_secret', 'kite_access_token', 'typesafe_api_key'],
    us: ['alpaca_api_key', 'alpaca_api_secret', 'typesafe_api_key'],
  };

  function selectedMarket() { return String(document.querySelector('[name="market"]')?.value || 'india').toLowerCase(); }
  function requiredCredentialFields(market = selectedMarket()) { return credentialFieldsByMarket[market] || credentialFieldsByMarket.india; }
  function credentialValues(values, market = selectedMarket()) {
    const stored = values?.credentials?.[market];
    if (stored) return stored;
    // Migrate values saved by the earlier single-bucket UI into the active
    // market bucket the first time this page is opened.
    const migrated = {};
    requiredCredentialFields(market).forEach((key) => { if (values?.[key]) migrated[key] = values[key]; });
    return migrated;
  }
  function restoreCredentialFields(values, market = selectedMarket()) {
    const stored = credentialValues(values, market);
    requiredCredentialFields(market).forEach((key) => {
      const input = document.querySelector(`[name="${key}"]`);
      if (input) {
        // Switching tabs should never leave another market's secret in the
        // active form. Saved credentials are restored only for this market.
        input.value = String(stored[key] || '');
        input.type = 'password';
      }
    });
  }

  function setMarket(market, resetFields = true, source = 'system') {
    market = market === 'us' ? 'us' : 'india';
    if (source === 'user') localMarketChosen = true;
    const input = document.querySelector('[name="market"]');
    const previous = input?.value;
    if (input) input.value = market;
    document.querySelectorAll('.market-tab').forEach((button) => { const active = button.dataset.market === market; button.classList.toggle('active', active); button.setAttribute('aria-selected', active ? 'true' : 'false'); });
    $('marketLabel').value = market === 'us' ? 'US' : 'India';
    $('indiaCredentials').hidden = market !== 'india';
    $('usCredentials').hidden = market !== 'us';
    $('kiteActions').hidden = market !== 'india';
    $('requestTokenWrap').hidden = market !== 'india';
    $('usProviderNote').hidden = market !== 'us';
    const exchange = document.querySelector('[name="exchange"]');
    const symbol = document.querySelector('[name="symbol"]');
    if (resetFields && previous && previous !== market) {
      exchange.value = market === 'us' ? 'NASDAQ' : 'NSE';
      symbol.value = market === 'us' ? 'AAPL' : 'INFY';
    }
    if (source === 'user' || source === 'restore') {
      try { restoreCredentialFields(JSON.parse(localStorage.getItem(formStorageKey) || '{}'), market); } catch (_) {}
    }
    $('topTicker').value = symbol?.value || '';
    $('setupGuide').innerHTML = market === 'us'
      ? '<strong>US setup</strong><span>1. Create a free Alpaca paper account and copy its paper API key and secret. 2. Enter a US venue and ticker (for example NASDAQ / AAPL). 3. Enter the TypeSafe key and save credentials. 4. Click Run paper loop. Free real-time coverage is IEX; historical bars use Alpaca’s IEX feed.</span>'
      : '<strong>India setup</strong><span>1. Create a Kite Connect app and set its redirect URL. 2. Generate the login URL below and sign in. 3. Copy the fresh <em>request_token</em> from the redirect, exchange it for the daily access token, then save credentials. 4. Choose a ticker and click Run paper loop. Kite tokens are API-key-specific and usually expire each day.</span>';
    if (resetFields) persistFormValues();
  }

  function credentialInputs() {
    return requiredCredentialFields().map((key) => document.querySelector(`[name="${key}"]`)).filter(Boolean);
  }

  function updateCredentialStatus() {
    const status = $('credentialStatus');
    if (!status) return;
    let values = {};
    try { values = JSON.parse(localStorage.getItem(formStorageKey) || '{}'); } catch (_) {}
    const required = requiredCredentialFields();
    const stored = credentialValues(values);
    const saved = values.remember_credentials === true && required.every((key) => String(stored[key] || '').trim());
    const entered = required.every((key) => String(document.querySelector(`[name="${key}"]`)?.value || '').trim());
    status.textContent = saved ? 'Saved locally · values hidden' : entered ? 'Entered · not saved' : 'Not saved in this browser';
  }

  function toggleCredentials() {
    const inputs = credentialInputs();
    const revealing = inputs.some((input) => input.type === 'password');
    inputs.forEach((input) => { input.type = revealing ? 'text' : 'password'; });
    $('showCredentialsButton').textContent = revealing ? 'Hide values' : 'Show values';
  }

  function persistFormValues(includeCredentials = false) {
    try {
      hasLocalForm = true;
      let values = {};
      try { values = JSON.parse(localStorage.getItem(formStorageKey) || '{}'); } catch (_) {}
      document.querySelectorAll('#runForm [name]').forEach((input) => {
        if (!credentialFields.includes(input.name)) values[input.name] = input.value;
      });
      const rememberCredentials = includeCredentials || $('rememberCredentials').checked;
      values.remember_credentials = rememberCredentials;
      values.action_filter = $('actionFilter').value;
      values.regime_filter = $('regimeFilter').value;
      values.confidence_filter = $('confidenceFilter').value;
      values.log_lines = $('logLines').value;
      values.credentials = values.credentials || {};
      if (includeCredentials) {
        values.credentials[selectedMarket()] = {};
        requiredCredentialFields().forEach((key) => { values.credentials[selectedMarket()][key] = String(document.querySelector(`[name="${key}"]`)?.value || '').trim(); });
      } else if (!rememberCredentials) {
        delete values.credentials[selectedMarket()];
      }
      credentialFields.forEach((key) => { delete values[key]; });
      localStorage.setItem(formStorageKey, JSON.stringify(values));
      updateCredentialStatus();
    } catch (_) {
      // Private browsing or a disabled storage API should not block a run.
    }
  }

  function restoreFormValues() {
    // Some embedded browser contexts restore native form values before the
    // page script runs but do not expose localStorage. Upgrade that legacy
    // 16k value before attempting any storage reads.
    const historyBudget = document.querySelector('#runForm [name="max_history_bytes"]');
    const upgradeLegacyHistoryBudget = () => {
      if (historyBudget && Number(historyBudget.value) === 16000) historyBudget.value = '32000';
    };
    upgradeLegacyHistoryBudget();
    // Chrome can restore form controls after scripts run; check once more
    // after that restoration pass so the new default is visible immediately.
    setTimeout(upgradeLegacyHistoryBudget, 0);
    setTimeout(upgradeLegacyHistoryBudget, 250);
    try {
      const values = JSON.parse(localStorage.getItem(formStorageKey) || '{}');
      hasLocalForm = Object.keys(values).length > 0;
      localMarketChosen = Boolean(values.market);
      const migrateHistoryBudget = Number(values.max_history_bytes) === 16000;
      Object.entries(values).forEach(([key, value]) => {
        if (key === 'remember_credentials' || key === 'credentials' || credentialFields.includes(key)) return;
        const input = document.querySelector(`#runForm [name="${key}"]`);
        if (input) input.value = value;
      });
      $('rememberCredentials').checked = values.remember_credentials === true;
      if (values.action_filter) $('actionFilter').value = values.action_filter;
      if (values.regime_filter) $('regimeFilter').value = values.regime_filter;
      if (values.confidence_filter != null) $('confidenceFilter').value = values.confidence_filter;
      if (values.log_lines) $('logLines').value = values.log_lines;
      // Migrate the old 16k default once, including browsers that restored the
      // field value but did not expose the saved object consistently.
      if (historyBudget && (migrateHistoryBudget || Number(historyBudget.value) === 16000)) {
        historyBudget.value = '32000';
        values.max_history_bytes = '32000';
        localStorage.setItem(formStorageKey, JSON.stringify(values));
      }
      const restoredMarket = values.market || 'us';
      setMarket(restoredMarket, false, 'restore');
      // Upgrade the pre-market-split credential format once, preserving the
      // selected market and removing the old flat secret fields.
      if (!values.credentials) {
        const migratedCredentials = credentialValues(values, restoredMarket);
        if (Object.keys(migratedCredentials).length) {
          values.credentials = { [restoredMarket]: migratedCredentials };
          credentialFields.forEach((key) => { delete values[key]; });
          localStorage.setItem(formStorageKey, JSON.stringify(values));
        }
      }
      restoreCredentialFields(values);
      updateCredentialStatus();
    } catch (_) {
      // Ignore malformed or unavailable local state and keep safe defaults.
    }
  }

  function normalizeLegacyHistoryBudgetInput() {
    const input = document.querySelector('#runForm [name="max_history_bytes"]');
    if (input && Number(input.value) === 16000) input.value = '32000';
  }

  function clearSavedValues() {
    try { localStorage.removeItem(formStorageKey); } catch (_) {}
    document.querySelectorAll('#runForm [name]').forEach((input) => { input.value = ''; });
    credentialFields.forEach((key) => { const input = document.querySelector(`[name="${key}"]`); if (input) { input.value = ''; input.type = 'password'; } });
    $('showCredentialsButton').textContent = 'Show values';
    $('rememberCredentials').checked = false;
    $('actionFilter').value = 'all';
    $('regimeFilter').value = 'all';
    $('confidenceFilter').value = '0';
    $('logLines').value = '80';
    $('kiteUrlResult').textContent = '';
    setMarket('us', true, 'system');
    document.querySelector('[name="exchange"]').value = 'NASDAQ';
    document.querySelector('[name="symbol"]').value = 'AAPL';
    $('topTicker').value = 'AAPL';
    hasLocalForm = true;
    localMarketChosen = true;
    updateCredentialStatus();
    showMessage('Saved dashboard values cleared from this browser.', true);
  }

  function saveCredentials() {
    const missing = requiredCredentialFields().filter((key) => !document.querySelector(`[name="${key}"]`).value.trim());
    if (missing.length) {
      showMessage(`Enter ${missing.length} required ${selectedMarket() === 'us' ? 'Alpaca/TypeSafe' : 'Kite/TypeSafe'} credential(s) before saving them.`, false);
      return;
    }
    $('rememberCredentials').checked = true;
    persistFormValues(true);
    updateCredentialStatus();
    showMessage('Credentials saved locally in this browser. They are not sent back by the dashboard API.', true);
  }

  async function getState() {
    const logLines = Number($('logLines')?.value || 80);
    const response = await fetch(`/api/state?log_lines=${encodeURIComponent(logLines)}`, { cache: 'no-store' });
    if (!response.ok) throw new Error(`state ${response.status}`);
    return response.json();
  }

  async function refresh() {
    try {
      state = await getState();
      render(state);
      $('controlMessage').className = 'control-message';
    } catch (error) {
      $('streamText').textContent = 'Dashboard API unavailable';
      $('runnerBadge').textContent = 'Offline';
      $('runnerBadge').className = 'badge danger';
      $('controlMessage').textContent = error.message;
      $('controlMessage').className = 'control-message error';
    }
  }

  function render(data) {
    const metrics = data.metrics || {};
    const latest = data.latest || {};
    const live = data.market_latest || latest;
    const liveTick = live.tick || {};
    const runner = data.runner || {};
    const config = data.config || {};
    const running = Boolean(runner.running);
    // A stopped dashboard may have a server-side default from an older run.
    // Once the user has selected/edited a local form value, never overwrite it
    // on the one-second refresh; a running process remains the source of truth.
    // The browser's saved form is authoritative after the first edit. The
    // API state may still expose the runner's defaults, so never let a
    // one-second refresh switch the selected market or ticker back.
    const syncServerConfig = !hasLocalForm;
    if (syncServerConfig && config.market) setMarket(config.market, false, 'system');
    if (syncServerConfig && config.symbol && document.querySelector('[name="symbol"]')) { document.querySelector('[name="symbol"]').value = config.symbol; $('topTicker').value = config.symbol; }
    if (syncServerConfig && config.exchange && document.querySelector('[name="exchange"]')) document.querySelector('[name="exchange"]').value = config.exchange;
    $('runnerBadge').textContent = running ? `Running · ${runner.mode || 'paper'}` : 'Stopped';
    $('runnerBadge').className = `badge ${running ? 'success' : 'neutral'}`;
    $('streamText').textContent = running ? (data.market_latest ? 'Live tick stream · AI every minute' : (runner.owned ? 'Runner process connected' : 'Paper stream detected')) : 'No runner process';
    $('lastUpdated').textContent = data.updated_at ? `updated ${time(data.updated_at)}` : 'waiting';
    $('footerClock').textContent = `last checked ${time(data.server_time)}`;
    $('lastPrice').textContent = metrics.last_price ? inr(metrics.last_price) : '—';
    const quoteHint = metrics.bid && metrics.ask ? `bid ${inr(metrics.bid)} · ask ${inr(metrics.ask)}` : (metrics.last_quantity ? `tick qty ${number(metrics.last_quantity, 0)}` : 'quote pending');
    $('priceTime').textContent = live.timestamp ? `${dateTime(live.timestamp)} · ${quoteHint}` : 'No quote yet';
    $('dailyPnl').textContent = inr(metrics.daily_pnl || 0);
    $('dailyPnl').style.color = Number(metrics.daily_pnl || 0) < 0 ? 'var(--red)' : Number(metrics.daily_pnl || 0) > 0 ? 'var(--green)' : '';
    const unrealized = Number(metrics.unrealized_pnl || 0);
    $('pnlHint').textContent = `${number(metrics.trades || 0, 0)} paper intents · live ${inr(unrealized)} unrealized`;
    $('position').textContent = String(metrics.position || 'flat').toUpperCase();
    $('positionQty').textContent = `${number(metrics.quantity || 0, 0)} units · tick ${time(metrics.tick_timestamp || live.timestamp)}`;
    $('aiAction').textContent = latest.action || '—';
    $('aiAction').className = `action ${actionClass(latest.action)}`;
    $('aiConfidence').textContent = `AI ${latest.timestamp ? time(latest.timestamp) : '—'} · ${latest.action_confidence == null ? 'confidence —' : pct(latest.action_confidence)}`;
    $('latency').textContent = latest.latency_ms == null ? '—' : `${number(latest.latency_ms, 0)} ms`;
    const runCost = metrics.ai_cost_known ? `$${Number(metrics.ai_cost_usd || 0).toFixed(6)} run` : 'Cost pending';
    $('payload').textContent = latest.state_bytes ? `State ${number(latest.state_bytes, 0)} B · ${runCost}` : runCost;
    $('decisionCount').textContent = number(metrics.decisions || 0, 0);
    $('errorCount').textContent = `${number(metrics.errors || 0, 0)} errors`;
    $('runFile').textContent = data.run_file || 'No event file';
    renderCredentialDiagnostics(config);
    $('candleTime').textContent = latest.current_bar ? dateTime(latest.current_bar.timestamp) : '—';
    const quote = data.market_latest ? liveTick : (latest.quote || {});
    renderKeyValues('candleData', [
      ['Open', latest.current_bar?.open ? inr(latest.current_bar.open) : '—'],
      ['High', latest.current_bar?.high ? inr(latest.current_bar.high) : '—'],
      ['Low', latest.current_bar?.low ? inr(latest.current_bar.low) : '—'],
      ['Close', latest.current_bar?.close ? inr(latest.current_bar.close) : '—'],
      ['Volume', latest.current_bar?.volume ? number(latest.current_bar.volume, 0) : '—'],
      ['Bid / ask', quote.bid && quote.ask ? `${inr(quote.bid)} / ${inr(quote.ask)}` : '—'],
      ['Buy qty', quote.buy_quantity ? number(quote.buy_quantity, 0) : '—'],
      ['Sell qty', quote.sell_quantity ? number(quote.sell_quantity, 0) : '—'],
      ['Position', `${String(metrics.position || 'flat').toUpperCase()} · ${number(metrics.quantity || 0, 0)}`],
    ]);
    renderKeyValues('historyData', [
      ['1m bars', number(latest.history_1m_bars || 0, 0)],
      ['5m bars', number(latest.history_5m_bars || 0, 0)],
      ['Daily bars', number(latest.history_1d_bars || 0, 0)],
      ['Payload', latest.state_bytes ? `${number(latest.state_bytes, 0)} B` : '—'],
      ['Model', config.model || 'jev-latest'],
      ['Cadence', '1 completed minute'],
      ['Decision config', config.paper_only ? `paper · max ${number(config.max_position_value_percent, 1)}% · min ${pct(config.min_action_confidence)} / ${pct(config.min_buy_support)}` : '—'],
      ['Context budget', config.max_history_bytes ? `${number(config.max_history_bytes, 0)} B history cap · 64k request / 32k state tokens` : '—'],
    ]);
    renderDecisions(data.events || []);
    renderTrades(data.trades || []);
    renderRuns(data.runs || []);
    $('consoleLog').textContent = (data.console || []).join('\n') || 'No console output yet.';
    drawCharts(data.events || [], data.market || []);
    if (!running) $('startButton').disabled = false;
  }

  function renderKeyValues(id, values) {
    $(id).innerHTML = values.map(([key, value]) => `<div><dt>${html(key)}</dt><dd>${html(value)}</dd></div>`).join('');
  }

  function renderCredentialDiagnostics(config) {
    const target = $('credentialDiagnostics');
    if (!target) return;
    const diagnostics = config.credential_diagnostics || {};
    const labels = { kite_api_key: 'Kite key', kite_api_secret: 'Kite secret', kite_access_token: 'Kite token', alpaca_api_key: 'Alpaca key', alpaca_api_secret: 'Alpaca secret', typesafe_api_key: 'TypeSafe key' };
    const values = Object.keys(labels).map((key) => {
      const value = diagnostics[key] || {};
      if (!value.set) return `${labels[key]} missing`;
      return `${labels[key]} received · ${value.length} chars · sha256 ${value.sha256_12 || '—'}`;
    });
    target.textContent = `API received: ${values.join(' · ')}`;
  }

  function filteredEvents(events) {
    const action = $('actionFilter').value;
    const regime = $('regimeFilter').value;
    const minimum = Number($('confidenceFilter').value || 0);
    return events.filter((event) => (action === 'all' || event.action === action) && (regime === 'all' || event.market_regime === regime) && Number(event.action_confidence || 0) >= minimum);
  }

  function renderDecisions(events) {
    const rows = filteredEvents(events).slice().reverse().slice(0, 100);
    $('decisionRows').innerHTML = rows.length ? rows.map((event) => `<tr>
      <td>${html(dateTime(event.timestamp))}</td><td class="action ${actionClass(event.action)}">${html(event.action || '—')}</td>
      <td>${pct(event.action_confidence)}</td><td>${pct(event.buy_support)}</td><td>${html(event.market_regime || '—')}</td>
      <td>${event.price ? html(inr(event.price)) : '—'}</td><td>${event.latency_ms == null ? '—' : html(number(event.latency_ms, 1) + ' ms')}</td>
      <td>${event.ai_cost_usd == null ? (event.input_tokens ? '—' : 'No API call') : html('$' + Number(event.ai_cost_usd).toFixed(6))}${event.input_tokens ? `<small class="cell-note">${html(number(event.input_tokens, 0))} in · ${html(number(event.output_tokens || 0, 0))} out</small>` : ''}</td>
      <td class="${event.error ? 'error-text' : ''}">${html(event.error ? 'Rejected: ' + event.error : event.intent ? (event.intent.action === 'no_action' ? 'Observed' : 'Authorized') : 'Observed')}</td>
    </tr>`).join('') : '<tr><td colspan="9" class="empty">No decisions match the current filters.</td></tr>';
  }

  function renderTrades(events) {
    const rows = events.slice().reverse().slice(0, 100);
    $('tradeRows').innerHTML = rows.length ? rows.map((event) => {
      const intent = event.intent || {};
      return `<tr><td>${html(dateTime(event.timestamp))}</td><td class="action ${actionClass(intent.action)}">${html(intent.action || '—')}</td><td>${number(intent.quantity, 0)}</td><td>${intent.price ? html(inr(intent.price)) : '—'}</td><td>${intent.stop_loss ? html(inr(intent.stop_loss)) : '—'}</td><td>${html(String(event.position_side || 'flat').toUpperCase())} · ${number(event.position_quantity, 0)}</td><td>${html(inr(event.daily_pnl || 0))}</td></tr>`;
    }).join('') : '<tr><td colspan="7" class="empty">No paper trades yet. TypeSafe may be returning no_action or risk rules may be rejecting entries.</td></tr>';
  }

  function renderRuns(runs) {
    $('runRows').innerHTML = runs.length ? runs.map((run) => {
      const account = run.account || {};
      const daily = run.daily_pnl ?? account.daily_pnl ?? 0;
      return `<tr><td>${html(run.run_id || run.file || '—')}</td><td>${html(run.market || '—')}</td><td>${html(run.symbol || '—')}</td><td>${number(run.decisions ?? '—', 0)}</td><td>${number(run.trades ?? account.trades_today ?? '—', 0)}</td><td>${html(inr(daily))}</td><td>${html(dateTime(run.updated_at))}</td></tr>`;
    }).join('') : '<tr><td colspan="6" class="empty">No saved summaries yet. The active run writes its summary on stop.</td></tr>';
  }

  const chartState = { allValues: [], values: [], window: 300, pan: 0, hover: null, hoverKind: null, drag: null, fromDate: '', toDate: '' };

  function drawCharts(events, market) {
    chartState.allValues = market.length
      ? market.map((event) => ({ price: Number(event.tick?.last_price || 0), volume: Number(event.tick?.last_quantity || 0), time: event.timestamp, bid: Number(event.tick?.bid || 0), ask: Number(event.tick?.ask || 0), buyQuantity: Number(event.tick?.buy_quantity || 0), sellQuantity: Number(event.tick?.sell_quantity || 0) })).filter((event) => event.price > 0)
      : events.map((event) => ({ price: Number(event.price || event.current_bar?.close || 0), volume: Number(event.current_bar?.volume || 0), time: event.timestamp, bid: Number(event.quote?.bid || 0), ask: Number(event.quote?.ask || 0), buyQuantity: Number(event.quote?.buy_quantity || 0), sellQuantity: Number(event.quote?.sell_quantity || 0) })).filter((event) => event.price > 0);
    chartState.values = filterChartDates(chartState.allValues);
    chartState.pan = clamp(chartState.pan, 0, Math.max(0, chartState.values.length - chartState.window));
    renderInteractiveCharts();
  }

  function filterChartDates(values) {
    return values.filter((value) => {
      const key = istDateKey(value.time);
      return (!chartState.fromDate || key >= chartState.fromDate) && (!chartState.toDate || key <= chartState.toDate);
    });
  }

  function istDateKey(value) {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Kolkata', year: 'numeric', month: '2-digit', day: '2-digit' }).format(date);
  }

  function chartView() {
    const end = Math.max(0, chartState.values.length - chartState.pan);
    const start = Math.max(0, end - Math.min(chartState.window, end));
    return { values: chartState.values.slice(start, end), start };
  }

  function renderInteractiveCharts() {
    const view = chartView();
    const hover = chartState.hover == null || chartState.hover < view.start || chartState.hover >= view.start + view.values.length ? null : chartState.hover - view.start;
    drawLine($('priceChart'), view.values, '#111113', hover);
    drawBars($('volumeChart'), view.values, hover);
    document.querySelectorAll('[data-chart-window]').forEach((button) => button.classList.toggle('active', Number(button.dataset.chartWindow) === chartState.window));
    const prices = view.values.map((value) => value.price);
    const volumes = view.values.map((value) => value.volume);
    $('priceRange').textContent = prices.length ? `${inr(Math.min(...prices))} — ${inr(Math.max(...prices))} · ${view.values.length} ticks` : 'No loaded ticks';
    $('volumeRange').textContent = volumes.length ? `${number(Math.max(...volumes), 0)} max tick quantity · ${view.values.length} ticks` : 'No loaded ticks';
    updateChartTooltip();
  }

  function setupCanvas(canvas) {
    const rect = canvas.getBoundingClientRect();
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(rect.width * ratio));
    canvas.height = Math.max(1, Math.floor(rect.height * ratio));
    const context = canvas.getContext('2d');
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    return { context, width: rect.width, height: rect.height };
  }

  function drawLine(canvas, values, color, hoverIndex) {
    const { context, width, height } = setupCanvas(canvas);
    context.clearRect(0, 0, width, height);
    if (!values.length) { drawEmpty(context, width, height); return; }
    drawGrid(context, width, height);
    const prices = values.map((value) => value.price);
    const min = Math.min(...prices), max = Math.max(...prices), span = max - min || 1;
    context.beginPath(); context.strokeStyle = color; context.lineWidth = 1.7;
    values.forEach((value, index) => { const x = chartX(index, values.length, width); const y = height - 9 - (height - 18) * ((value.price - min) / span); index ? context.lineTo(x, y) : context.moveTo(x, y); });
    context.stroke();
    drawCrosshair(context, width, height, values, hoverIndex);
    context.fillStyle = '#77777b'; context.font = '10px sans-serif'; context.fillText(`₹${min.toFixed(0)}`, 8, height - 2); context.fillText(`₹${max.toFixed(0)}`, 8, 11);
  }

  function drawBars(canvas, values, hoverIndex) {
    const { context, width, height } = setupCanvas(canvas);
    context.clearRect(0, 0, width, height);
    if (!values.length) { drawEmpty(context, width, height); return; }
    drawGrid(context, width, height);
    const max = Math.max(...values.map((value) => value.volume)) || 1; const barWidth = Math.max(1, (width - 14) / values.length - 1);
    context.fillStyle = '#b8b8bc';
    values.forEach((value, index) => { const x = chartX(index, values.length, width) - barWidth / 2; const barHeight = (height - 18) * value.volume / max; context.fillStyle = index === hoverIndex ? '#111113' : '#b8b8bc'; context.fillRect(x, height - 9 - barHeight, barWidth, barHeight); });
    drawCrosshair(context, width, height, values, hoverIndex);
  }

  function chartX(index, length, width) { return 7 + (width - 14) * (index / Math.max(length - 1, 1)); }
  function drawCrosshair(context, width, height, values, hoverIndex) { if (hoverIndex == null || !values[hoverIndex]) return; const x = chartX(hoverIndex, values.length, width); context.strokeStyle = '#8a8a8e'; context.lineWidth = 1; context.setLineDash([3, 3]); context.beginPath(); context.moveTo(x, 8); context.lineTo(x, height - 9); context.stroke(); context.setLineDash([]); context.fillStyle = '#111113'; context.beginPath(); context.arc(x, 10, 3, 0, Math.PI * 2); context.fill(); }
  function drawGrid(context, width, height) { context.strokeStyle = '#ededee'; context.lineWidth = 1; for (let i = 1; i < 4; i++) { const y = 8 + (height - 16) * (i / 4); context.beginPath(); context.moveTo(7, y); context.lineTo(width - 7, y); context.stroke(); } }
  function drawEmpty(context, width, height) { context.strokeStyle = '#ededee'; context.strokeRect(7, 8, width - 14, height - 17); context.fillStyle = '#96969a'; context.font = '11px sans-serif'; context.fillText('Waiting for live ticks…', 17, height / 2); }

  function clamp(value, min, max) { return Math.min(max, Math.max(min, value)); }
  function handleChartMove(event) {
    const canvas = event.currentTarget;
    const rect = canvas.getBoundingClientRect();
    if (chartState.drag) {
      const view = chartView();
      const delta = event.clientX - chartState.drag.x;
      const pointsPerPixel = Math.max(1, view.values.length) / Math.max(1, rect.width - 14);
      chartState.pan = clamp(chartState.drag.pan + Math.round(delta * pointsPerPixel), 0, Math.max(0, chartState.values.length - chartState.window));
    }
    const view = chartView();
    if (!view.values.length) return;
    const x = clamp(event.clientX - rect.left, 7, rect.width - 7);
    const relative = clamp(Math.round(((x - 7) / Math.max(1, rect.width - 14)) * Math.max(0, view.values.length - 1)), 0, view.values.length - 1);
    chartState.hover = view.start + relative;
    chartState.hoverKind = canvas.id === 'volumeChart' ? 'volume' : 'price';
    chartState.hoverX = x;
    renderInteractiveCharts();
  }

  function handleChartLeave(event) { if (chartState.drag) return; chartState.hover = null; chartState.hoverKind = null; chartState.hoverX = null; renderInteractiveCharts(); }
  function handleChartWheel(event) { event.preventDefault(); const current = chartState.window; const next = event.deltaY < 0 ? Math.max(20, Math.round(current * 0.8)) : Math.min(Math.max(20, chartState.values.length || 20), Math.round(current * 1.25)); chartState.window = Math.max(20, next); chartState.pan = clamp(chartState.pan, 0, Math.max(0, chartState.values.length - chartState.window)); renderInteractiveCharts(); }
  function updateChartTooltip() {
    const priceTooltip = $('priceTooltip'); const volumeTooltip = $('volumeTooltip');
    priceTooltip.hidden = true; volumeTooltip.hidden = true;
    if (chartState.hover == null || !chartState.values[chartState.hover]) return;
    const point = chartState.values[chartState.hover];
    const body = `<strong>${html(inr(point.price))}</strong><span>${html(dateTime(point.time))}</span><br><span>Tick qty ${html(number(point.volume, 0))}</span>${point.bid && point.ask ? `<br><span>Bid / ask ${html(inr(point.bid))} / ${html(inr(point.ask))}</span>` : ''}${point.buyQuantity || point.sellQuantity ? `<br><span>Buy / sell qty ${html(number(point.buyQuantity, 0))} / ${html(number(point.sellQuantity, 0))}</span>` : ''}`;
    const tooltip = chartState.hoverKind === 'volume' ? volumeTooltip : priceTooltip;
    tooltip.innerHTML = body;
    tooltip.hidden = false;
    const chart = tooltip.parentElement.querySelector('canvas');
    const x = chartState.hoverX || chart.getBoundingClientRect().width / 2;
    tooltip.style.left = `${clamp(x + 10, 8, chart.getBoundingClientRect().width - 170)}px`;
    tooltip.style.top = '12px';
  }

  function collectForm() {
    const form = $('runForm'); const payload = {};
    new FormData(form).forEach((value, key) => { payload[key] = credentialFields.includes(key) ? String(value).trim() : value; });
    // Hidden provider fields belong to the other market and must not be sent
    // to the runner (or accidentally overwrite its credentials).
    credentialFields.forEach((key) => { if (!requiredCredentialFields().includes(key)) delete payload[key]; });
    payload.market = selectedMarket();
    payload.symbol = String($('topTicker').value || payload.symbol || '').trim().toUpperCase();
    document.querySelector('[name="symbol"]').value = payload.symbol;
    ['capital','history_1m','history_5m','history_1d','max_history_bytes','timeout_ms','max_retries','max_position_value_percent','max_daily_loss_percent','max_trades_per_day','stop_loss_percent','max_spread_percent','min_action_confidence','min_buy_support'].forEach((key) => { payload[key] = Number(payload[key]); });
    return payload;
  }

  async function start(event) {
    event.preventDefault();
    const button = $('startButton'); button.disabled = true; button.textContent = 'Starting…';
    try {
      const response = await fetch('/api/control/start', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(collectForm()) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || 'start failed');
      persistFormValues();
      if (!$('rememberCredentials').checked) requiredCredentialFields().forEach((key) => { const input = document.querySelector(`[name="${key}"]`); if (input) input.value = ''; });
      updateCredentialStatus();
      showMessage($('rememberCredentials').checked ? 'Paper runner started. Saved values remain available on this browser.' : 'Paper runner started. Credentials were cleared from the form.', true);
      $('configDialog').close();
      await refresh();
    } catch (error) { showMessage(error.message, false); }
    button.disabled = false; button.textContent = 'Run paper loop';
  }

  async function stop() {
    $('stopButton').disabled = true;
    try { const response = await fetch('/api/control/stop', {method: 'POST'}); const result = await response.json(); if (!response.ok) throw new Error(result.error || 'stop failed'); showMessage('Stop signal sent to paper-only runner processes.', true); await refresh(); }
    catch (error) { showMessage(error.message, false); }
    $('stopButton').disabled = false;
  }

  async function kiteURL() {
    const key = document.querySelector('[name="kite_api_key"]').value.trim();
    if (!key) { showMessage('Enter a Kite API key first.', false); return; }
    try { const response = await fetch(`/api/kite-url?api_key=${encodeURIComponent(key)}`); const result = await response.json(); if (!response.ok) throw new Error(result.error || 'could not build URL'); $('kiteUrlResult').innerHTML = `<a href="${html(result.url)}" target="_blank" rel="noreferrer">Open Kite login</a>`; }
    catch (error) { showMessage(error.message, false); }
  }

  async function exchangeKiteToken() {
    const apiKey = document.querySelector('[name="kite_api_key"]').value.trim();
    const apiSecret = document.querySelector('[name="kite_api_secret"]').value.trim();
    const requestToken = $('kiteRequestToken').value.trim();
    const missing = [];
    if (!apiKey) missing.push('Kite API key');
    if (!apiSecret) missing.push('Kite API secret');
    if (!requestToken) missing.push('Fresh Kite request token');
    if (missing.length) {
      showMessage(`Missing: ${missing.join(', ')}. The access-token field is not used by this exchange button.`, false);
      return;
    }
    const button = $('exchangeKiteButton');
    button.disabled = true;
    button.textContent = 'Exchanging…';
    try {
      const response = await fetch('/api/kite-token', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({api_key: apiKey, api_secret: apiSecret, request_token: requestToken}) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || 'Kite token exchange failed');
      document.querySelector('[name="kite_access_token"]').value = result.access_token;
      $('kiteRequestToken').value = '';
      persistFormValues();
      updateCredentialStatus();
      showMessage('Fresh Kite access token received. Click Save credentials if you want to keep it in this browser.', true);
    } catch (error) {
      showMessage(error.message, false);
    }
    button.disabled = false;
    button.textContent = 'Exchange request token';
  }

  function showMessage(message, success) { $('controlMessage').textContent = message; $('controlMessage').className = `control-message ${success ? 'success' : 'error'}`; }

  $('runForm').addEventListener('submit', start);
  $('stopButton').addEventListener('click', stop);
  $('kiteUrlButton').addEventListener('click', kiteURL);
  $('exchangeKiteButton').addEventListener('click', exchangeKiteToken);
  $('saveCredentialsButton').addEventListener('click', saveCredentials);
  $('showCredentialsButton').addEventListener('click', toggleCredentials);
  $('clearSavedButton').addEventListener('click', clearSavedValues);
  document.querySelectorAll('.market-tab').forEach((button) => button.addEventListener('click', () => setMarket(button.dataset.market, true, 'user')));
  $('topTicker').addEventListener('input', (event) => { document.querySelector('[name="symbol"]').value = event.target.value.toUpperCase(); persistFormValues(); });
  document.querySelector('[name="symbol"]').addEventListener('input', (event) => { $('topTicker').value = event.target.value.toUpperCase(); });
  restoreFormValues();
  setMarket(selectedMarket(), false);
  $('configButton').addEventListener('click', () => $('configDialog').showModal());
  $('closeConfigButton').addEventListener('click', () => $('configDialog').close());
  $('configDialog').addEventListener('click', (event) => { if (event.target === $('configDialog')) $('configDialog').close(); });
  window.addEventListener('pageshow', normalizeLegacyHistoryBudgetInput);
  setTimeout(normalizeLegacyHistoryBudgetInput, 1000);
  document.querySelectorAll('#runForm [name]').forEach((input) => { input.addEventListener('input', persistFormValues); input.addEventListener('change', persistFormValues); });
  $('rememberCredentials').addEventListener('change', () => { persistFormValues(); updateCredentialStatus(); });
  ['actionFilter','regimeFilter','confidenceFilter'].forEach((id) => $(id).addEventListener('input', () => { persistFormValues(); if (state) renderDecisions(state.events || []); }));
  $('logLines').addEventListener('change', () => { persistFormValues(); refresh(); });
  document.querySelectorAll('#priceChart, #volumeChart').forEach((canvas) => {
    canvas.addEventListener('pointerdown', (event) => { chartState.drag = { x: event.clientX, pan: chartState.pan }; canvas.setPointerCapture?.(event.pointerId); });
    canvas.addEventListener('pointermove', handleChartMove);
    canvas.addEventListener('pointerleave', handleChartLeave);
    canvas.addEventListener('pointerup', () => { chartState.drag = null; });
    canvas.addEventListener('pointercancel', () => { chartState.drag = null; });
    canvas.addEventListener('wheel', handleChartWheel, { passive: false });
  });
  document.querySelectorAll('[data-chart-window]').forEach((button) => button.addEventListener('click', () => {
    chartState.window = Number(button.dataset.chartWindow);
    chartState.pan = 0;
    renderInteractiveCharts();
  }));
  $('chartReset').addEventListener('click', () => {
    chartState.window = 300;
    chartState.pan = 0;
    chartState.fromDate = '';
    chartState.toDate = '';
    $('chartFromDate').value = '';
    $('chartToDate').value = '';
    chartState.values = filterChartDates(chartState.allValues);
    renderInteractiveCharts();
  });
  $('chartFromDate').addEventListener('change', (event) => { chartState.fromDate = event.target.value; chartState.pan = 0; chartState.values = filterChartDates(chartState.allValues); renderInteractiveCharts(); });
  $('chartToDate').addEventListener('change', (event) => { chartState.toDate = event.target.value; chartState.pan = 0; chartState.values = filterChartDates(chartState.allValues); renderInteractiveCharts(); });
  window.addEventListener('resize', () => { if (state) drawCharts(state.events || [], state.market || []); });
  refresh();
  window.setInterval(refresh, 1000);
})();
