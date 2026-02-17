package risk

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"ai_quant/internal/config"
	"ai_quant/internal/domain"

	"github.com/google/uuid"
)

type Input struct {
	CycleID   string
	Signal    domain.Signal
	Portfolio domain.PortfolioState
}

type Agent interface {
	Evaluate(ctx context.Context, input Input) (domain.RiskDecision, error)
}

type RuleAgent struct {
	maxSingleStakeUSDT float64 // 单笔最大下单金额上限
	maxDailyLossUSDT   float64
	maxExposureUSDT    float64
	minConfidence      float64
	tradingMode        string // "spot" 或 "futures"
	leverage           int    // 杠杆倍数
}

func New(cfg config.Config) Agent {
	leverage := 1
	if cfg.TradingMode == "futures" {
		leverage = cfg.FuturesLeverage
		if leverage < 1 {
			leverage = 3
		}
	}
	return &RuleAgent{
		maxSingleStakeUSDT: cfg.MaxSingleStakeUSDT,
		maxDailyLossUSDT:   cfg.MaxDailyLossUSDT,
		maxExposureUSDT:    cfg.MaxExposureUSDT,
		minConfidence:      cfg.MinConfidence,
		tradingMode:        cfg.TradingMode,
		leverage:           leverage,
	}
}

func (a *RuleAgent) Evaluate(_ context.Context, input Input) (domain.RiskDecision, error) {
	now := time.Now().UTC()
	decision := domain.RiskDecision{
		ID:           uuid.NewString(),
		CycleID:      input.CycleID,
		SignalID:     input.Signal.ID,
		Approved:     false,
		RejectReason: "",
		MaxStakeUSDT: 0,
		CreatedAt:    now,
	}

	if input.Signal.Side == domain.SideNone {
		decision.RejectReason = "signal side is none"
		return decision, nil
	}

	// close（卖出）信号：只检查置信度，不检查敞口限制
	if input.Signal.Side == domain.SideClose {
		if input.Signal.Confidence < a.minConfidence {
			decision.RejectReason = fmt.Sprintf("close signal confidence %.2f below min %.2f", input.Signal.Confidence, a.minConfidence)
			return decision, nil
		}
		decision.Approved = true
		decision.MaxStakeUSDT = 0 // close 不需要 stake，卖出全部持仓
		return decision, nil
	}

	// long（买入）信号：检查置信度 + 敞口 + 每日亏损
	if input.Signal.Confidence < a.minConfidence {
		decision.RejectReason = fmt.Sprintf("signal confidence %.2f below min %.2f", input.Signal.Confidence, a.minConfidence)
		return decision, nil
	}
	if input.Portfolio.DailyPnLUSDT <= -math.Abs(a.maxDailyLossUSDT) {
		decision.RejectReason = fmt.Sprintf("daily pnl %.2f below max loss limit -%.2f", input.Portfolio.DailyPnLUSDT, math.Abs(a.maxDailyLossUSDT))
		return decision, nil
	}

	remainingExposure := a.maxExposureUSDT - input.Portfolio.OpenExposureUSDT
	if remainingExposure <= 0 {
		decision.RejectReason = "max exposure limit reached"
		return decision, nil
	}

	decision.MaxStakeUSDT = math.Min(a.maxSingleStakeUSDT, remainingExposure)
	if decision.MaxStakeUSDT <= 0 {
		decision.RejectReason = "computed max stake is zero"
		return decision, nil
	}

	// 合约模式：显示杠杆放大后的实际仓位
	if a.tradingMode == "futures" && a.leverage > 1 {
		actualPosition := decision.MaxStakeUSDT * float64(a.leverage)
		log.Printf("[风控] 合约模式: 保证金=%.2f USDT x%d倍杠杆 = 实际仓位 %.2f USDT",
			decision.MaxStakeUSDT, a.leverage, actualPosition)
	}

	decision.Approved = true
	return decision, nil
}
