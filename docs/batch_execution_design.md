# 后续批次自动触发实现方案

## 📋 需求分析

当前状态：
- ✅ 建仓策略已生成（包含多批次计划）
- ✅ 第1批已执行
- ❌ 第2、3批需要价格触发时自动执行

目标：
- 监控市场价格
- 当价格达到触发条件时，自动执行对应批次
- 更新批次状态
- 记录执行日志

---

## 🎯 设计方案

### 方案一：定时轮询（推荐）

**原理**：
- 启动一个后台 goroutine
- 每隔 N 秒检查一次所有待执行批次
- 对比当前价格与触发价格
- 满足条件则执行

**优点**：
- ✅ 实现简单
- ✅ 可控性强
- ✅ 易于调试
- ✅ 资源占用低

**缺点**：
- ❌ 有延迟（最多 N 秒）
- ❌ 可能错过瞬间价格

**适用场景**：
- 现货交易（价格变化相对平缓）
- 分钟级触发精度要求

---

### 方案二：WebSocket 实时监控

**原理**：
- 连接 Binance WebSocket
- 订阅价格流
- 实时接收价格更新
- 立即判断并执行

**优点**：
- ✅ 实时性强（毫秒级）
- ✅ 不会错过价格
- ✅ 资源效率高（推送模式）

**缺点**：
- ❌ 实现复杂
- ❌ 需要处理连接断开重连
- ❌ 需要管理订阅列表

**适用场景**：
- 合约交易（价格波动大）
- 秒级触发精度要求

---

### 方案三：混合方案

**原理**：
- 定时轮询作为主要机制
- WebSocket 作为辅助（可选）
- 优先使用 WebSocket，降级到轮询

**优点**：
- ✅ 兼顾实时性和稳定性
- ✅ 容错能力强

**缺点**：
- ❌ 实现最复杂

---

## 🚀 推荐实现：方案一（定时轮询）

### 架构设计

```
┌─────────────────────────────────────────────────┐
│          BatchExecutor Service                  │
│  (后台 goroutine，每30秒运行一次)               │
└────────────────┬────────────────────────────────┘
                 │
                 ▼
    ┌────────────────────────┐
    │ 1. 查询所有待执行批次   │
    │    (status = pending)  │
    └────────┬───────────────┘
             │
             ▼
    ┌────────────────────────┐
    │ 2. 获取当前市场价格     │
    │    (Binance API)       │
    └────────┬───────────────┘
             │
             ▼
    ┌────────────────────────┐
    │ 3. 判断触发条件         │
    │    price <= trigger?   │
    └────────┬───────────────┘
             │
        ┌────┴────┐
        │ 满足？   │
        └────┬────┘
             │
    ┌────────┴────────┐
    │ 是              │ 否
    ▼                 ▼
┌────────┐      ┌──────────┐
│ 执行   │      │ 继续等待  │
│ 订单   │      └──────────┘
└───┬────┘
    │
    ▼
┌────────────────┐
│ 更新批次状态   │
│ executed       │
└────────────────┘
```

---

## 💻 实现代码

### 1. 数据库查询方法

```go
// internal/store/position_strategy.go

// ListPendingBatches 获取所有待执行的批次
func (r *SQLiteRepository) ListPendingBatches(ctx context.Context) ([]PendingBatch, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT 
			ps.id as strategy_id,
			ps.cycle_id,
			ps.pair,
			ps.batches
		FROM position_strategies ps
		WHERE EXISTS (
			SELECT 1 FROM json_each(ps.batches)
			WHERE json_extract(value, '$.status') = 'pending'
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var batches []PendingBatch
	for rows.Next() {
		var pb PendingBatch
		var batchesJSON string
		if err := rows.Scan(&pb.StrategyID, &pb.CycleID, &pb.Pair, &batchesJSON); err != nil {
			return nil, err
		}
		
		// 解析批次数据
		var allBatches []domain.PositionBatch
		if err := json.Unmarshal([]byte(batchesJSON), &allBatches); err != nil {
			return nil, err
		}
		
		// 只保留 pending 状态的批次
		for _, b := range allBatches {
			if b.Status == "pending" {
				pb.Batches = append(pb.Batches, b)
			}
		}
		
		if len(pb.Batches) > 0 {
			batches = append(batches, pb)
		}
	}
	return batches, nil
}

// UpdateBatchStatus 更新批次状态
func (r *SQLiteRepository) UpdateBatchStatus(ctx context.Context, strategyID string, batchNo int, status string, executedPrice, executedQty float64, executedAt time.Time) error {
	// 1. 读取当前策略
	strategy, err := r.GetPositionStrategy(ctx, strategyID)
	if err != nil {
		return err
	}
	
	// 2. 更新对应批次
	for i := range strategy.Batches {
		if strategy.Batches[i].BatchNo == batchNo {
			strategy.Batches[i].Status = status
			strategy.Batches[i].ExecutedPrice = executedPrice
			strategy.Batches[i].ExecutedQty = executedQty
			strategy.Batches[i].ExecutedAt = &executedAt
			break
		}
	}
	
	// 3. 序列化并更新
	batchesJSON, err := json.Marshal(strategy.Batches)
	if err != nil {
		return err
	}
	
	_, err = r.db.ExecContext(ctx, `
		UPDATE position_strategies
		SET batches = ?
		WHERE id = ?
	`, string(batchesJSON), strategyID)
	
	return err
}

type PendingBatch struct {
	StrategyID string
	CycleID    string
	Pair       string
	Batches    []domain.PositionBatch
}
```

### 2. 批次执行服务

```go
// internal/executor/batch_executor.go

package executor

import (
	"context"
	"log"
	"strings"
	"time"

	"ai_quant/internal/agent/execution"
	"ai_quant/internal/domain"
	"ai_quant/internal/store"
)

type BatchExecutor struct {
	repo     store.Repository
	executor execution.Executor
	interval time.Duration
	stopCh   chan struct{}
}

func NewBatchExecutor(repo store.Repository, executor execution.Executor, intervalSec int) *BatchExecutor {
	return &BatchExecutor{
		repo:     repo,
		executor: executor,
		interval: time.Duration(intervalSec) * time.Second,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动批次执行器
func (be *BatchExecutor) Start(ctx context.Context) {
	ticker := time.NewTicker(be.interval)
	defer ticker.Stop()

	log.Printf("[批次执行器] 已启动 检查间隔=%s", be.interval)

	for {
		select {
		case <-ticker.C:
			be.checkAndExecute(ctx)
		case <-be.stopCh:
			log.Println("[批次执行器] 已停止")
			return
		case <-ctx.Done():
			log.Println("[批次执行器] 上下文取消")
			return
		}
	}
}

// Stop 停止批次执行器
func (be *BatchExecutor) Stop() {
	close(be.stopCh)
}

// checkAndExecute 检查并执行待触发批次
func (be *BatchExecutor) checkAndExecute(ctx context.Context) {
	// 1. 获取所有待执行批次
	pendingBatches, err := be.repo.ListPendingBatches(ctx)
	if err != nil {
		log.Printf("[批次执行器] ⚠ 查询待执行批次失败: %v", err)
		return
	}

	if len(pendingBatches) == 0 {
		return
	}

	log.Printf("[批次执行器] 检查 %d 个待执行批次", len(pendingBatches))

	// 2. 遍历每个批次
	for _, pb := range pendingBatches {
		// 3. 获取当前价格
		currentPrice, err := be.getCurrentPrice(ctx, pb.Pair)
		if err != nil {
			log.Printf("[批次执行器] ⚠ 获取 %s 价格失败: %v", pb.Pair, err)
			continue
		}

		// 4. 检查每个批次是否满足触发条件
		for _, batch := range pb.Batches {
			if be.shouldExecute(batch, currentPrice) {
				log.Printf("[批次执行器] 🎯 触发批次 %s 第%d批 触发价=%.6f 当前价=%.6f",
					pb.Pair, batch.BatchNo, batch.TriggerPrice, currentPrice)
				
				// 5. 执行订单
				if err := be.executeBatch(ctx, pb, batch, currentPrice); err != nil {
					log.Printf("[批次执行器] ✘ 执行失败: %v", err)
				} else {
					log.Printf("[批次执行器] ✔ 批次 %d 执行成功", batch.BatchNo)
				}
			}
		}
	}
}

// shouldExecute 判断是否应该执行批次
func (be *BatchExecutor) shouldExecute(batch domain.PositionBatch, currentPrice float64) bool {
	// 买入：当前价格 <= 触发价格（价格下跌到目标位）
	return currentPrice <= batch.TriggerPrice
}

// getCurrentPrice 获取当前市场价格
func (be *BatchExecutor) getCurrentPrice(ctx context.Context, pair string) (float64, error) {
	// 使用 Binance API 获取实时价格
	symbol := strings.Replace(pair, "/", "", 1)
	
	// 简化版：直接调用 ticker API
	// 实际应该复用 orchestrator 中的 fetchTickerPrice 方法
	url := "https://api.binance.com/api/v3/ticker/price?symbol=" + symbol
	
	var result struct {
		Price string `json:"price"`
	}
	
	// HTTP 请求获取价格（省略具体实现）
	// ...
	
	return parseFloat(result.Price), nil
}

// executeBatch 执行批次订单
func (be *BatchExecutor) executeBatch(ctx context.Context, pb store.PendingBatch, batch domain.PositionBatch, currentPrice float64) error {
	// 1. 构造执行输入
	execInput := execution.Input{
		CycleID:       pb.CycleID,
		SignalID:      "", // 批次执行没有新的 signal
		Pair:          pb.Pair,
		Side:          domain.SideLong,
		StakeUSDT:     batch.Amount,
		EstimatedFill: currentPrice,
	}

	// 2. 执行订单
	order, err := be.executor.Execute(ctx, execInput)
	if err != nil {
		return err
	}

	// 3. 更新批次状态
	now := time.Now().UTC()
	err = be.repo.UpdateBatchStatus(
		ctx,
		pb.StrategyID,
		batch.BatchNo,
		"executed",
		order.FilledPrice,
		order.FilledQuantity,
		now,
	)
	if err != nil {
		log.Printf("[批次执行器] ⚠ 更新批次状态失败: %v", err)
	}

	// 4. 记录日志
	log.Printf("[批次执行器] 批次 %d 已执行 价格=%.6f 数量=%.4f 金额=%.2f",
		batch.BatchNo, order.FilledPrice, order.FilledQuantity, batch.Amount)

	return nil
}
```

### 3. 集成到主程序

```go
// main.go

import (
	"ai_quant/internal/executor"
)

func main() {
	// ... 现有初始化代码 ...

	// 初始化批次执行器
	batchExecutor := executor.NewBatchExecutor(repo, execAgent, 30) // 每30秒检查一次
	
	// 启动批次执行器（后台运行）
	go batchExecutor.Start(context.Background())
	log.Println("📦 批次执行器已启动")

	// ... HTTP 服务器启动 ...
}
```

---

## 🎛️ 配置选项

在 `.env` 中添加：

```bash
# 批次执行器配置
BATCH_EXECUTOR_ENABLED=true      # 是否启用
BATCH_EXECUTOR_INTERVAL_SEC=30   # 检查间隔（秒）
```

在 `config.go` 中添加：

```go
type Config struct {
	// ... 现有字段 ...
	
	// 批次执行器
	BatchExecutorEnabled    bool
	BatchExecutorIntervalSec int
}

func Load() Config {
	// ... 现有代码 ...
	
	cfg.BatchExecutorEnabled = getEnvBool("BATCH_EXECUTOR_ENABLED", true)
	cfg.BatchExecutorIntervalSec = getEnvInt("BATCH_EXECUTOR_INTERVAL_SEC", 30)
	
	return cfg
}
```

---

## 📊 监控和日志

### 日志示例

```
[批次执行器] 已启动 检查间隔=30s
[批次执行器] 检查 2 个待执行批次
[批次执行器] 🎯 触发批次 DOGE/USDT 第2批 触发价=0.104763 当前价=0.104500
[批次执行器] 💸 执行: 正在下单 DOGE/USDT long 15.00 USDT ...
[批次执行器] ✔ 批次 2 执行成功
[批次执行器] 批次 2 已执行 价格=0.104500 数量=143.54 金额=15.00
```

### 监控指标

- 待执行批次数量
- 触发执行次数
- 执行成功/失败率
- 平均执行延迟

---

## 🔒 安全考虑

### 1. 防止重复执行

```go
// 在 UpdateBatchStatus 前加锁
var batchLocks sync.Map

func (be *BatchExecutor) executeBatch(...) error {
	lockKey := fmt.Sprintf("%s_%d", pb.StrategyID, batch.BatchNo)
	
	// 尝试获取锁
	if _, loaded := batchLocks.LoadOrStore(lockKey, true); loaded {
		return errors.New("批次正在执行中")
	}
	defer batchLocks.Delete(lockKey)
	
	// ... 执行逻辑 ...
}
```

### 2. 价格滑点保护

```go
func (be *BatchExecutor) shouldExecute(batch domain.PositionBatch, currentPrice float64) bool {
	// 允许 0.5% 的滑点
	maxPrice := batch.TriggerPrice * 1.005
	return currentPrice <= maxPrice
}
```

### 3. 余额检查

```go
func (be *BatchExecutor) executeBatch(...) error {
	// 执行前检查 USDT 余额
	balances, err := be.executor.FetchFullBalance(ctx)
	// ... 余额不足则跳过 ...
}
```

---

## 🚀 优化建议

### 1. 批量获取价格

```go
// 一次性获取所有需要的交易对价格
prices := be.getBatchPrices(ctx, uniquePairs)
```

### 2. 优先级队列

```go
// 按触发价格排序，优先检查接近触发的批次
sort.Slice(batches, func(i, j int) bool {
	return batches[i].TriggerPrice > batches[j].TriggerPrice
})
```

### 3. 动态调整间隔

```go
// 有待执行批次时，缩短检查间隔
if len(pendingBatches) > 0 {
	ticker.Reset(10 * time.Second)
} else {
	ticker.Reset(60 * time.Second)
}
```

---

## 📈 未来增强

1. **WebSocket 实时监控**（方案二）
2. **止盈止损自动触发**
3. **批次执行通知**（邮件/Webhook）
4. **批次执行历史统计**
5. **手动取消待执行批次**

---

## 总结

**推荐方案**：定时轮询（30秒间隔）

**实现步骤**：
1. ✅ 添加数据库查询方法
2. ✅ 创建 BatchExecutor 服务
3. ✅ 集成到 main.go
4. ✅ 添加配置选项
5. ✅ 测试验证

**优点**：
- 实现简单，易于维护
- 资源占用低
- 满足现货交易需求

**下一步**：
- 实现代码
- 单元测试
- 集成测试
- 生产验证
