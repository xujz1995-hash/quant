package market

import (
	"context"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"strings"
)

// GoogleTrendsData 保存 Google Trends 检查结果
type GoogleTrendsData struct {
	IsTrending bool   // 是否出现在 Google 每日热搜
	Title      string // 匹配到的热搜词条（如 "Dogecoin price"）
}

// rssItem RSS feed 中的单个条目
type rssItem struct {
	Title string `xml:"title"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

// fetchGoogleTrends 检查币种是否出现在 Google 每日热搜中。
// 使用 Google Trends 公开 RSS feed，完全免费，无需 API key。
// 失败时静默返回空数据。
func (c *Client) fetchGoogleTrends(ctx context.Context, pair string) GoogleTrendsData {
	coin := strings.ToLower(strings.Split(pair, "/")[0])

	// 搜索关键词：币名和全称
	keywords := coinToKeywords(coin)

	// Google Trends 每日热搜 RSS（美国区，加密货币用户集中）
	geos := []string{"US"}

	for _, geo := range geos {
		url := "https://trends.google.com/trends/trendingsearches/daily/rss?geo=" + geo

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AIQuant/1.0)")

		resp, err := c.http.Do(req)
		if err != nil {
			log.Printf("[热搜] Google Trends RSS 请求失败: %v，跳过", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("[热搜] Google Trends RSS 返回 HTTP %d，跳过", resp.StatusCode)
			continue
		}

		var feed rssFeed
		if err := xml.Unmarshal(body, &feed); err != nil {
			log.Printf("[热搜] 解析 Google Trends RSS 失败: %v", err)
			continue
		}

		// 在热搜条目中查找与币种相关的关键词
		for _, item := range feed.Channel.Items {
			title := strings.ToLower(item.Title)
			for _, kw := range keywords {
				if strings.Contains(title, kw) {
					log.Printf("[热搜] 🔥 %s 出现在 Google 热搜！匹配: %q", strings.ToUpper(coin), item.Title)
					return GoogleTrendsData{
						IsTrending: true,
						Title:      item.Title,
					}
				}
			}
		}
	}

	return GoogleTrendsData{}
}

// coinToKeywords 将币种缩写映射为搜索关键词列表
func coinToKeywords(coin string) []string {
	base := []string{coin}
	extra := map[string][]string{
		"btc":  {"bitcoin"},
		"eth":  {"ethereum"},
		"sol":  {"solana"},
		"bnb":  {"binance coin"},
		"doge": {"dogecoin", "doge coin", "elon musk doge", "elon doge"},
		"xrp":  {"ripple", "xrp"},
	}
	if kws, ok := extra[coin]; ok {
		base = append(base, kws...)
	}
	return base
}
