package market

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// PromptData holds all template fields for UserPrompt.md.
type PromptData struct {
	MinutesElapsed int
	Pair           string

	// Current snapshot
	Price        string
	Change24hPct string
	FundingRate  string
	OpenInterest string
	OpenInterestAvg string

	// Short-term series
	ShortInterval string
	ShortCount    int
	ShortPrices   string
	ShortEMA20    string
	ShortMACD     string
	ShortRSI14    string
	ShortVolume   string

	// Long-term (4h)
	LongCount       int
	LongPrices      string
	LongEMA20Latest string
	LongEMA50Latest string
	LongMACD        string
	LongRSI14       string
	LongATR14       string
	LongVolumeAvg   string

	// 情绪因子
	LongShortRatio    string
	TopLongShortRatio string
	TopPositionRatio  string
	TakerBuySellRatio string
	FearGreedIndex    string
	FearGreedLabel    string

	// News (from CryptoPanic, may be empty)
	NewsItems []NewsItemData

	// CoinGecko community data (free, always available)
	HasCoinGeckoData    bool
	GeckoIsTrending     bool
	GeckoTrendingRank   string
	GeckoCommunityScore string
	GeckoSentimentUp    string
	GeckoTwitterFollowers  string
	GeckoRedditSubscribers string
	GeckoRedditPosts48h    string
	GeckoRedditComments48h string

	// Google Trends (free)
	GoogleIsTrending bool
	GoogleTrendTitle string

	// Social media metrics (from LunarCrush, may be empty)
	HasSocialData      bool
	GalaxyScore        string
	AltRank            string
	SocialVolume24h    string
	SocialDominance    string
	SocialSentiment    string
	SocialInteractions string
	SocialVolumeChange string
	InfluencerPosts    []InfluencerPostData

	// Extra pairs for correlation context
	ExtraPairs []ExtraPairData

	// Account
	AccountValue  string
	CashAvailable string
	ReturnPct     string
	SharpeRatio   string

	// Trading mode
	TradingMode string // "spot" 或 "futures"
	Leverage    string // 杠杆倍数
	IsFutures   bool

	// Positions
	Positions []PositionData
}

// NewsItemData holds a single news item for prompt rendering.
type NewsItemData struct {
	Title     string
	Source    string
	Sentiment string
	TimeAgo   string
}

// InfluencerPostData holds a KOL post for prompt rendering.
type InfluencerPostData struct {
	Creator   string
	Title     string
	TimeAgo   string
	Sentiment string
}

// ExtraPairData holds summary data for correlation context.
type ExtraPairData struct {
	Pair         string
	Price        string
	Change24hPct string
	FundingRate  string
	RSI14        string
}

// PositionData holds current position info.
type PositionData struct {
	Symbol       string
	Side         string
	Quantity     string
	EntryPrice   string
	CurrentPrice string
	UnrealizedPnl string
	Leverage     string
	ProfitTarget string
	StopLoss     string
}

// BuildPrompt generates the user prompt from a CoinSnapshot and account info.
func BuildPrompt(tmpl string, snap CoinSnapshot, account AccountInfo, extraSnaps []CoinSnapshot) (string, error) {
	data := buildPromptData(snap, account, extraSnaps)

	t, err := template.New("prompt").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute prompt template: %w", err)
	}
	return buf.String(), nil
}

// AccountInfo carries portfolio state for prompt rendering.
type AccountInfo struct {
	AccountValue   float64
	CashAvailable  float64
	ReturnPct      float64
	SharpeRatio    float64
	MinutesElapsed int
	TradingMode    string // "spot" 或 "futures"
	Leverage       int    // 杠杆倍数
	Positions      []PositionData
}

func buildPromptData(snap CoinSnapshot, account AccountInfo, extras []CoinSnapshot) PromptData {
	// Short-term indicators
	shortCloses := extractCloses(snap.ShortKlines)
	shortEMA20 := EMA(shortCloses, 20)
	shortMACD := MACD(shortCloses)
	shortRSI14 := RSI(shortCloses, 14)
	shortVols := extractVolumes(snap.ShortKlines)

	// Long-term indicators
	longCloses := extractCloses(snap.LongKlines)
	longHighs := extractHighs(snap.LongKlines)
	longLows := extractLows(snap.LongKlines)
	longEMA20 := EMA(longCloses, 20)
	longEMA50 := EMA(longCloses, 50)
	longMACD := MACD(longCloses)
	longRSI14 := RSI(longCloses, 14)
	longATR14 := ATR(longHighs, longLows, longCloses, 14)
	longVols := extractVolumes(snap.LongKlines)

	// Take last 10 for short series to keep prompt concise
	shortN := min(len(shortCloses), 10)

	data := PromptData{
		MinutesElapsed: account.MinutesElapsed,
		Pair:           snap.Pair,

		Price:        ff(snap.Price, pricePrecision(snap.Pair)),
		Change24hPct: ff(snap.Change24hPct, 2),
		FundingRate:  ff(snap.FundingRate, 6),
		OpenInterest: ff(snap.OpenInterest, 2),
		OpenInterestAvg: "N/A",

		ShortInterval: snap.ShortInterval,
		ShortCount:    shortN,
		ShortPrices:   joinLast(shortCloses, shortN, pricePrecision(snap.Pair)),
		ShortEMA20:    joinLast(shortEMA20, shortN, pricePrecision(snap.Pair)),
		ShortMACD:     joinLast(shortMACD, shortN, 4),
		ShortRSI14:    joinLast(shortRSI14, shortN, 1),
		ShortVolume:   joinLast(shortVols, shortN, 0),

		LongCount:       len(longCloses),
		LongPrices:      joinLast(longCloses, min(len(longCloses), 10), pricePrecision(snap.Pair)),
		LongEMA20Latest: lastFF(longEMA20, pricePrecision(snap.Pair)),
		LongEMA50Latest: lastFF(longEMA50, pricePrecision(snap.Pair)),
		LongMACD:        joinLast(longMACD, min(len(longMACD), 10), 4),
		LongRSI14:       joinLast(longRSI14, min(len(longRSI14), 10), 1),
		LongATR14:       lastFF(longATR14, pricePrecision(snap.Pair)),
		LongVolumeAvg:   ff(avg(longVols), 0),

		LongShortRatio:    ff(snap.Sentiment.LongShortRatio, 4),
		TopLongShortRatio: ff(snap.Sentiment.TopLongShortRatio, 4),
		TopPositionRatio:  ff(snap.Sentiment.TopPositionRatio, 4),
		TakerBuySellRatio: ff(snap.Sentiment.TakerBuySellRatio, 4),
		FearGreedIndex:    fmt.Sprintf("%d", snap.Sentiment.FearGreedIndex),
		FearGreedLabel:    snap.Sentiment.FearGreedLabel,

		AccountValue:  ff(account.AccountValue, 2),
		CashAvailable: ff(account.CashAvailable, 2),
		ReturnPct:     ff(account.ReturnPct, 2),
		SharpeRatio:   ff(account.SharpeRatio, 2),
		TradingMode:   account.TradingMode,
		Leverage:      fmt.Sprintf("%d", account.Leverage),
		IsFutures:     account.TradingMode == "futures",
		Positions:     account.Positions,
	}

	// CoinGecko data (always attempt, free)
	cg := snap.CoinGecko
	if cg.CommunityScore > 0 || cg.IsTrending {
		data.HasCoinGeckoData = true
		data.GeckoIsTrending = cg.IsTrending
		data.GeckoTrendingRank = fmt.Sprintf("%d", cg.TrendingRank)
		data.GeckoCommunityScore = ff(cg.CommunityScore, 0)
		data.GeckoSentimentUp = ff(cg.SentimentVotesUpPct, 1)
		data.GeckoTwitterFollowers = formatLargeNumber(cg.TwitterFollowers)
		data.GeckoRedditSubscribers = formatLargeNumber(cg.RedditSubscribers)
		data.GeckoRedditPosts48h = ff(cg.RedditActivePosts48h, 1)
		data.GeckoRedditComments48h = ff(cg.RedditActiveComments48h, 0)
	}

	// Google Trends
	data.GoogleIsTrending = snap.GoogleTrends.IsTrending
	data.GoogleTrendTitle = snap.GoogleTrends.Title

	// Social media metrics (LunarCrush)
	if snap.Social.GalaxyScore > 0 || snap.Social.SocialVolume24h > 0 {
		data.HasSocialData = true
		data.GalaxyScore = ff(snap.Social.GalaxyScore, 0)
		data.AltRank = fmt.Sprintf("%d", snap.Social.AltRank)
		data.SocialVolume24h = fmt.Sprintf("%d", snap.Social.SocialVolume24h)
		data.SocialDominance = ff(snap.Social.SocialDominance, 2)
		data.SocialSentiment = ff(snap.Social.SentimentScore, 1)
		data.SocialInteractions = fmt.Sprintf("%d", snap.Social.Interactions24h)
		data.SocialVolumeChange = ff(snap.Social.SocialVolumeChange, 1)

		for _, p := range snap.Social.InfluencerPosts {
			sentLabel := "neutral"
			if p.Sentiment > 3.5 {
				sentLabel = "positive"
			} else if p.Sentiment < 2.5 {
				sentLabel = "negative"
			}
			data.InfluencerPosts = append(data.InfluencerPosts, InfluencerPostData{
				Creator:   p.Creator,
				Title:     p.Title,
				TimeAgo:   p.TimeAgo,
				Sentiment: sentLabel,
			})
		}
	}

	// News items
	for _, n := range snap.News {
		data.NewsItems = append(data.NewsItems, NewsItemData{
			Title:     n.Title,
			Source:    n.Source,
			Sentiment: n.Sentiment,
			TimeAgo:   n.TimeAgo,
		})
	}

	// Extra pairs for correlation
	for _, es := range extras {
		ec := extractCloses(es.ShortKlines)
		eRSI := RSI(ec, 14)
		data.ExtraPairs = append(data.ExtraPairs, ExtraPairData{
			Pair:         es.Pair,
			Price:        ff(es.Price, pricePrecision(es.Pair)),
			Change24hPct: ff(es.Change24hPct, 2),
			FundingRate:  ff(es.FundingRate, 6),
			RSI14:        lastFF(eRSI, 1),
		})
	}

	return data
}

// ---- helpers ----

func extractCloses(klines []Kline) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = k.Close
	}
	return out
}

func extractHighs(klines []Kline) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = k.High
	}
	return out
}

func extractLows(klines []Kline) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = k.Low
	}
	return out
}

func extractVolumes(klines []Kline) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = k.Volume
	}
	return out
}

func ff(v float64, decimals int) string {
	return fmt.Sprintf("%.*f", decimals, v)
}

func joinLast(s []float64, n int, decimals int) string {
	if len(s) == 0 {
		return "N/A"
	}
	start := len(s) - n
	if start < 0 {
		start = 0
	}
	parts := make([]string, 0, n)
	for _, v := range s[start:] {
		parts = append(parts, ff(v, decimals))
	}
	return strings.Join(parts, ", ")
}

func lastFF(s []float64, decimals int) string {
	if len(s) == 0 {
		return "N/A"
	}
	return ff(s[len(s)-1], decimals)
}

func avg(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	return sum / float64(len(s))
}

func formatLargeNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func pricePrecision(pair string) int {
	p := strings.ToUpper(pair)
	switch {
	case strings.HasPrefix(p, "BTC"):
		return 2
	case strings.HasPrefix(p, "ETH"):
		return 2
	case strings.HasPrefix(p, "BNB"):
		return 2
	case strings.HasPrefix(p, "DOGE"), strings.HasPrefix(p, "XRP"):
		return 4
	default:
		return 4
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

