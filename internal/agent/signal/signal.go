package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"ai_quant/internal/auth"
	"ai_quant/internal/config"
	"ai_quant/internal/domain"
	"ai_quant/internal/market"

	"github.com/google/uuid"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

type Input struct {
	CycleID  string
	Pair     string
	Snapshot domain.MarketSnapshot
}

type Agent interface {
	Generate(ctx context.Context, input Input) (domain.Signal, error)
}

type RuleBasedAgent struct{}

type llmResponse struct {
	Signal        string  `json:"signal"`
	Side          string  `json:"side"`
	Coin          string  `json:"coin"`
	Confidence    float64 `json:"confidence"`
	Thinking      string  `json:"thinking"`
	Reason        string  `json:"reason"`
	Justification string  `json:"justification"`
	TTLSeconds    int     `json:"ttl_seconds"`
}

// AccountDataFunc 获取真实账户数据的回调函数
type AccountDataFunc func(ctx context.Context, pair string) (balance float64, positions []market.PositionData)

type LangChainAgent struct {
	model          llms.Model
	fallback       Agent
	marketClient   *market.Client
	systemPrompt   string
	userTemplate   string
	startTime      time.Time
	getAccountData AccountDataFunc // 由 orchestrator 注入
	tradingMode    string          // "spot" 或 "futures"
	leverage       int             // 杠杆倍数
	modelName      string          // 模型名称
}

func New(cfg config.Config) Agent {
	return NewWithAuth(cfg, nil)
}

func NewWithAuth(cfg config.Config, authService *auth.Service) Agent {
	fallback := &RuleBasedAgent{}

	// 创建 LLM 认证管理器
	authMode := auth.AuthMode(cfg.LLMAuthMode)
	provider := auth.Provider(cfg.LLMAuthProvider)
	authManager := auth.NewLLMAuthManager(authService, cfg.OpenAIAPIKey, authMode, provider)

	// 获取认证 token
	token, err := authManager.GetToken()
	if err != nil {
		log.Printf("[信号] 获取认证失败: %v，使用规则引擎", err)
		return fallback
	}

	// 显示认证状态
	status := authManager.GetStatus()
	log.Printf("[信号] LLM 认证模式=%s 提供商=%s OAuth可用=%v",
		status["mode"], status["provider"], status["oauth_available"])

	opts := []openai.Option{
		openai.WithToken(token),
		openai.WithModel(cfg.OpenAIModel),
	}
	if strings.TrimSpace(cfg.OpenAIBaseURL) != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAIBaseURL))
	}

	llm, err := openai.New(opts...)
	if err != nil {
		log.Printf("[信号] 初始化大模型客户端失败: %v，使用规则引擎", err)
		return fallback
	}

	sysProm := loadFile("SystemPrompt.md")
	userTmpl := loadFile("UserPrompt.md")

	log.Printf("[信号] 大模型已就绪 模型=%s 系统提示词=%d字符 用户模板=%d字符",
		cfg.OpenAIModel, len(sysProm), len(userTmpl))

	mc := market.NewClient()
	mc.CryptoPanicKey = cfg.CryptoPanicAPIKey
	mc.LunarCrushKey = cfg.LunarCrushAPIKey

	return &LangChainAgent{
		model:        llm,
		fallback:     fallback,
		marketClient: mc,
		systemPrompt: sysProm,
		userTemplate: userTmpl,
		startTime:    time.Now(),
		modelName:    cfg.OpenAIModel,
	}
}

// SetAccountDataFunc 设置账户数据回调（由 orchestrator 在启动时注入）
func SetAccountDataFunc(agent Agent, fn AccountDataFunc) {
	if lca, ok := agent.(*LangChainAgent); ok {
		lca.getAccountData = fn
	}
}

// SetTradingMode 设置交易模式信息（由 orchestrator 在启动时注入）
func SetTradingMode(agent Agent, mode string, leverage int) {
	if lca, ok := agent.(*LangChainAgent); ok {
		lca.tradingMode = mode
		lca.leverage = leverage
	}
}

func loadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[信号] 加载文件 %s 失败: %v", path, err)
		return ""
	}
	return string(data)
}

func (a *RuleBasedAgent) Generate(_ context.Context, input Input) (domain.Signal, error) {
	now := time.Now().UTC()
	side := domain.SideNone
	confidence := 0.5
	reason := "市场中性，无明确方向"
	ttl := 300

	if input.Snapshot.Change24h >= 1.2 && input.Snapshot.FundingRate <= 0.01 {
		side = domain.SideLong
		confidence = clamp(0.55+math.Abs(input.Snapshot.Change24h)/25, 0.55, 0.9)
		reason = "动量为正且资金费率可接受"
	}
	if input.Snapshot.Change24h <= -1.2 && input.Snapshot.FundingRate >= -0.01 {
		side = domain.SideShort
		confidence = clamp(0.55+math.Abs(input.Snapshot.Change24h)/25, 0.55, 0.9)
		reason = "动量为负且资金费率可接受"
	}

	return domain.Signal{
		ID:         uuid.NewString(),
		CycleID:    input.CycleID,
		Pair:       input.Pair,
		Side:       side,
		Confidence: confidence,
		Reason:     reason,
		ModelName:  "rule-based",
		TTLSeconds: ttl,
		CreatedAt:  now,
	}, nil
}

func (a *LangChainAgent) Generate(ctx context.Context, input Input) (domain.Signal, error) {
	// 从币安获取实时行情
	log.Printf("[信号] 正在从 Binance 获取 %s 的行情数据 ...", input.Pair)
	t0 := time.Now()
	userPrompt, err := a.buildUserPrompt(ctx, input)
	if err != nil {
		log.Printf("[信号] ⚠️ Binance 数据获取失败 (耗时%s): %v，使用简化提示词", time.Since(t0), err)
		userPrompt = a.buildSimplePrompt(input)
	} else {
		log.Printf("[信号] ✔ 行情数据就绪 (耗时%s)，提示词长度=%d字符", time.Since(t0), len(userPrompt))
	}

	// 根据交易模式动态调整系统提示词
	sysPrompt := a.adaptSystemPrompt()
	log.Printf("[信号] 系统提示词已加载=%v (%d字符) 模式=%s", sysPrompt != "", len(sysPrompt), a.tradingMode)

	// 组装消息：系统提示词 + 用户提示词
	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: sysPrompt}},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: userPrompt}},
		},
	}

	// 调试日志：打印完整用户提示词（便于排查敏感词问题）
	log.Printf("[信号] 用户提示词内容:\n%s", userPrompt)

	log.Printf("[信号] 正在调用大模型 ...")
	t1 := time.Now()
	resp, err := a.model.GenerateContent(ctx, messages)
	llmElapsed := time.Since(t1)
	if err != nil {
		log.Printf("[信号] ✘ 大模型调用失败 (耗时%s): %v → 降级为规则引擎", llmElapsed, err)
		return a.fallbackGenerate(ctx, input, "大模型调用失败: "+err.Error())
	}

	if len(resp.Choices) == 0 {
		log.Printf("[信号] ✘ 大模型返回空结果 (耗时%s) → 降级为规则引擎", llmElapsed)
		return a.fallbackGenerate(ctx, input, "大模型返回空结果")
	}

	choice := resp.Choices[0]
	completion := choice.Content

	// 提取 token 用量
	promptTokens, completionTokens, totalTokens := extractTokenUsage(choice.GenerationInfo)
	log.Printf("[信号] ✔ 大模型响应成功 (耗时%s)，响应长度=%d字符，Token: prompt=%d completion=%d total=%d",
		llmElapsed, len(completion), promptTokens, completionTokens, totalTokens)
	log.Printf("[信号] 大模型原始输出: %.500s", completion)

	parsed, err := parseLLMOutput(completion)
	if err != nil {
		log.Printf("[信号] ✘ 解析大模型输出失败: %v → 降级为规则引擎", err)
		return a.fallbackGenerate(ctx, input, "解析大模型输出失败: "+err.Error())
	}

	side := normalizeSide(parsed.Side, parsed.Signal)
	if side == domain.SideNone {
		parsed.Confidence = math.Min(parsed.Confidence, 0.55)
	}

	reason := parsed.Reason
	if reason == "" {
		reason = parsed.Justification
	}

	thinking := parsed.Thinking
	// 如果没有单独的 thinking，把完整 reason/justification 当作思维链
	if thinking == "" && len(parsed.Justification) > len(parsed.Reason) {
		thinking = parsed.Justification
	}

	log.Printf("[信号] 解析结果: signal=%q side=%q → 标准化方向=%s 置信度=%.2f thinking=%d字符",
		parsed.Signal, parsed.Side, side, parsed.Confidence, len(thinking))

	return domain.Signal{
		ID:               uuid.NewString(),
		CycleID:          input.CycleID,
		Pair:             input.Pair,
		Side:             side,
		Confidence:       clamp(parsed.Confidence, 0.0, 1.0),
		Reason:           trimReason(reason),
		Thinking:         thinking,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		ModelName:        a.modelName,
		TTLSeconds:       clampInt(parsed.TTLSeconds, 60, 1800),
		CreatedAt:        time.Now().UTC(),
	}, nil
}

func (a *LangChainAgent) buildUserPrompt(ctx context.Context, input Input) (string, error) {
	if a.userTemplate == "" {
		return "", fmt.Errorf("未加载用户提示词模板")
	}

	snap, err := a.marketClient.FetchSnapshot(ctx, input.Pair)
	if err != nil {
		return "", err
	}

	// 情绪数据日志
	s := snap.Sentiment
	log.Printf("[信号] 情绪因子: 恐惧贪婪=%d(%s) 全网多空比=%.4f 大户多空比=%.4f 大户持仓比=%.4f 主动买卖比=%.4f",
		s.FearGreedIndex, s.FearGreedLabel,
		s.LongShortRatio, s.TopLongShortRatio, s.TopPositionRatio, s.TakerBuySellRatio)

	elapsed := int(time.Since(a.startTime).Minutes())

	// 获取真实账户余额和持仓
	var cashAvailable float64 = 0
	var positions []market.PositionData
	if a.getAccountData != nil {
		cashAvailable, positions = a.getAccountData(ctx, input.Pair)
		log.Printf("[信号] 📊 真实账户数据: USDT余额=%.2f 持仓数=%d", cashAvailable, len(positions))
	} else {
		log.Printf("[信号] ⚠ 未注入账户数据回调，使用默认值")
		cashAvailable = 0
	}

	// 计算总资产价值 = USDT 余额 + 所有持仓市值
	totalValue := cashAvailable
	for i := range positions {
		var qty, price float64
		fmt.Sscanf(positions[i].Quantity, "%f", &qty)
		fmt.Sscanf(positions[i].CurrentPrice, "%f", &price)
		totalValue += qty * price
	}

	tradingMode := a.tradingMode
	if tradingMode == "" {
		tradingMode = "spot"
	}
	leverage := a.leverage
	if leverage < 1 {
		leverage = 1
	}

	account := market.AccountInfo{
		AccountValue:   totalValue,
		CashAvailable:  cashAvailable,
		ReturnPct:      0,
		SharpeRatio:    0,
		MinutesElapsed: elapsed,
		TradingMode:    tradingMode,
		Leverage:       leverage,
		Positions:      positions,
	}

	// 获取关联币对数据（BTC 作为市场风向标）
	var extraSnaps []market.CoinSnapshot
	mainCoin := strings.Split(strings.ToUpper(input.Pair), "/")[0]
	if mainCoin != "BTC" {
		btcSnap, btcErr := a.marketClient.FetchLightSnapshot(ctx, "BTC/USDT")
		if btcErr == nil {
			extraSnaps = append(extraSnaps, btcSnap)
			log.Printf("[信号] 📊 BTC参考: 价格=%.2f 24h涨跌=%.2f%% 资金费率=%.6f",
				btcSnap.Price, btcSnap.Change24hPct, btcSnap.FundingRate)
		} else {
			log.Printf("[信号] ⚠ BTC参考数据获取失败: %v（不影响主信号）", btcErr)
		}
	}

	return market.BuildPrompt(a.userTemplate, snap, account, extraSnaps)
}

// adaptSystemPrompt 根据交易模式动态修改系统提示词
func (a *LangChainAgent) adaptSystemPrompt() string {
	if a.tradingMode != "futures" {
		return a.systemPrompt // 现货模式：原样返回
	}

	// 合约模式：替换关键段落
	prompt := a.systemPrompt

	// 替换合规声明
	prompt = strings.Replace(prompt,
		"The system only performs spot trading (buying and selling digital assets) on regulated exchanges.",
		fmt.Sprintf("The system performs USDT-M perpetual futures trading with %dx leverage (long only) on regulated exchanges.", a.leverage),
		1)

	// 替换角色描述
	prompt = strings.Replace(prompt,
		"on Binance spot market",
		fmt.Sprintf("on Binance USDT-M Futures market (%dx leverage, long only)", a.leverage),
		1)

	// 替换交易模式
	prompt = strings.Replace(prompt,
		"- **Trading Mode**: Spot only (NO leverage, NO margin, NO futures)",
		fmt.Sprintf("- **Trading Mode**: USDT-M Perpetual Futures (%dx leverage, long only)", a.leverage),
		1)
	prompt = strings.Replace(prompt,
		"- **Exchange**: Binance (spot market)",
		"- **Exchange**: Binance (USDT-M Futures)",
		1)

	// 替换交易机制说明
	prompt = strings.Replace(prompt,
		"## Trading Mechanics\n\n- **Spot Trading**: You buy coins with USDT and sell coins back to USDT\n- **No Leverage**: All positions are 1x (you can only spend what you have)\n- **No Short Selling**: You can only profit when prices go UP\n- **Trading Fees**: ~0.1% per trade (maker/taker)\n- **Slippage**: Expect 0.01-0.1% on market orders depending on size",
		fmt.Sprintf(`## Trading Mechanics

- **Futures Trading**: You open LONG positions with margin and close them to take profit/cut loss
- **Leverage**: %dx fixed leverage (margin = position_value / %d)
- **Long Only**: You can only open LONG positions (profit when price goes UP)
- **No Short Selling**: Short positions are disabled in this configuration
- **Funding Rate**: Paid/received every 8 hours — factor this into holding decisions
- **Liquidation Risk**: With %dx leverage, liquidation occurs at ~%.0f%% price drop from entry
- **Trading Fees**: ~0.04%% per trade (maker/taker, lower than spot)
- **Slippage**: Expect 0.01-0.05%% on market orders`, a.leverage, a.leverage, a.leverage, 100.0/float64(a.leverage)*0.8),
		1)

	// 移除 "不能做空" 的强制提示
	prompt = strings.Replace(prompt,
		"**IMPORTANT: You CANNOT short sell in spot trading. If you see bearish signals and have NO position, use \"hold\". If you HAVE a position and see bearish signals, use \"close\" to take profit or cut losses.**",
		"**IMPORTANT: You can only go LONG (no short selling). If bearish, use \"hold\" (no position) or \"close\" (has position). Consider funding rate costs for extended holds.**",
		1)

	// 替换仓位框架中的无杠杆说明
	prompt = strings.Replace(prompt,
		"5. **NO leverage**: Maximum risk is 100% of position value (coin goes to zero)",
		fmt.Sprintf("5. **%dx Leverage**: Maximum risk is the margin amount (liquidation before 100%% loss). With %dx leverage, a %.1f%% adverse move will liquidate your position.", a.leverage, a.leverage, 100.0/float64(a.leverage)*0.8),
		1)

	// 替换策略指南标题
	prompt = strings.Replace(prompt,
		"# SPOT TRADING STRATEGY GUIDELINES",
		"# FUTURES TRADING STRATEGY GUIDELINES (LONG ONLY)",
		1)

	// 替换常见陷阱中的 short 提醒
	prompt = strings.Replace(prompt,
		"- ⚠️ **Outputting \"short\"**: You CANNOT short in spot. Use \"hold\" or \"close\" instead.",
		"- ⚠️ **Outputting \"short\"**: Short positions are disabled. Use \"hold\" or \"close\" instead.\n- ⚠️ **Ignoring funding rate**: High positive funding = holding cost; consider closing if funding > 0.1%\n- ⚠️ **Ignoring liquidation risk**: Always check how far price is from your liquidation price",
		1)

	// 替换最终指示中的 short 提醒
	prompt = strings.Replace(prompt,
		"5. **NEVER output \"short\" as signal — spot trading supports \"long\", \"close\", \"hold\", or \"none\"**",
		fmt.Sprintf("5. **NEVER output \"short\"** — only \"long\", \"close\", \"hold\", or \"none\" (long-only mode, %dx leverage)", a.leverage),
		1)

	return prompt
}

func (a *LangChainAgent) buildSimplePrompt(input Input) string {
	return fmt.Sprintf(`请分析并给出交易决策（交易对=%s）。
last_price=%.8f change_24h=%.4f volume_24h=%.4f funding_rate=%.6f

请严格输出 JSON，reason/justification 必须为中文。`,
		input.Pair, input.Snapshot.LastPrice, input.Snapshot.Change24h,
		input.Snapshot.Volume24h, input.Snapshot.FundingRate)
}

func (a *LangChainAgent) fallbackGenerate(_ context.Context, input Input, reason string) (domain.Signal, error) {
	log.Printf("[信号] 降级为 hold（大模型不可用，不做交易决策）: %s", reason)
	return domain.Signal{
		ID:         uuid.NewString(),
		CycleID:    input.CycleID,
		Pair:       input.Pair,
		Side:       domain.SideNone,
		Confidence: 0,
		Reason:     "大模型不可用，自动跳过本轮: " + trimReason(reason),
		ModelName:  "fallback",
		TTLSeconds: 60,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func parseLLMOutput(raw string) (llmResponse, error) {
	var out llmResponse
	clean := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return out, nil
	}

	re := regexp.MustCompile(`(?s)\{.*\}`)
	match := re.FindString(clean)
	if match == "" {
		return out, fmt.Errorf("大模型响应中未找到JSON对象")
	}
	if err := json.Unmarshal([]byte(match), &out); err != nil {
		return out, fmt.Errorf("解析大模型JSON输出失败: %w", err)
	}

	return out, nil
}

func normalizeSide(side, signal string) domain.Side {
	// 检查 side 字段
	s := strings.ToLower(strings.TrimSpace(side))
	if s == string(domain.SideLong) || s == "buy" || s == "buy_to_enter" {
		return domain.SideLong
	}
	if s == string(domain.SideClose) || s == "sell" || s == "sell_to_exit" {
		return domain.SideClose
	}

	// 检查 signal 字段
	sig := strings.ToLower(strings.TrimSpace(signal))
	if sig == string(domain.SideLong) || sig == "buy" || sig == "buy_to_enter" {
		return domain.SideLong
	}
	if sig == string(domain.SideClose) || sig == "sell" || sig == "sell_to_exit" {
		return domain.SideClose
	}

	// hold / none / 其他 → 不交易
	return domain.SideNone
}

func trimReason(reason string) string {
	clean := strings.TrimSpace(reason)
	if clean == "" {
		return "模型未给出理由"
	}
	if len(clean) <= 500 {
		return clean
	}
	return clean[:500]
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// extractTokenUsage 从 LangChainGo GenerationInfo 中提取 token 用量
func extractTokenUsage(info map[string]any) (prompt, completion, total int) {
	if info == nil {
		return 0, 0, 0
	}
	prompt = toInt(info["PromptTokens"])
	completion = toInt(info["CompletionTokens"])
	total = toInt(info["TotalTokens"])
	if total == 0 && (prompt > 0 || completion > 0) {
		total = prompt + completion
	}
	return
}

func toInt(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
