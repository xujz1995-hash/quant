package position

import (
	"context"
	"fmt"
	"log"
	"time"

	"ai_quant/internal/domain"
)

// Input 建仓策略输入
type Input struct {
	CycleID      string
	SignalID     string
	Pair         string
	Side         domain.Side
	Signal       domain.Signal
	MaxStakeUSDT float64
	CurrentPrice float64
	Volatility   float64 // 波动率（可选）
}

// Agent 建仓策略生成器
type Agent interface {
	Generate(ctx context.Context, input Input) (domain.PositionStrategy, error)
}

type agent struct {
	minBatchAmount float64 // 最小单批金额
}

// New 创建建仓策略 Agent
func New() Agent {
	return &agent{
		minBatchAmount: 10.0, // 最小单批 10 USDT
	}
}

// Generate 生成建仓策略
func (a *agent) Generate(ctx context.Context, input Input) (domain.PositionStrategy, error) {
	if input.Side == domain.SideClose {
		// 平仓不需要建仓策略，直接全部卖出
		return domain.PositionStrategy{
			ID:            generateID(),
			CycleID:       input.CycleID,
			SignalID:      input.SignalID,
			Pair:          input.Pair,
			Side:          input.Side,
			Strategy:      domain.StrategyFull,
			TotalAmount:   0,
			EntryLevels:   1,
			Batches:       []domain.PositionBatch{},
			Reason:        "平仓操作，无需建仓策略",
			CreatedAt:     time.Now().UTC(),
		}, nil
	}

	// 根据信号置信度选择策略
	strategy := a.selectStrategy(input.Signal.Confidence, input.MaxStakeUSDT)
	
	var batches []domain.PositionBatch
	var reason string
	var takeProfitPercent, stopLossPercent float64

	switch strategy {
	case domain.StrategyFull:
		// 全仓：高置信度，一次性建仓
		batches = a.generateFullStrategy(input.MaxStakeUSDT, input.CurrentPrice)
		reason = fmt.Sprintf("高置信度(%.2f)，采用全仓策略一次性建仓", input.Signal.Confidence)
		takeProfitPercent = 5.0  // 5% 止盈
		stopLossPercent = 2.0    // 2% 止损

	case domain.StrategyPyramid:
		// 金字塔：中等置信度，分批建仓，价格下跌时加仓
		batches = a.generatePyramidStrategy(input.MaxStakeUSDT, input.CurrentPrice)
		reason = fmt.Sprintf("中等置信度(%.2f)，采用金字塔策略分批建仓，降低风险", input.Signal.Confidence)
		takeProfitPercent = 8.0  // 8% 止盈
		stopLossPercent = 3.0    // 3% 止损

	case domain.StrategyGrid:
		// 网格：低置信度或震荡行情，网格分批
		batches = a.generateGridStrategy(input.MaxStakeUSDT, input.CurrentPrice)
		reason = fmt.Sprintf("置信度(%.2f)较低或震荡行情，采用网格策略分散风险", input.Signal.Confidence)
		takeProfitPercent = 10.0 // 10% 止盈
		stopLossPercent = 4.0    // 4% 止损

	default:
		return domain.PositionStrategy{}, fmt.Errorf("未知策略类型: %s", strategy)
	}

	log.Printf("[建仓策略] %s 策略=%s 总金额=%.2f 分批=%d 止盈=%.1f%% 止损=%.1f%%",
		input.Pair, strategy, input.MaxStakeUSDT, len(batches), takeProfitPercent, stopLossPercent)

	return domain.PositionStrategy{
		ID:                generateID(),
		CycleID:           input.CycleID,
		SignalID:          input.SignalID,
		Pair:              input.Pair,
		Side:              input.Side,
		Strategy:          strategy,
		TotalAmount:       input.MaxStakeUSDT,
		EntryLevels:       len(batches),
		Batches:           batches,
		TakeProfitPercent: takeProfitPercent,
		StopLossPercent:   stopLossPercent,
		Reason:            reason,
		CreatedAt:         time.Now().UTC(),
	}, nil
}

// selectStrategy 根据置信度和金额选择策略
func (a *agent) selectStrategy(confidence, amount float64) string {
	if confidence >= 0.75 {
		// 高置信度：全仓
		return domain.StrategyFull
	} else if confidence >= 0.60 {
		// 中等置信度：金字塔
		return domain.StrategyPyramid
	} else {
		// 低置信度：网格
		return domain.StrategyGrid
	}
}

// generateFullStrategy 全仓策略：一次性建仓
func (a *agent) generateFullStrategy(totalAmount, currentPrice float64) []domain.PositionBatch {
	return []domain.PositionBatch{
		{
			BatchNo:      1,
			TriggerPrice: currentPrice,
			Amount:       totalAmount,
			Percentage:   100.0,
			Status:       "pending",
		},
	}
}

// generatePyramidStrategy 金字塔策略：首次50%，后续逐步加仓
func (a *agent) generatePyramidStrategy(totalAmount, currentPrice float64) []domain.PositionBatch {
	// 分3批：50% + 30% + 20%
	batches := []domain.PositionBatch{
		{
			BatchNo:      1,
			TriggerPrice: currentPrice,
			Amount:       totalAmount * 0.50,
			Percentage:   50.0,
			Status:       "pending",
		},
		{
			BatchNo:      2,
			TriggerPrice: currentPrice * 0.98, // 下跌 2% 时加仓
			Amount:       totalAmount * 0.30,
			Percentage:   30.0,
			Status:       "pending",
		},
		{
			BatchNo:      3,
			TriggerPrice: currentPrice * 0.96, // 下跌 4% 时加仓
			Amount:       totalAmount * 0.20,
			Percentage:   20.0,
			Status:       "pending",
		},
	}
	return batches
}

// generateGridStrategy 网格策略：均匀分批
func (a *agent) generateGridStrategy(totalAmount, currentPrice float64) []domain.PositionBatch {
	// 分5批，每批20%，价格间隔1%
	numBatches := 5
	amountPerBatch := totalAmount / float64(numBatches)
	
	batches := make([]domain.PositionBatch, numBatches)
	for i := 0; i < numBatches; i++ {
		priceOffset := 1.0 - (float64(i) * 0.01) // 0%, -1%, -2%, -3%, -4%
		batches[i] = domain.PositionBatch{
			BatchNo:      i + 1,
			TriggerPrice: currentPrice * priceOffset,
			Amount:       amountPerBatch,
			Percentage:   100.0 / float64(numBatches),
			Status:       "pending",
		}
	}
	return batches
}

// generateID 生成唯一ID
func generateID() string {
	return fmt.Sprintf("ps_%d", time.Now().UnixNano())
}
