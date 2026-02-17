package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

const (
	binanceSpotBase    = "https://api.binance.com"
	binanceFuturesBase = "https://fapi.binance.com"
)

// Kline represents a single candlestick.
type Kline struct {
	OpenTime  time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime time.Time
}

// SentimentData holds sentiment factor data.
type SentimentData struct {
	LongShortRatio    float64 // Global long/short account ratio
	TopLongShortRatio float64 // Top trader long/short account ratio
	TopPositionRatio  float64 // Top trader position long/short ratio
	TakerBuySellRatio float64 // Taker buy/sell ratio (>1 = buyers dominate)
	FearGreedIndex    int     // Fear & Greed index 0-100
	FearGreedLabel    string  // "Extreme Fear" / "Fear" / "Neutral" / "Greed" / "Extreme Greed"
}

// CoinSnapshot holds all market data for one trading pair.
type CoinSnapshot struct {
	Pair         string
	Price        float64
	Change24hPct float64
	FundingRate  float64
	OpenInterest float64

	// Short-term series (e.g. 5m)
	ShortInterval string
	ShortKlines   []Kline

	// Long-term series (4h)
	LongKlines []Kline

	// Sentiment factors
	Sentiment SentimentData

	// News (from CryptoPanic, best effort)
	News []NewsItem

	// Social media metrics (from LunarCrush, best effort)
	Social SocialMetrics

	// CoinGecko community & trending data (free)
	CoinGecko CoinGeckoData

	// Google Trends daily trending check (free)
	GoogleTrends GoogleTrendsData
}

// Client fetches market data from Binance public APIs (no API key required).
type Client struct {
	http           *http.Client
	CryptoPanicKey string // 可选，为空则跳过新闻获取
	LunarCrushKey  string // 可选，为空则跳过社交数据获取
}

// NewClient creates a Binance market data client.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchSnapshot gathers all data for a single pair.
// pair format: "BTC/USDT" -> converts to "BTCUSDT" for Binance.
func (c *Client) FetchSnapshot(ctx context.Context, pair string) (CoinSnapshot, error) {
	symbol := pairToSymbol(pair)
	snap := CoinSnapshot{
		Pair:          pair,
		ShortInterval: "5m",
	}

	// 1. 24h ticker (price + change)
	ticker, err := c.fetch24hTicker(ctx, symbol)
	if err != nil {
		return snap, fmt.Errorf("ticker %s: %w", symbol, err)
	}
	snap.Price = ticker.LastPrice
	snap.Change24hPct = ticker.PriceChangePercent

	// 2. Short-term klines (5m, last 50 candles ≈ 4 hours)
	shortKlines, err := c.fetchKlines(ctx, symbol, "5m", 50)
	if err != nil {
		return snap, fmt.Errorf("klines 5m %s: %w", symbol, err)
	}
	snap.ShortKlines = shortKlines

	// 3. Long-term klines (4h, last 30 candles ≈ 5 days)
	longKlines, err := c.fetchKlines(ctx, symbol, "4h", 30)
	if err != nil {
		return snap, fmt.Errorf("klines 4h %s: %w", symbol, err)
	}
	snap.LongKlines = longKlines

	// 4. Funding rate (futures, best effort)
	funding, _ := c.fetchFundingRate(ctx, symbol)
	snap.FundingRate = funding

	// 5. Open interest (futures, best effort)
	oi, _ := c.fetchOpenInterest(ctx, symbol)
	snap.OpenInterest = oi

	// 6. Sentiment (all best effort, failures won't block)
	snap.Sentiment.LongShortRatio, _ = c.fetchRatio(ctx, symbol, "globalLongShortAccountRatio")
	snap.Sentiment.TopLongShortRatio, _ = c.fetchRatio(ctx, symbol, "topLongShortAccountRatio")
	snap.Sentiment.TopPositionRatio, _ = c.fetchRatio(ctx, symbol, "topLongShortPositionRatio")
	snap.Sentiment.TakerBuySellRatio, _ = c.fetchRatio(ctx, symbol, "takerlongshortRatio")
	snap.Sentiment.FearGreedIndex, snap.Sentiment.FearGreedLabel, _ = fetchFearGreedIndex(ctx, c.http)

	// 7. News from CryptoPanic (best effort, empty key or failure → skip)
	snap.News = c.fetchNews(ctx, pair)

	// 8. Social media metrics from LunarCrush (best effort)
	snap.Social = c.fetchSocialMetrics(ctx, pair)

	// 9. CoinGecko community & trending (free, no key needed)
	snap.CoinGecko = c.fetchCoinGeckoData(ctx, pair)

	// 10. Google Trends daily trending check (free)
	snap.GoogleTrends = c.fetchGoogleTrends(ctx, pair)

	return snap, nil
}

// FetchPrice returns just the latest price for a pair (lightweight).
func (c *Client) FetchPrice(ctx context.Context, pair string) (float64, error) {
	symbol := pairToSymbol(pair)
	url := fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", binanceSpotBase, symbol)

	var result struct {
		Price string `json:"price"`
	}
	if err := c.getJSON(ctx, url, &result); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(result.Price, 64)
}

// FetchLightSnapshot 轻量级快照：只获取价格、涨跌幅、短期K线和资金费率
// 用于关联币对参考（如 BTC），不拉新闻/社交/情绪等耗时数据
func (c *Client) FetchLightSnapshot(ctx context.Context, pair string) (CoinSnapshot, error) {
	symbol := pairToSymbol(pair)
	snap := CoinSnapshot{
		Pair:          pair,
		ShortInterval: "5m",
	}

	// 1. 24h ticker
	ticker, err := c.fetch24hTicker(ctx, symbol)
	if err != nil {
		return snap, fmt.Errorf("ticker %s: %w", symbol, err)
	}
	snap.Price = ticker.LastPrice
	snap.Change24hPct = ticker.PriceChangePercent

	// 2. 短期 K 线（5m x 50 = 4h，用于计算 RSI）
	shortKlines, err := c.fetchKlines(ctx, symbol, "5m", 50)
	if err != nil {
		log.Printf("[行情] 关联币对 %s 短期K线获取失败: %v", pair, err)
	} else {
		snap.ShortKlines = shortKlines
	}

	// 3. 资金费率（参考指标）
	snap.FundingRate, _ = c.fetchFundingRate(ctx, symbol)

	return snap, nil
}

// ---- internal methods ----

type tickerResult struct {
	LastPrice          float64
	PriceChangePercent float64
}

func (c *Client) fetch24hTicker(ctx context.Context, symbol string) (tickerResult, error) {
	url := fmt.Sprintf("%s/api/v3/ticker/24hr?symbol=%s", binanceSpotBase, symbol)

	var raw struct {
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
	}
	if err := c.getJSON(ctx, url, &raw); err != nil {
		return tickerResult{}, err
	}
	price, _ := strconv.ParseFloat(raw.LastPrice, 64)
	change, _ := strconv.ParseFloat(raw.PriceChangePercent, 64)
	return tickerResult{LastPrice: price, PriceChangePercent: change}, nil
}

func (c *Client) fetchKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		binanceSpotBase, symbol, interval, limit)

	var raw [][]json.RawMessage
	if err := c.getJSON(ctx, url, &raw); err != nil {
		return nil, err
	}

	klines := make([]Kline, 0, len(raw))
	for _, row := range raw {
		if len(row) < 12 {
			continue
		}
		k := Kline{
			OpenTime:  msToTime(row[0]),
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
			CloseTime: msToTime(row[6]),
		}
		klines = append(klines, k)
	}
	return klines, nil
}

func (c *Client) fetchFundingRate(ctx context.Context, symbol string) (float64, error) {
	url := fmt.Sprintf("%s/fapi/v1/fundingRate?symbol=%s&limit=1", binanceFuturesBase, symbol)

	var results []struct {
		FundingRate string `json:"fundingRate"`
	}
	if err := c.getJSON(ctx, url, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return strconv.ParseFloat(results[0].FundingRate, 64)
}

func (c *Client) fetchOpenInterest(ctx context.Context, symbol string) (float64, error) {
	url := fmt.Sprintf("%s/fapi/v1/openInterest?symbol=%s", binanceFuturesBase, symbol)

	var result struct {
		OpenInterest string `json:"openInterest"`
	}
	if err := c.getJSON(ctx, url, &result); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(result.OpenInterest, 64)
}

// fetchRatio gets long/short or buy/sell ratios from Binance futures data endpoints.
// endpoint: globalLongShortAccountRatio / topLongShortAccountRatio / topLongShortPositionRatio / takerlongshortRatio
func (c *Client) fetchRatio(ctx context.Context, symbol, endpoint string) (float64, error) {
	url := fmt.Sprintf("%s/futures/data/%s?symbol=%s&period=5m&limit=1",
		binanceFuturesBase, endpoint, symbol)

	var results []struct {
		LongShortRatio string `json:"longShortRatio"`
		BuySellRatio   string `json:"buySellRatio"`
	}
	if err := c.getJSON(ctx, url, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	val := results[0].LongShortRatio
	if val == "" {
		val = results[0].BuySellRatio
	}
	return strconv.ParseFloat(val, 64)
}

// fetchFearGreedIndex gets Fear & Greed Index from alternative.me (best effort).
func fetchFearGreedIndex(ctx context.Context, client *http.Client) (int, string, error) {
	url := "https://api.alternative.me/fng/?limit=1"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("fear greed API %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Value               string `json:"value"`
			ValueClassification string `json:"value_classification"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", err
	}
	if len(result.Data) == 0 {
		return 0, "", nil
	}
	val, _ := strconv.Atoi(result.Data[0].Value)
	return val, result.Data[0].ValueClassification, nil
}

// ---- HTTP helper ----

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Binance API %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---- helpers ----

func pairToSymbol(pair string) string {
	// "BTC/USDT" -> "BTCUSDT"
	out := ""
	for _, c := range pair {
		if c != '/' {
			out += string(c)
		}
	}
	return out
}

func msToTime(raw json.RawMessage) time.Time {
	var ms int64
	_ = json.Unmarshal(raw, &ms)
	return time.UnixMilli(ms)
}

func parseFloat(raw json.RawMessage) float64 {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var f float64
		_ = json.Unmarshal(raw, &f)
		return f
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
