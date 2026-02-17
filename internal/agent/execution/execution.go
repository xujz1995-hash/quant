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

type Input struct {
	CycleID       string
	SignalID      string
	Pair          string
	Side          domain.Side
	StakeUSDT     float64
	EstimatedFill float64
	SellQuantity  float64 // 卖出时的币数量（close 信号用）
}

// Balance 交易所账户余额
type Balance struct {
	Symbol string  // 如 DOGE
	Free   float64 // 可用余额
	Locked float64 // 冻结余额
	Total  float64 // Free + Locked
}

// Trade 币安成交记录
type Trade struct {
	TradeID   int64
	OrderID   int64
	Symbol    string
	Price     float64
	Quantity  float64
	QuoteQty  float64
	IsBuyer   bool
	Timestamp time.Time
}

type Executor interface {
	Execute(ctx context.Context, input Input) (domain.Order, error)
	FetchAccountBalances(ctx context.Context) ([]Balance, error)
	FetchFullBalance(ctx context.Context) ([]Balance, error) // 含 USDT
	FetchTradeHistory(ctx context.Context, pair string, limit int) ([]Trade, error)
	FetchPositionRisk(ctx context.Context, pair string) (float64, error) // 合约持仓数量（现货返回 0）
	IsDryRun() bool
	TradingMode() string // "spot" 或 "futures"
	Leverage() int       // 杠杆倍数，现货=1
}

// BinanceExecutor 直接通过 Binance API 下单（无需 Freqtrade）
type BinanceExecutor struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	secretKey  string
	dryRun     bool
}

func New(cfg config.Config) Executor {
	return &BinanceExecutor{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    strings.TrimRight(cfg.ExchangeBaseURL, "/"),
		apiKey:     cfg.ExchangeAPIKey,
		secretKey:  cfg.ExchangeSecretKey,
		dryRun:     cfg.DryRun,
	}
}

func (e *BinanceExecutor) Execute(ctx context.Context, input Input) (domain.Order, error) {
	order := domain.Order{
		ID:            uuid.NewString(),
		CycleID:       input.CycleID,
		SignalID:      input.SignalID,
		ClientOrderID: fmt.Sprintf("aq%s", uuid.NewString()[:8]),
		Pair:          input.Pair,
		Side:          input.Side,
		StakeUSDT:     input.StakeUSDT,
		Status:        "created",
		CreatedAt:     time.Now().UTC(),
	}

	// 模拟模式：不调交易所
	if e.dryRun {
		estimatedFill := input.EstimatedFill
		// 如果没有价格，尝试从 Binance 获取实时价格
		if estimatedFill <= 0 {
			if price, err := e.fetchCurrentPrice(ctx, input.Pair); err == nil && price > 0 {
				estimatedFill = price
				log.Printf("[执行] 获取实时价格: %s = %.8f", input.Pair, price)
			}
		}

		order.Status = "simulated_filled"
		order.ExchangeOrderID = "dryrun-" + order.ID
		order.FilledPrice = estimatedFill
		order.RawResponse = `{"mode":"dry_run"}`

		// 计算模拟成交数量
		if estimatedFill > 0 && input.Side == domain.SideLong {
			order.FilledQuantity = input.StakeUSDT / estimatedFill
		} else if input.SellQuantity > 0 {
			order.FilledQuantity = input.SellQuantity
		}

		action := "买入"
		if input.Side == domain.SideClose {
			action = "卖出"
		}
		log.Printf("[执行] 模拟%s: %s %s %.2f USDT @ %.8f 数量=%.4f",
			action, input.Side, input.Pair, input.StakeUSDT, estimatedFill, order.FilledQuantity)
		return order, nil
	}

	// 实盘模式：调用 Binance API
	if e.apiKey == "" || e.secretKey == "" {
		order.Status = "rejected"
		return order, fmt.Errorf("交易所 API Key 未配置，无法实盘下单")
	}

	symbol := pairToSymbol(input.Pair)
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
		// 买入：用 quoteOrderQty 按 USDT 金额
		params.Set("quoteOrderQty", strconv.FormatFloat(input.StakeUSDT, 'f', 2, 64))
	} else {
		// 卖出：用 quantity 按币数量
		if input.SellQuantity > 0 {
			// 根据交易对调整数量精度（Binance LOT_SIZE 要求）
			qty := quantityPrecision(symbol, input.SellQuantity)

			// 检查格式化后的数量是否有效（防止灰尘持仓）
			qtyFloat, _ := strconv.ParseFloat(qty, 64)
			if qtyFloat <= 0 {
				order.Status = "rejected"
				minQty := getMinQuantity(symbol)
				log.Printf("[执行] ⚠ 卖出数量不足: %.8f < 最小交易量 %.0f，跳过交易", input.SellQuantity, minQty)
				return order, fmt.Errorf("卖出数量不足: %.8f %s 低于最小交易量 %.0f（灰尘持仓无法交易）",
					input.SellQuantity, symbol, minQty)
			}

			params.Set("quantity", qty)
			log.Printf("[执行] 卖出数量: 原始=%.8f 格式化=%s", input.SellQuantity, qty)
		} else {
			// 没有指定数量，按 USDT 金额估算
			params.Set("quoteOrderQty", strconv.FormatFloat(input.StakeUSDT, 'f', 2, 64))
		}
	}

	// HMAC-SHA256 签名
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/api/v3/order"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		return order, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	log.Printf("[执行] 发送 Binance 订单: %s %s %.2f USDT", side, symbol, input.StakeUSDT)

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
		log.Printf("[执行] ✘ Binance 拒绝: HTTP %d %s", resp.StatusCode, string(respBytes))
		return order, fmt.Errorf("Binance HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	// 解析返回
	var result struct {
		OrderID       int64  `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
		Status        string `json:"status"`
		Fills         []struct {
			Price string `json:"price"`
			Qty   string `json:"qty"`
		} `json:"fills"`
	}
	if err := json.Unmarshal(respBytes, &result); err == nil {
		order.ExchangeOrderID = strconv.FormatInt(result.OrderID, 10)
		order.Status = mapBinanceStatus(result.Status)

		// 计算加权平均成交价和总成交量
		if len(result.Fills) > 0 {
			var totalQty, totalCost float64
			for _, f := range result.Fills {
				p, _ := strconv.ParseFloat(f.Price, 64)
				q, _ := strconv.ParseFloat(f.Qty, 64)
				totalQty += q
				totalCost += p * q
			}
			if totalQty > 0 {
				order.FilledPrice = totalCost / totalQty
				order.FilledQuantity = totalQty
			}
		}
	}

	log.Printf("[执行] ✔ Binance 订单完成: ID=%s 状态=%s 成交价=%.4f",
		order.ExchangeOrderID, order.Status, order.FilledPrice)

	return order, nil
}

// sign 使用 HMAC-SHA256 对请求参数签名
func (e *BinanceExecutor) sign(queryString string) string {
	mac := hmac.New(sha256.New, []byte(e.secretKey))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

// mapBinanceStatus 将 Binance 订单状态映射为内部状态
func mapBinanceStatus(s string) string {
	switch s {
	case "FILLED":
		return "filled"
	case "PARTIALLY_FILLED":
		return "partial_filled"
	case "NEW":
		return "submitted"
	case "CANCELED", "REJECTED", "EXPIRED":
		return "rejected"
	default:
		return s
	}
}

// fetchCurrentPrice 从 Binance 公开 API 获取当前价格（用于 dry-run 模拟）
func (e *BinanceExecutor) fetchCurrentPrice(ctx context.Context, pair string) (float64, error) {
	symbol := pairToSymbol(pair)
	apiURL := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Binance price API %d", resp.StatusCode)
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(result.Price, 64)
}

// IsDryRun 返回当前是否为模拟模式
func (e *BinanceExecutor) IsDryRun() bool {
	return e.dryRun
}

func (e *BinanceExecutor) TradingMode() string {
	return "spot"
}

func (e *BinanceExecutor) Leverage() int {
	return 1
}

// FetchPositionRisk 现货模式不支持，返回 0
func (e *BinanceExecutor) FetchPositionRisk(ctx context.Context, pair string) (float64, error) {
	return 0, nil
}

// FetchAccountBalances 从 Binance 获取账户所有非零余额
func (e *BinanceExecutor) FetchAccountBalances(ctx context.Context) ([]Balance, error) {
	if e.apiKey == "" || e.secretKey == "" {
		return nil, fmt.Errorf("交易所 API Key 未配置，无法查询余额")
	}

	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/api/v3/account?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Binance 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Binance HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	balances := make([]Balance, 0)
	for _, b := range result.Balances {
		free, _ := strconv.ParseFloat(b.Free, 64)
		locked, _ := strconv.ParseFloat(b.Locked, 64)
		total := free + locked
		// 只保留非零余额，过滤掉 USDT 本身（我们关心的是持仓币种）
		if total > 0 && b.Asset != "USDT" && b.Asset != "BNB" && b.Asset != "LDUSDT" {
			balances = append(balances, Balance{
				Symbol: b.Asset,
				Free:   free,
				Locked: locked,
				Total:  total,
			})
		}
	}

	log.Printf("[交易所] 同步到 %d 个币种余额", len(balances))
	return balances, nil
}

// FetchFullBalance 获取完整余额（含 USDT、BNB 等所有非零资产）
func (e *BinanceExecutor) FetchFullBalance(ctx context.Context) ([]Balance, error) {
	if e.apiKey == "" || e.secretKey == "" {
		return nil, fmt.Errorf("交易所 API Key 未配置")
	}

	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/api/v3/account?" + params.Encode()
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

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Binance HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, err
	}

	balances := make([]Balance, 0)
	for _, b := range result.Balances {
		free, _ := strconv.ParseFloat(b.Free, 64)
		locked, _ := strconv.ParseFloat(b.Locked, 64)
		total := free + locked
		if total > 0 {
			balances = append(balances, Balance{
				Symbol: b.Asset,
				Free:   free,
				Locked: locked,
				Total:  total,
			})
		}
	}
	return balances, nil
}

// FetchTradeHistory 从 Binance 获取指定交易对的成交历史
func (e *BinanceExecutor) FetchTradeHistory(ctx context.Context, pair string, limit int) ([]Trade, error) {
	if e.apiKey == "" || e.secretKey == "" {
		return nil, fmt.Errorf("交易所 API Key 未配置")
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}

	symbol := pairToSymbol(pair)
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	signature := e.sign(params.Encode())
	params.Set("signature", signature)

	apiURL := e.baseURL + "/api/v3/myTrades?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Binance 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Binance HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var raw []struct {
		ID       int64  `json:"id"`
		OrderID  int64  `json:"orderId"`
		Price    string `json:"price"`
		Qty      string `json:"qty"`
		QuoteQty string `json:"quoteQty"`
		Time     int64  `json:"time"`
		IsBuyer  bool   `json:"isBuyer"`
	}
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	trades := make([]Trade, 0, len(raw))
	for _, r := range raw {
		price, _ := strconv.ParseFloat(r.Price, 64)
		qty, _ := strconv.ParseFloat(r.Qty, 64)
		quoteQty, _ := strconv.ParseFloat(r.QuoteQty, 64)
		trades = append(trades, Trade{
			TradeID:   r.ID,
			OrderID:   r.OrderID,
			Symbol:    symbol,
			Price:     price,
			Quantity:  qty,
			QuoteQty:  quoteQty,
			IsBuyer:   r.IsBuyer,
			Timestamp: time.UnixMilli(r.Time).UTC(),
		})
	}

	log.Printf("[交易所] 获取 %s 成交记录 %d 笔", pair, len(trades))
	return trades, nil
}

// pairToSymbol 将 "BTC/USDT" 转为 "BTCUSDT"
func pairToSymbol(pair string) string {
	out := ""
	for _, c := range pair {
		if c != '/' {
			out += string(c)
		}
	}
	return out
}

// getMinQuantity 获取交易对的最小交易数量
// Binance 每个交易对有不同的 minQty 要求
func getMinQuantity(symbol string) float64 {
	sym := strings.ToUpper(symbol)
	switch {
	case strings.HasPrefix(sym, "DOGE"):
		return 1 // DOGE 最小交易 1 个
	case strings.HasPrefix(sym, "XRP"):
		return 1 // XRP 最小交易 1 个
	case strings.HasPrefix(sym, "BNB"):
		return 0.01
	case strings.HasPrefix(sym, "SOL"):
		return 0.01
	case strings.HasPrefix(sym, "ETH"):
		return 0.0001
	case strings.HasPrefix(sym, "BTC"):
		return 0.00001
	default:
		return 1
	}
}

// quantityPrecision 根据交易对返回正确精度的数量字符串
// Binance LOT_SIZE 要求不同币的 stepSize 不同：
//
//	DOGEUSDT stepSize=1（整数）, XRPUSDT stepSize=0.1, BTCUSDT stepSize=0.00001
func quantityPrecision(symbol string, qty float64) string {
	sym := strings.ToUpper(symbol)
	var decimals int
	switch {
	case strings.HasPrefix(sym, "DOGE"):
		decimals = 0          // stepSize=1，必须整数
		qty = math.Floor(qty) // 向下取整，避免超过持仓
	case strings.HasPrefix(sym, "XRP"):
		decimals = 1 // stepSize=0.1
		qty = math.Floor(qty*10) / 10
	case strings.HasPrefix(sym, "BNB"), strings.HasPrefix(sym, "SOL"):
		decimals = 2 // stepSize=0.01
		qty = math.Floor(qty*100) / 100
	case strings.HasPrefix(sym, "ETH"):
		decimals = 4 // stepSize=0.0001
		qty = math.Floor(qty*10000) / 10000
	case strings.HasPrefix(sym, "BTC"):
		decimals = 5 // stepSize=0.00001
		qty = math.Floor(qty*100000) / 100000
	default:
		decimals = 2
		qty = math.Floor(qty*100) / 100
	}
	return strconv.FormatFloat(qty, 'f', decimals, 64)
}
