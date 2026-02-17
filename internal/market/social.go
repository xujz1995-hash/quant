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

const lunarCrushBase = "https://lunarcrush.com/api4"

// SocialMetrics 保存 LunarCrush 社交媒体指标
type SocialMetrics struct {
	GalaxyScore     float64 // 综合评分 0-100（社交+市场）
	AltRank         int     // 排名（越小越热）
	SocialVolume24h int     // 24h 社交提及量
	SocialDominance float64 // 社交关注占比 %
	SentimentScore  float64 // 情绪评分
	Interactions24h int     // 24h 社交互动量

	// 与前 24h 对比
	SocialVolumeChange float64 // 社交量变化百分比

	// 关键 KOL 最新动态（如马斯克）
	InfluencerPosts []InfluencerPost
}

// InfluencerPost 关键意见领袖的最新帖子
type InfluencerPost struct {
	Creator   string
	Title     string
	TimeAgo   string
	Sentiment float64 // 帖子情绪
}

// coinToTopic 将交易对映射为 LunarCrush topic 名称
func coinToTopic(pair string) string {
	coin := strings.ToLower(strings.Split(pair, "/")[0])
	mapping := map[string]string{
		"btc":  "bitcoin",
		"eth":  "ethereum",
		"sol":  "solana",
		"bnb":  "bnb",
		"doge": "dogecoin",
		"xrp":  "xrp",
	}
	if topic, ok := mapping[coin]; ok {
		return topic
	}
	return coin
}

// fetchSocialMetrics 从 LunarCrush 获取社交指标。
// 无 key 或请求失败 → 返回零值，不影响主流程。
func (c *Client) fetchSocialMetrics(ctx context.Context, pair string) SocialMetrics {
	if c.LunarCrushKey == "" {
		return SocialMetrics{}
	}

	var metrics SocialMetrics

	// 1. Topic 社交概览（24h 聚合）
	topic := coinToTopic(pair)
	topicData := c.lunarGet(ctx, fmt.Sprintf("/public/topic/%s/v1", topic))
	if topicData != nil {
		if data, ok := topicData["data"].(map[string]interface{}); ok {
			metrics.GalaxyScore = toFloat(data["galaxy_score"])
			metrics.AltRank = int(toFloat(data["alt_rank"]))
			metrics.SocialVolume24h = int(toFloat(data["num_posts"]))
			metrics.SocialDominance = toFloat(data["social_dominance"])
			metrics.Interactions24h = int(toFloat(data["interactions_24h"]))

			// 情绪：0-5 尺度
			metrics.SentimentScore = toFloat(data["sentiment"])

			// 社交量变化
			prevVolume := toFloat(data["num_posts_previous"])
			if prevVolume > 0 {
				metrics.SocialVolumeChange = (float64(metrics.SocialVolume24h) - prevVolume) / prevVolume * 100
			}
		}
		log.Printf("[社交] LunarCrush topic=%s: GalaxyScore=%.0f SocialVol=%d Sentiment=%.1f Dominance=%.2f%%",
			topic, metrics.GalaxyScore, metrics.SocialVolume24h, metrics.SentimentScore, metrics.SocialDominance)
	}

	// 2. 马斯克最新推文（对 DOGE 尤其重要）
	coin := strings.ToLower(strings.Split(pair, "/")[0])
	if coin == "doge" {
		posts := c.fetchInfluencerPosts(ctx, "twitter", "elonmusk")
		metrics.InfluencerPosts = posts
	}

	return metrics
}

// fetchInfluencerPosts 获取指定 KOL 的最新热帖
func (c *Client) fetchInfluencerPosts(ctx context.Context, network, username string) []InfluencerPost {
	raw := c.lunarGet(ctx, fmt.Sprintf("/public/creator/%s/%s/posts/v1", network, username))
	if raw == nil {
		return nil
	}

	dataArr, ok := raw["data"].([]interface{})
	if !ok {
		return nil
	}

	now := time.Now()
	limit := 3
	if len(dataArr) < limit {
		limit = len(dataArr)
	}

	posts := make([]InfluencerPost, 0, limit)
	for _, item := range dataArr[:limit] {
		post, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		title := toString(post["post_title"])
		if title == "" {
			title = toString(post["post_description"])
		}
		// 截取前 200 字符
		if len(title) > 200 {
			title = title[:200] + "..."
		}

		createdAt := int64(toFloat(post["post_created"]))
		timeAgo := ""
		if createdAt > 0 {
			t := time.Unix(createdAt, 0)
			timeAgo = humanTimeAgo(now, t)
		}

		posts = append(posts, InfluencerPost{
			Creator:   "@" + username,
			Title:     sanitizeNewsTitle(title),
			TimeAgo:   timeAgo,
			Sentiment: toFloat(post["sentiment"]),
		})
	}

	if len(posts) > 0 {
		log.Printf("[社交] @%s 最新 %d 条帖子已获取", username, len(posts))
	}

	return posts
}

// lunarGet 发起 LunarCrush API GET 请求（带 Bearer Token）
// 任何错误返回 nil（静默失败）
func (c *Client) lunarGet(ctx context.Context, path string) map[string]interface{} {
	url := lunarCrushBase + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("[社交] 创建请求失败: %v", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.LunarCrushKey)

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[社交] LunarCrush 请求失败: %v，跳过社交数据", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[社交] LunarCrush 返回 HTTP %d（额度不足或无权限），跳过社交数据", resp.StatusCode)
		return nil
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[社交] 解析 LunarCrush 响应失败: %v", err)
		return nil
	}
	return result
}

// ---- helpers ----

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
