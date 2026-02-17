package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// NewsItem 表示一条加密货币新闻（来自 CryptoPanic）
type NewsItem struct {
	Title       string
	PublishedAt time.Time
	Source      string
	Sentiment   string // positive / negative / neutral
	TimeAgo     string // 人类可读的时间差，如 "2h ago"
}

// fetchNews 从 CryptoPanic 获取指定币种的最新新闻。
// 任何错误（无 key、额度耗尽、网络异常）都返回 nil，不影响主流程。
func (c *Client) fetchNews(ctx context.Context, pair string) []NewsItem {
	if c.CryptoPanicKey == "" {
		return nil
	}

	// "DOGE/USDT" → "DOGE"
	coin := strings.Split(pair, "/")[0]

	url := fmt.Sprintf(
		"https://cryptopanic.com/api/v1/posts/?auth_token=%s&currencies=%s&kind=news&public=true",
		c.CryptoPanicKey, coin,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("[新闻] 创建请求失败: %v", err)
		return nil
	}

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[新闻] 请求 CryptoPanic 失败: %v，跳过新闻数据", err)
		return nil
	}
	defer resp.Body.Close()

	// 非 200（含 429 额度耗尽）→ 静默跳过
	if resp.StatusCode != http.StatusOK {
		log.Printf("[新闻] CryptoPanic 返回 HTTP %d（额度用完或其他错误），跳过新闻数据", resp.StatusCode)
		return nil
	}

	var result struct {
		Results []struct {
			Title     string `json:"title"`
			CreatedAt string `json:"created_at"`
			Source    struct {
				Title string `json:"title"`
			} `json:"source"`
			Votes struct {
				Positive  int `json:"positive"`
				Negative  int `json:"negative"`
				Important int `json:"important"`
			} `json:"votes"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[新闻] 解析 CryptoPanic 响应失败: %v，跳过新闻数据", err)
		return nil
	}

	// 最多取 5 条最新新闻
	limit := 5
	if len(result.Results) < limit {
		limit = len(result.Results)
	}

	now := time.Now()
	items := make([]NewsItem, 0, limit)
	for _, r := range result.Results[:limit] {
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)

		// 根据投票判断情绪倾向
		sentiment := "neutral"
		if r.Votes.Positive > r.Votes.Negative*2 {
			sentiment = "positive"
		} else if r.Votes.Negative > r.Votes.Positive*2 {
			sentiment = "negative"
		}

		items = append(items, NewsItem{
			Title:       sanitizeNewsTitle(r.Title),
			PublishedAt: t,
			Source:      r.Source.Title,
			Sentiment:   sentiment,
			TimeAgo:     humanTimeAgo(now, t),
		})
	}

	log.Printf("[新闻] 获取到 %d 条 %s 相关新闻", len(items), coin)
	return items
}

// sanitizeNewsTitle 清洗新闻标题中可能触发内容安全过滤的敏感词
func sanitizeNewsTitle(title string) string {
	// 替换可能触发中国大模型内容审核的词汇
	replacer := strings.NewReplacer(
		"hack", "security incident",
		"Hack", "Security Incident",
		"HACK", "SECURITY INCIDENT",
		"scam", "fraud risk",
		"Scam", "Fraud Risk",
		"SCAM", "FRAUD RISK",
		"kill", "eliminate",
		"Kill", "Eliminate",
		"attack", "incident",
		"Attack", "Incident",
		"bomb", "surge",
		"Bomb", "Surge",
		"crash", "sharp decline",
		"Crash", "Sharp Decline",
		"drug", "substance",
		"Drug", "Substance",
		"terror", "risk event",
		"Terror", "Risk Event",
		"war", "conflict",
		"War", "Conflict",
		"weapon", "tool",
		"Weapon", "Tool",
		"launder", "transfer",
		"Launder", "Transfer",
		"ponzi", "pyramid scheme",
		"Ponzi", "Pyramid Scheme",
	)
	return replacer.Replace(title)
}

// humanTimeAgo 返回人类可读的时间差
func humanTimeAgo(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
