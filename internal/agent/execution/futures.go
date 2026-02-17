package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ai_quant/internal/config"
	"ai_quant/internal/domain"

	"github.com/google/uuid"
)

// BinanceFuturesExecutor 通过 Binance USDT-M 永续合约 API 下单
type BinanceFuturesExecutor struct {
	httpClient *http.Client
	baseURL    string // https://fapi.binance.com
	apiKey     string
	secretKey  string
	dryRun     bool
	leverage   int
	marginType string // "CROSSED" 或 "ISOLATED"
}

// NewFutures 创建合约 Executor，启动时自动设置杠杆和保证金模式
func NewFutures(cfg config.Config) Executor {
	e := &BinanceFuturesExecutor{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    strings.TrimRight(cfg.FuturesBaseURL, "/"),
		apiKey:     cfg.ExchangeAPIKey,
		secretKey:  cfg.ExchangeSecretKey,
		dryRun:     cfg.DryRun,
		leverage:   cfg.FuturesLeverage,
		marginType: cfg.FuturesMarginType,
	}

	// 限制杠杆范围 2-20
	if e.leverage < 1 {
		e.leverage = 3
	}
	if e.leverage > 20 {
		e.leverage = 20
	}

	log.Printf("[合约] 初始化: baseURL=%s 杠杆=%dx 保证金=%s dryRun=%v",
		e.baseURL, e.leverage, e.marginType, e.dryRun)

	// 非 dry-run 模式且有 API Key 时，自动设置杠杆和保证金模式
	if !e.dryRun && e.apiKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pairs := strings.Split(cfg.AutoRunPairs, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			symbol := strings.ReplaceAll(strings.ToUpper(pair), "/", "")
			e.setupLeverage(ctx, symbol)
			e.setupMarginType(ctx, symbol)
		}
	}

	return e
}

// setupLeverage 设置交易对的杠杆倍数
func (e *BinanceFuturesExecutor) setupLeverage(ctx context.Context, symbol string) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("leverage", strconv.Itoa(e.leverage))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v1/leverage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		log.Printf("[合约] 设置杠杆请求构建失败 %s: %v", symbol, err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		log.Printf("[合约] 设置杠杆请求失败 %s: %v", symbol, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		log.Printf("[合约] ⚠ 设置杠杆失败 %s: HTTP %d %s", symbol, resp.StatusCode, string(body))
	} else {
		log.Printf("[合约] ✔ 杠杆已设置 %s: %dx", symbol, e.leverage)
	}
}

// setupMarginType 设置保证金模式（全仓/逐仓）
func (e *BinanceFuturesExecutor) setupMarginType(ctx context.Context, symbol string) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("marginType", e.marginType)
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v1/marginType"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		log.Printf("[合约] 设置保证金模式请求构建失败 %s: %v", symbol, err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		log.Printf("[合约] 设置保证金模式请求失败 %s: %v", symbol, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// -4046 = "No need to change margin type" 表示已经是目标模式，不算错误
	if resp.StatusCode >= 300 && !strings.Contains(string(body), "-4046") {
		log.Printf("[合约] ⚠ 设置保证金模式失败 %s: HTTP %d %s", symbol, resp.StatusCode, string(body))
	} else {
		log.Printf("[合约] ✔ 保证金模式已设置 %s: %s", symbol, e.marginType)
	}
}

// Execute 执行合约交易
func (e *BinanceFuturesExecutor) Execute(ctx context.Context, input Input) (domain.Order, error) {
	order := domain.Order{
		ID:            uuid.NewString(),
		CycleID:       input.CycleID,
		SignalID:      input.SignalID,
		ClientOrderID: fmt.Sprintf("aq%s", uuid.NewString()[:8]),
		Pair:          input.Pair,
		Side:          input.Side,
		StakeUSDT:     input.StakeUSDT,
		Leverage:      e.leverage,
		Status:        "created",
		CreatedAt:     time.Now().UTC(),
	}

	// 模拟模式
	if e.dryRun {
		estimatedFill := input.EstimatedFill
		if estimatedFill <= 0 {
			if price, err := e.fetchCurrentPrice(ctx, input.Pair); err == nil && price > 0 {
				estimatedFill = price
				log.Printf("[合约] 获取实时价格: %s = %.8f", input.Pair, price)
			}
		}

		order.Status = "simulated_filled"
		order.ExchangeOrderID = "dryrun-futures-" + order.ID
		order.FilledPrice = estimatedFill
		order.RawResponse = fmt.Sprintf(`{"mode":"dry_run","leverage":%d}`, e.leverage)

		if estimatedFill > 0 && input.Side == domain.SideLong {
			// 合约：保证金 * 杠杆 / 价格 = 开仓数量
			order.FilledQuantity = (input.StakeUSDT * float64(e.leverage)) / estimatedFill
		} else if input.SellQuantity > 0 {
			order.FilledQuantity = input.SellQuantity
		}

		action := "开多"
		if input.Side == domain.SideClose {
			action = "平仓"
		}
		log.Printf("[合约] 模拟%s: %s %s 保证金=%.2f USDT x%d @ %.8f 数量=%.4f",
			action, input.Side, input.Pair, input.StakeUSDT, e.leverage, estimatedFill, order.FilledQuantity)
		return order, nil
	}

	// 实盘模式
	if e.apiKey == "" || e.secretKey == "" {
		order.Status = "rejected"
		return order, fmt.Errorf("交易所 API Key 未配置，无法实盘下单")
	}

	symbol := strings.ReplaceAll(strings.ToUpper(input.Pair), "/", "")
	side := "BUY"
	if input.Side == domain.SideClose {
		side = "SELL"
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", "MARKET")
	params.Set("newClientOrderId", order.ClientOrderID)
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	if side == "BUY" {
		// 开多：用保证金 * 杠杆计算开仓数量
		if input.EstimatedFill > 0 {
			rawQty := (input.StakeUSDT * float64(e.leverage)) / input.EstimatedFill
			qty := futuresQuantityPrecision(symbol, rawQty)
			params.Set("quantity", qty)
			log.Printf("[合约] 开多数量: 保证金=%.2f x%d / 价格=%.8f = %s",
				input.StakeUSDT, e.leverage, input.EstimatedFill, qty)
		} else {
			// 没有预估价格，无法计算数量
			order.Status = "rejected"
			return order, fmt.Errorf("无法计算开仓数量：缺少价格数据")
		}
	} else {
		// 平仓：用 quantity + reduceOnly
		params.Set("reduceOnly", "true")
		if input.SellQuantity > 0 {
			qty := futuresQuantityPrecision(symbol, input.SellQuantity)
			params.Set("quantity", qty)
			log.Printf("[合约] 平仓数量: %s", qty)
		} else {
			order.Status = "rejected"
			return order, fmt.Errorf("平仓缺少数量参数")
		}
	}

	// HMAC-SHA256 签名
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v1/order"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		return order, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	log.Printf("[合约] 发送 Binance 合约订单: %s %s 保证金=%.2f USDT x%d", side, symbol, input.StakeUSDT, e.leverage)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		order.Status = "failed"
		return order, fmt.Errorf("Binance 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		order.Status = "failed"
		return order, fmt.Errorf("读取响应失败: %w", err)
	}
	order.RawResponse = string(respBytes)

	if resp.StatusCode >= 300 {
		order.Status = "rejected"
		log.Printf("[合约] ✘ Binance 拒绝: HTTP %d %s", resp.StatusCode, string(respBytes))
		return order, fmt.Errorf("Binance HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	// 解析返回
	var result struct {
		OrderID       int64  `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
		Status        string `json:"status"`
		AvgPrice      string `json:"avgPrice"`
		ExecutedQty   string `json:"executedQty"`
	}
	if err := json.Unmarshal(respBytes, &result); err == nil {
		order.ExchangeOrderID = strconv.FormatInt(result.OrderID, 10)
		order.Status = mapBinanceStatus(result.Status)
		if p, e := strconv.ParseFloat(result.AvgPrice, 64); e == nil {
			order.FilledPrice = p
		}
		if q, e := strconv.ParseFloat(result.ExecutedQty, 64); e == nil {
			order.FilledQuantity = q
		}
	}

	action := "开多"
	if input.Side == domain.SideClose {
		action = "平仓"
	}
	log.Printf("[合约] ✔ %s成功: %s %s 价格=%.8f 数量=%.4f x%d 状态=%s",
		action, side, symbol, order.FilledPrice, order.FilledQuantity, e.leverage, order.Status)
	return order, nil
}

func (e *BinanceFuturesExecutor) IsDryRun() bool {
	return e.dryRun
}

func (e *BinanceFuturesExecutor) TradingMode() string {
	return "futures"
}

func (e *BinanceFuturesExecutor) Leverage() int {
	return e.leverage
}

// FetchPositionRisk 从合约 API 获取持仓数量
func (e *BinanceFuturesExecutor) FetchPositionRisk(ctx context.Context, pair string) (float64, error) {
	if e.dryRun {
		return 0, nil
	}

	symbol := strings.ReplaceAll(strings.ToUpper(pair), "/", "")

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v2/positionRisk?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var positions []struct {
		Symbol      string `json:"symbol"`
		PositionAmt string `json:"positionAmt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return 0, err
	}

	for _, p := range positions {
		if strings.EqualFold(p.Symbol, symbol) {
			amt, _ := strconv.ParseFloat(p.PositionAmt, 64)
			return math.Abs(amt), nil // 返回绝对值
		}
	}
	return 0, nil
}

// FetchAccountBalances 获取合约账户 USDT 余额
func (e *BinanceFuturesExecutor) FetchAccountBalances(ctx context.Context) ([]Balance, error) {
	return e.fetchFuturesBalance(ctx, false)
}

// FetchFullBalance 获取合约账户所有余额
func (e *BinanceFuturesExecutor) FetchFullBalance(ctx context.Context) ([]Balance, error) {
	return e.fetchFuturesBalance(ctx, true)
}

func (e *BinanceFuturesExecutor) fetchFuturesBalance(ctx context.Context, includeAll bool) ([]Balance, error) {
	if e.dryRun {
		return []Balance{{Symbol: "USDT", Free: 1000, Total: 1000}}, nil
	}

	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v2/balance?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rawBalances []struct {
		Asset            string `json:"asset"`
		Balance          string `json:"balance"`
		AvailableBalance string `json:"availableBalance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawBalances); err != nil {
		return nil, err
	}

	var balances []Balance
	for _, b := range rawBalances {
		total, _ := strconv.ParseFloat(b.Balance, 64)
		free, _ := strconv.ParseFloat(b.AvailableBalance, 64)
		if !includeAll && total == 0 {
			continue
		}
		if includeAll || b.Asset == "USDT" || total > 0 {
			balances = append(balances, Balance{
				Symbol: b.Asset,
				Free:   free,
				Locked: total - free,
				Total:  total,
			})
		}
	}
	return balances, nil
}

// FetchTradeHistory 获取合约交易记录
func (e *BinanceFuturesExecutor) FetchTradeHistory(ctx context.Context, pair string, limit int) ([]Trade, error) {
	if e.dryRun {
		return nil, nil
	}

	symbol := strings.ReplaceAll(strings.ToUpper(pair), "/", "")

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/fapi/v1/userTrades?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rawTrades []struct {
		ID       int64  `json:"id"`
		OrderID  int64  `json:"orderId"`
		Symbol   string `json:"symbol"`
		Price    string `json:"price"`
		Qty      string `json:"qty"`
		QuoteQty string `json:"quoteQty"`
		Buyer    bool   `json:"buyer"`
		Time     int64  `json:"time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawTrades); err != nil {
		return nil, err
	}

	var trades []Trade
	for _, r := range rawTrades {
		price, _ := strconv.ParseFloat(r.Price, 64)
		qty, _ := strconv.ParseFloat(r.Qty, 64)
		quoteQty, _ := strconv.ParseFloat(r.QuoteQty, 64)
		trades = append(trades, Trade{
			TradeID:   r.ID,
			OrderID:   r.OrderID,
			Symbol:    r.Symbol,
			Price:     price,
			Quantity:  qty,
			QuoteQty:  quoteQty,
			IsBuyer:   r.Buyer,
			Timestamp: time.UnixMilli(r.Time).UTC(),
		})
	}

	log.Printf("[合约] 获取 %s 成交记录 %d 笔", pair, len(trades))
	return trades, nil
}

// fetchCurrentPrice 从公共 API 获取合约最新价格
func (e *BinanceFuturesExecutor) fetchCurrentPrice(ctx context.Context, pair string) (float64, error) {
	symbol := strings.ReplaceAll(strings.ToUpper(pair), "/", "")
	apiURL := fmt.Sprintf("%s/fapi/v1/ticker/price?symbol=%s", e.baseURL, symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	p, _ := strconv.ParseFloat(result.Price, 64)
	return p, nil
}

// sign HMAC-SHA256 签名（与现货完全一致）
func (e *BinanceFuturesExecutor) sign(queryString string) string {
	mac := hmac.New(sha256.New, []byte(e.secretKey))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

// futuresQuantityPrecision 合约数量精度（与现货类似但合约规则可能不同）
func futuresQuantityPrecision(symbol string, qty float64) string {
	sym := strings.ToUpper(symbol)
	var decimals int
	switch {
	case strings.HasPrefix(sym, "DOGE"):
		decimals = 0 // stepSize=1
		qty = math.Floor(qty)
	case strings.HasPrefix(sym, "XRP"):
		decimals = 1
		qty = math.Floor(qty*10) / 10
	case strings.HasPrefix(sym, "BNB"), strings.HasPrefix(sym, "SOL"):
		decimals = 2
		qty = math.Floor(qty*100) / 100
	case strings.HasPrefix(sym, "ETH"):
		decimals = 3
		qty = math.Floor(qty*1000) / 1000
	case strings.HasPrefix(sym, "BTC"):
		decimals = 3
		qty = math.Floor(qty*1000) / 1000
	default:
		decimals = 2
		qty = math.Floor(qty*100) / 100
	}
	return strconv.FormatFloat(qty, 'f', decimals, 64)
}
