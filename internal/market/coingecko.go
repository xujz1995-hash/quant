package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const coingeckoBase = "https://api.coingecko.com/api/v3"

// CoinGeckoData 保存 CoinGecko 社区与趋势数据
type CoinGeckoData struct {
	// 是否在 CoinGecko 热门趋势中（top 15）
	IsTrending   bool
	TrendingRank int // 1=最热，0=不在榜

	// 社区数据
	CommunityScore        float64
	TwitterFollowers      int
	RedditSubscribers     int
	RedditActivePosts48h  float64
	RedditActiveComments48h float64
	SentimentVotesUpPct   float64 // 看涨投票占比 %
}

// coinToGeckoID 将交易对映射为 CoinGecko coin id
func coinToGeckoID(pair string) string {
	coin := strings.ToLower(strings.Split(pair, "/")[0])
	mapping := map[string]string{
		"btc":  "bitcoin",
		"eth":  "ethereum",
		"sol":  "solana",
		"bnb":  "binancecoin",
		"doge": "dogecoin",
		"xrp":  "ripple",
	}
	if id, ok := mapping[coin]; ok {
		return id
	}
	return coin
}

// fetchCoinGeckoData 从 CoinGecko 获取趋势和社区数据。
// 完全免费，无需 API key。失败时静默跳过。
func (c *Client) fetchCoinGeckoData(ctx context.Context, pair string) CoinGeckoData {
	var data CoinGeckoData
	coinID := coinToGeckoID(pair)
	symbol := strings.ToUpper(strings.Split(pair, "/")[0])

	// 1. 检查是否在趋势榜
	data.IsTrending, data.TrendingRank = c.checkCoinGeckoTrending(ctx, symbol)
	if data.IsTrending {
		log.Printf("[社区] %s 在 CoinGecko 趋势榜排名 #%d 🔥", symbol, data.TrendingRank)
	}

	// 2. 获取社区数据
	c.fetchCoinGeckoCommunity(ctx, coinID, &data)

	return data
}

// checkCoinGeckoTrending 检查币种是否在 CoinGecko 趋势 top 15
func (c *Client) checkCoinGeckoTrending(ctx context.Context, symbol string) (bool, int) {
	url := coingeckoBase + "/search/trending"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, 0
	}

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[社区] CoinGecko trending 请求失败: %v，跳过", err)
		return false, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[社区] CoinGecko trending 返回 HTTP %d，跳过", resp.StatusCode)
		return false, 0
	}

	var result struct {
		Coins []struct {
			Item struct {
				Symbol string `json:"symbol"`
				Score  int    `json:"score"` // 0 = most trending
			} `json:"item"`
		} `json:"coins"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[社区] 解析 CoinGecko trending 失败: %v", err)
		return false, 0
	}

	for _, coin := range result.Coins {
		if strings.EqualFold(coin.Item.Symbol, symbol) {
			rank := coin.Item.Score + 1 // score 0 → rank 1
			return true, rank
		}
	}

	return false, 0
}

// fetchCoinGeckoCommunity 获取币种的社区指标
func (c *Client) fetchCoinGeckoCommunity(ctx context.Context, coinID string, data *CoinGeckoData) {
	url := fmt.Sprintf(
		"%s/coins/%s?localization=false&tickers=false&market_data=false&community_data=true&developer_data=false&sparkline=false",
		coingeckoBase, coinID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[社区] CoinGecko coin detail 请求失败: %v，跳过社区数据", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[社区] CoinGecko coin detail 返回 HTTP %d，跳过社区数据", resp.StatusCode)
		return
	}

	var result struct {
		CommunityScore float64 `json:"community_score"`
		SentimentUp    float64 `json:"sentiment_votes_up_percentage"`
		CommunityData  struct {
			TwitterFollowers   int     `json:"twitter_followers"`
			RedditSubscribers  int     `json:"reddit_subscribers"`
			RedditAvgPosts48h  float64 `json:"reddit_average_posts_48h"`
			RedditAvgComments  float64 `json:"reddit_average_comments_48h"`
			RedditActive48h    int     `json:"reddit_accounts_active_48h"`
		} `json:"community_data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[社区] 解析 CoinGecko coin detail 失败: %v", err)
		return
	}

	data.CommunityScore = result.CommunityScore
	data.SentimentVotesUpPct = result.SentimentUp
	data.TwitterFollowers = result.CommunityData.TwitterFollowers
	data.RedditSubscribers = result.CommunityData.RedditSubscribers
	data.RedditActivePosts48h = result.CommunityData.RedditAvgPosts48h
	data.RedditActiveComments48h = result.CommunityData.RedditAvgComments

	log.Printf("[社区] CoinGecko %s: 社区评分=%.0f 看涨投票=%.1f%% Twitter粉丝=%d Reddit订阅=%d",
		coinID, data.CommunityScore, data.SentimentVotesUpPct,
		data.TwitterFollowers, data.RedditSubscribers)
}
