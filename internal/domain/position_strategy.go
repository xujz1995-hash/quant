package domain

import "time"

// PositionStrategy 建仓策略
type PositionStrategy struct {
	ID        string    `json:"id"`
	CycleID   string    `json:"cycle_id"`
	SignalID  string    `json:"signal_id"`
	Pair      string    `json:"pair"`
	Side      Side      `json:"side"`
	
	// 策略参数
	Strategy      string  `json:"strategy"`       // 策略类型: "full", "pyramid", "grid", "dca"
	TotalAmount   float64 `json:"total_amount"`   // 总投入金额 (USDT)
	EntryLevels   int     `json:"entry_levels"`   // 分批次数
	
	// 分批建仓计划
	Batches []PositionBatch `json:"batches"`
	
	// 止盈止损
	TakeProfitPercent float64 `json:"take_profit_percent"` // 止盈百分比
	StopLossPercent   float64 `json:"stop_loss_percent"`   // 止损百分比
	
	// 元数据
	Reason    string    `json:"reason"`     // 策略选择理由
	CreatedAt time.Time `json:"created_at"`
}

// PositionBatch 单次建仓批次
type PositionBatch struct {
	BatchNo       int     `json:"batch_no"`        // 批次编号 (1, 2, 3...)
	TriggerPrice  float64 `json:"trigger_price"`   // 触发价格
	Amount        float64 `json:"amount"`          // 本批次金额 (USDT)
	Percentage    float64 `json:"percentage"`      // 占总金额百分比
	Status        string  `json:"status"`          // "pending", "executed", "cancelled"
	ExecutedPrice float64 `json:"executed_price"`  // 实际成交价
	ExecutedQty   float64 `json:"executed_qty"`    // 实际成交量
	ExecutedAt    *time.Time `json:"executed_at"` // 执行时间
}

// StrategyType 建仓策略类型
const (
	StrategyFull    = "full"    // 全仓：一次性建仓
	StrategyPyramid = "pyramid" // 金字塔：价格下跌时加仓
	StrategyGrid    = "grid"    // 网格：固定间隔分批
	StrategyDCA     = "dca"     // 定投：时间分批
)
