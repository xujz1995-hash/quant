package domain

import "time"

type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
	SideClose Side = "close"
	SideNone  Side = "none"
)

type CycleStatus string

const (
	CycleStatusRunning  CycleStatus = "running"
	CycleStatusRejected CycleStatus = "rejected"
	CycleStatusSuccess  CycleStatus = "success"
	CycleStatusFailed   CycleStatus = "failed"
)

type Cycle struct {
	ID           string      `json:"id"`
	Pair         string      `json:"pair"`
	Status       CycleStatus `json:"status"`
	ErrorMessage string      `json:"error_message,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type MarketSnapshot struct {
	Pair        string    `json:"pair"`
	LastPrice   float64   `json:"last_price"`
	Change24h   float64   `json:"change_24h"`
	Volume24h   float64   `json:"volume_24h"`
	FundingRate float64   `json:"funding_rate"`
	Timestamp   time.Time `json:"timestamp"`
}

type Signal struct {
	ID               string    `json:"id"`
	CycleID          string    `json:"cycle_id"`
	Pair             string    `json:"pair"`
	Side             Side      `json:"side"`
	Confidence       float64   `json:"confidence"`
	Reason           string    `json:"reason"`
	Thinking         string    `json:"thinking,omitempty"`          // AI 思维链
	PromptTokens     int       `json:"prompt_tokens,omitempty"`     // 提示词 token 数
	CompletionTokens int       `json:"completion_tokens,omitempty"` // 回复 token 数
	TotalTokens      int       `json:"total_tokens,omitempty"`      // 总 token 数
	ModelName        string    `json:"model_name,omitempty"`        // 使用的模型名称
	TTLSeconds       int       `json:"ttl_seconds"`
	CreatedAt        time.Time `json:"created_at"`
}

type PortfolioState struct {
	DailyPnLUSDT     float64 `json:"daily_pnl_usdt"`
	OpenExposureUSDT float64 `json:"open_exposure_usdt"`
}

type RiskDecision struct {
	ID           string    `json:"id"`
	CycleID      string    `json:"cycle_id"`
	SignalID     string    `json:"signal_id"`
	Approved     bool      `json:"approved"`
	RejectReason string    `json:"reject_reason,omitempty"`
	MaxStakeUSDT float64   `json:"max_stake_usdt"`
	CreatedAt    time.Time `json:"created_at"`
}

type Order struct {
	ID              string    `json:"id"`
	CycleID         string    `json:"cycle_id"`
	SignalID        string    `json:"signal_id"`
	ClientOrderID   string    `json:"client_order_id"`
	Pair            string    `json:"pair"`
	Side            Side      `json:"side"`
	StakeUSDT       float64   `json:"stake_usdt"`
	Leverage        int       `json:"leverage,omitempty"` // 杠杆倍数，现货=0，合约=2-20
	Status          string    `json:"status"`
	ExchangeOrderID string    `json:"exchange_order_id,omitempty"`
	FilledPrice     float64   `json:"filled_price,omitempty"`
	FilledQuantity  float64   `json:"filled_qty,omitempty"`
	RawResponse     string    `json:"raw_response,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type CycleLog struct {
	ID        int64     `json:"id"`
	CycleID   string    `json:"cycle_id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type CycleReport struct {
	Cycle            Cycle             `json:"cycle"`
	Signal           *Signal           `json:"signal,omitempty"`
	Risk             *RiskDecision     `json:"risk,omitempty"`
	PositionStrategy *PositionStrategy `json:"position_strategy,omitempty"`
	Order            *Order            `json:"order,omitempty"`
	Logs             []CycleLog        `json:"logs,omitempty"`
}

type CycleResult struct {
	Cycle  Cycle        `json:"cycle"`
	Signal Signal       `json:"signal"`
	Risk   RiskDecision `json:"risk"`
	Order  *Order       `json:"order,omitempty"`
	Logs   []CycleLog   `json:"logs,omitempty"`
}

// CycleSummary 周期列表摘要视图（用于分页列表展示）
type CycleSummary struct {
	CycleID      string      `json:"cycle_id"`
	Pair         string      `json:"pair"`
	Status       CycleStatus `json:"status"`
	SignalSide   Side        `json:"signal_side"`
	Confidence   float64     `json:"confidence"`
	SignalReason string      `json:"signal_reason,omitempty"`
	TotalTokens  int         `json:"total_tokens,omitempty"`
	ModelName    string      `json:"model_name,omitempty"`
	RiskApproved *bool       `json:"risk_approved,omitempty"`
	RejectReason string      `json:"reject_reason,omitempty"`
	StakeUSDT    float64     `json:"stake_usdt,omitempty"`
	FilledPrice  float64     `json:"filled_price,omitempty"`
	OrderStatus  string      `json:"order_status,omitempty"`
	ErrorMessage string      `json:"error_message,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

// Holding 当前持仓快照（按币对聚合）
type Holding struct {
	ID        int64     `json:"id"`
	Pair      string    `json:"pair"`       // 如 DOGE/USDT
	Symbol    string    `json:"symbol"`     // 如 DOGE
	Quantity  float64   `json:"quantity"`   // 当前持有数量
	AvgPrice  float64   `json:"avg_price"`  // 平均买入价格
	TotalCost float64   `json:"total_cost"` // 总成本 (USDT)
	Source    string    `json:"source"`     // "local"=订单聚合, "exchange"=交易所同步
	UpdatedAt time.Time `json:"updated_at"`
}

// HoldingView 持仓展示视图（附实时行情数据）
type HoldingView struct {
	Holding
	CurrentPrice  float64 `json:"current_price"`  // 当前市价
	MarketValue   float64 `json:"market_value"`   // 市值 = 数量 × 当前价
	UnrealizedPnL float64 `json:"unrealized_pnl"` // 未实现盈亏 = 市值 - 成本
	PnLPercent    float64 `json:"pnl_percent"`    // 盈亏百分比
}

// PositionView 是订单的聚合视图，用于展示当前仓位。
type PositionView struct {
	OrderID         string    `json:"order_id"`
	CycleID         string    `json:"cycle_id"`
	Pair            string    `json:"pair"`
	Side            Side      `json:"side"`
	StakeUSDT       float64   `json:"stake_usdt"`
	FilledPrice     float64   `json:"filled_price"`
	FilledQuantity  float64   `json:"filled_qty"`
	Status          string    `json:"status"`
	ExchangeOrderID string    `json:"exchange_order_id,omitempty"`
	SignalReason    string    `json:"signal_reason,omitempty"`
	Confidence      float64   `json:"confidence"`
	CycleStatus     string    `json:"cycle_status"`
	CreatedAt       time.Time `json:"created_at"`
}
