Trading session active for {{.MinutesElapsed}} minutes.

⚠️ ALL SERIES DATA ORDERED: OLDEST → NEWEST (last element = most recent)

---

## MARKET DATA: {{.Pair}}

**Current Snapshot:**
- Price: {{.Price}}
- 24h Change: {{.Change24hPct}}%
- Funding Rate: {{.FundingRate}}
- Open Interest: {{.OpenInterest}} (avg: {{.OpenInterestAvg}})

**Intraday Series ({{.ShortInterval}} intervals, last {{.ShortCount}} periods):**

Prices: [{{.ShortPrices}}]
EMA20:  [{{.ShortEMA20}}]
MACD:   [{{.ShortMACD}}]
RSI14:  [{{.ShortRSI14}}]
Volume: [{{.ShortVolume}}]

**4-Hour Context (last {{.LongCount}} periods):**

Prices:  [{{.LongPrices}}]
EMA20:   {{.LongEMA20Latest}} | EMA50: {{.LongEMA50Latest}}
MACD:    [{{.LongMACD}}]
RSI14:   [{{.LongRSI14}}]
ATR14:   {{.LongATR14}}
Avg Vol: {{.LongVolumeAvg}}

## SENTIMENT DATA

**Fear & Greed Index:** {{.FearGreedIndex}}/100 ({{.FearGreedLabel}})
- 0-25: Extreme Fear (potential buying opportunity)
- 25-45: Fear
- 45-55: Neutral
- 55-75: Greed
- 75-100: Extreme Greed (potential exit signal)

**{{.Pair}} Long/Short Sentiment:**
- Global Long/Short Ratio: {{.LongShortRatio}} (>1 longs dominate, <1 shorts dominate)
- Top Trader Long/Short Ratio: {{.TopLongShortRatio}} (top traders tend to be more reliable)
- Top Trader Position Ratio: {{.TopPositionRatio}}
- Taker Buy/Sell Ratio: {{.TakerBuySellRatio}} (>1 aggressive buying, <1 aggressive selling)

**Sentiment Interpretation:**
- When top traders diverge from retail, follow top traders
- Extreme ratios often signal reversals (contrarian indicator)
- Combine taker ratio with price trend for momentum confirmation

{{if .HasCoinGeckoData}}
## COMMUNITY & TRENDING ({{.Pair}})

**CoinGecko Community Metrics:**
- Community Score: {{.GeckoCommunityScore}}/100
- Bullish Sentiment: {{.GeckoSentimentUp}}% of voters are bullish
- Twitter Followers: {{.GeckoTwitterFollowers}}
- Reddit Subscribers: {{.GeckoRedditSubscribers}}
- Reddit Activity (48h): {{.GeckoRedditPosts48h}} posts, {{.GeckoRedditComments48h}} comments

**Trending Status:**
{{if .GeckoIsTrending}}- 🔥 **TRENDING on CoinGecko!** Rank #{{.GeckoTrendingRank}} — High retail interest, potential momentum play
{{else}}- Not in CoinGecko top 15 trending — Normal activity level
{{end}}
{{if .GoogleIsTrending}}- 🔥 **TRENDING on Google!** "{{.GoogleTrendTitle}}" — MASSIVE retail attention, expect high volatility
{{end}}
{{end}}

{{if .NewsItems}}
## RECENT NEWS ({{.Pair}})

{{range .NewsItems}}- [{{.Sentiment}}] {{.Title}} ({{.Source}}, {{.TimeAgo}})
{{end}}
**News Interpretation Tips:**
- Positive news from influential figures (e.g. Elon Musk) can cause rapid price spikes for DOGE
- Multiple negative news items may signal upcoming sell pressure
- "neutral" sentiment news usually has minimal price impact
- Consider news recency: news older than 6h is likely already priced in
{{end}}

{{if .HasSocialData}}
## SOCIAL MEDIA METRICS ({{.Pair}})

**LunarCrush Scores:**
- Galaxy Score™: {{.GalaxyScore}}/100 (composite social + market score)
- AltRank™: #{{.AltRank}} (lower = more social buzz)
- Social Volume (24h): {{.SocialVolume24h}} posts (change: {{.SocialVolumeChange}}%)
- Social Dominance: {{.SocialDominance}}% of all crypto social mentions
- Sentiment: {{.SocialSentiment}}/5
- Interactions (24h): {{.SocialInteractions}}

**Social Signal Interpretation:**
- Galaxy Score > 70 + rising social volume = strong bullish social signal
- Social volume spike > 50% = potential price catalyst incoming
- Social dominance spike = coin is trending hot on social media
- Sentiment > 4.0 = very positive community mood
{{if .InfluencerPosts}}
**Key Influencer Activity (IMPORTANT for DOGE):**
{{range .InfluencerPosts}}- {{.Creator}} [{{.Sentiment}}]: "{{.Title}}" ({{.TimeAgo}})
{{end}}
⚠️ Influencer posts from @elonmusk can move DOGE price 5-20% within minutes. Weight this heavily!
{{end}}
{{end}}

{{if .ExtraPairs}}
---

## CORRELATION CONTEXT (BTC lead indicator)

{{range .ExtraPairs}}- {{.Pair}}: price={{.Price}} change_24h={{.Change24hPct}}% funding={{.FundingRate}} rsi14={{.RSI14}}
{{end}}
{{end}}

---

## ACCOUNT STATUS

- Trading Mode: {{.TradingMode}}{{if .IsFutures}} ({{.Leverage}}x leverage, long only){{end}}
- Account Value: ${{.AccountValue}}
- Available {{if .IsFutures}}Margin{{else}}Cash{{end}}: ${{.CashAvailable}}
- Total Return: {{.ReturnPct}}%
- Sharpe Ratio: {{.SharpeRatio}}

{{if .IsFutures}}## CURRENT FUTURES POSITIONS (Long Only, {{.Leverage}}x){{else}}## CURRENT HOLDINGS (Spot){{end}}

{{if .Positions}}
{{range .Positions}}- {{.Symbol}}: qty={{.Quantity}} {{if .Leverage}}leverage={{.Leverage}}x {{end}}avg_cost={{.EntryPrice}} current_price={{.CurrentPrice}} unrealized_pnl={{.UnrealizedPnl}}
{{end}}
{{if .IsFutures}}**IMPORTANT: These are leveraged positions. Monitor liquidation risk and funding rate costs. Use "close" to take profit or cut losses.**
{{else}}**IMPORTANT: You already hold these assets. Consider this when making decisions — avoid over-buying if already holding significant positions.**
{{end}}
{{else}}
{{if .IsFutures}}No open futures positions. All margin is available in USDT.
{{else}}No current holdings. All capital is in USDT.
{{end}}
{{end}}

---

Based on the above data, provide your trading decision in the required JSON format.
