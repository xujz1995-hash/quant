# 批次执行优化方案（定时交易场景）

## 🎯 场景分析

### 你的使用场景

```
定时器: 每 1 小时触发一次交易
  ↓
生成信号 → 风控 → 建仓策略 → 执行第1批
  ↓
第2、3批需要等待价格下跌
```

### 问题

❌ **30秒检查一次太频繁了！**

原因：
- 你的交易是每小时触发一次
- 第2、3批通常需要等待几小时甚至几天
- 30秒检查一次会造成：
  - 无谓的CPU消耗
  - 大量无效的API调用
  - 日志刷屏

---

## 💡 优化方案

### 方案一：降低检查频率（推荐）

**调整为 5-10 分钟检查一次**

```bash
# .env
BATCH_EXECUTOR_INTERVAL_SEC=300  # 5分钟
# 或
BATCH_EXECUTOR_INTERVAL_SEC=600  # 10分钟
```

**理由**：
- ✅ 金字塔策略：第2批触发价通常是下跌2%，需要等待较长时间
- ✅ 5-10分钟的延迟完全可以接受
- ✅ 大幅减少资源消耗
- ✅ 仍然能及时捕捉价格变化

**适用场景**：
- 定时交易（每小时/每天）
- 现货交易
- 中长期持仓

---

### 方案二：智能动态间隔

**根据价格距离动态调整检查频率**

```go
func (be *BatchExecutor) calculateInterval(batches []PendingBatch, prices map[string]float64) time.Duration {
	minDistance := 100.0 // 最小价格距离百分比
	
	for _, pb := range batches {
		currentPrice := prices[pb.Pair]
		for _, batch := range pb.Batches {
			distance := (currentPrice - batch.TriggerPrice) / currentPrice * 100
			if distance < minDistance {
				minDistance = distance
			}
		}
	}
	
	// 根据距离调整间隔
	if minDistance < 0.5 {
		return 1 * time.Minute   // 接近触发，1分钟检查
	} else if minDistance < 2 {
		return 5 * time.Minute   // 较近，5分钟检查
	} else {
		return 15 * time.Minute  // 较远，15分钟检查
	}
}
```

**优势**：
- ✅ 智能调节，节省资源
- ✅ 接近触发时提高精度
- ✅ 距离较远时降低频率

---

### 方案三：按需触发（最优）

**只在有待执行批次时才启动检查**

```go
// 在执行第1批后，启动批次执行器
func (s *Service) RunCycle(...) {
	// ... 执行第1批 ...
	
	// 如果有后续批次，通知批次执行器开始监控
	if len(posStrategy.Batches) > 1 {
		s.batchExecutor.AddMonitoring(posStrategy.ID, posStrategy.Batches[1:])
	}
}

// BatchExecutor 只在有监控任务时运行
func (be *BatchExecutor) Start(ctx context.Context) {
	for {
		// 等待有新的监控任务
		select {
		case <-be.newTaskCh:
			// 开始定时检查
			be.startMonitoring(ctx)
		case <-ctx.Done():
			return
		}
	}
}
```

**优势**：
- ✅ 完全按需运行
- ✅ 没有待执行批次时，零资源消耗
- ✅ 最节省资源

---

## 📊 方案对比

| 方案 | 检查频率 | 资源消耗 | 实现复杂度 | 推荐度 |
|------|---------|---------|-----------|--------|
| **方案一：降低频率** | 5-10分钟 | 低 | 简单 | ⭐⭐⭐⭐⭐ |
| **方案二：动态间隔** | 1-15分钟 | 中 | 中等 | ⭐⭐⭐⭐ |
| **方案三：按需触发** | 按需 | 极低 | 复杂 | ⭐⭐⭐ |

---

## 🚀 推荐配置

### 对于你的场景（定时交易）

```bash
# .env

# 定时器配置
AUTO_RUN_ENABLED=true
AUTO_RUN_INTERVAL_SEC=3600  # 每小时触发交易

# 批次执行器配置
BATCH_EXECUTOR_ENABLED=true
BATCH_EXECUTOR_INTERVAL_SEC=600  # 10分钟检查一次
```

### 计算逻辑

```
假设金字塔策略:
- 第1批: 立即执行
- 第2批: 下跌 2% 时触发
- 第3批: 下跌 4% 时触发

如果当前价格 0.10:
- 第2批触发价: 0.098 (需要下跌 0.002)
- 第3批触发价: 0.096 (需要下跌 0.004)

市场波动:
- 正常情况: 每小时波动 1-3%
- 剧烈波动: 每小时波动 5-10%

结论:
- 10分钟检查一次，每小时检查6次
- 足以捕捉 2-4% 的价格变化
- 不会错过触发机会
```

---

## 💻 实现代码（方案一）

### 简化版 BatchExecutor

```go
// internal/executor/batch_executor.go

package executor

import (
	"context"
	"log"
	"time"
)

type BatchExecutor struct {
	repo     store.Repository
	executor execution.Executor
	interval time.Duration
	enabled  bool
}

func NewBatchExecutor(repo store.Repository, executor execution.Executor, intervalSec int, enabled bool) *BatchExecutor {
	return &BatchExecutor{
		repo:     repo,
		executor: executor,
		interval: time.Duration(intervalSec) * time.Second,
		enabled:  enabled,
	}
}

func (be *BatchExecutor) Start(ctx context.Context) {
	if !be.enabled {
		log.Println("[批次执行器] 已禁用")
		return
	}

	log.Printf("[批次执行器] 已启动 检查间隔=%s", be.interval)

	ticker := time.NewTicker(be.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			be.checkAndExecute(ctx)
		case <-ctx.Done():
			log.Println("[批次执行器] 已停止")
			return
		}
	}
}

func (be *BatchExecutor) checkAndExecute(ctx context.Context) {
	// 查询待执行批次
	batches, err := be.repo.ListPendingBatches(ctx)
	if err != nil {
		log.Printf("[批次执行器] ⚠ 查询失败: %v", err)
		return
	}

	if len(batches) == 0 {
		// 没有待执行批次，静默返回
		return
	}

	log.Printf("[批次执行器] 检查 %d 个待执行批次", len(batches))

	// 执行检查逻辑
	// ... (与之前相同)
}
```

### 配置加载

```go
// config/config.go

type Config struct {
	// ... 现有字段 ...
	
	BatchExecutorEnabled    bool
	BatchExecutorIntervalSec int
}

func Load() Config {
	// ... 现有代码 ...
	
	cfg.BatchExecutorEnabled = getEnvBool("BATCH_EXECUTOR_ENABLED", true)
	cfg.BatchExecutorIntervalSec = getEnvInt("BATCH_EXECUTOR_INTERVAL_SEC", 600) // 默认10分钟
	
	return cfg
}
```

### 主程序集成

```go
// main.go

func main() {
	// ... 现有初始化 ...

	// 初始化批次执行器
	if cfg.BatchExecutorEnabled {
		batchExecutor := executor.NewBatchExecutor(
			repo, 
			execAgent, 
			cfg.BatchExecutorIntervalSec,
			cfg.BatchExecutorEnabled,
		)
		go batchExecutor.Start(context.Background())
		log.Printf("📦 批次执行器已启动 间隔=%ds", cfg.BatchExecutorIntervalSec)
	} else {
		log.Println("📦 批次执行器已禁用")
	}

	// ... HTTP 服务器启动 ...
}
```

---

## 📈 资源消耗对比

### 30秒间隔（原方案）

```
每小时检查次数: 120次
每天检查次数: 2,880次
API调用: 2,880次/天
CPU占用: 持续运行
```

### 10分钟间隔（推荐）

```
每小时检查次数: 6次
每天检查次数: 144次
API调用: 144次/天
CPU占用: 极低
节省: 95% ✅
```

---

## 🎯 最佳实践

### 1. 根据交易频率调整

```bash
# 高频交易（每5分钟）
BATCH_EXECUTOR_INTERVAL_SEC=60   # 1分钟

# 中频交易（每小时）
BATCH_EXECUTOR_INTERVAL_SEC=600  # 10分钟 ✅ 推荐

# 低频交易（每天）
BATCH_EXECUTOR_INTERVAL_SEC=1800 # 30分钟
```

### 2. 监控日志

```
# 正常情况（没有待执行批次）
[批次执行器] 已启动 检查间隔=10m0s
# 静默运行，不输出日志

# 有待执行批次时
[批次执行器] 检查 2 个待执行批次
[批次执行器] DOGE/USDT 当前价=0.105 第2批触发价=0.098 (距离 7.1%)
[批次执行器] BTC/USDT 当前价=45000 第2批触发价=44100 (距离 2.0%)

# 触发执行时
[批次执行器] 🎯 触发批次 DOGE/USDT 第2批
[批次执行器] ✔ 批次 2 执行成功
```

### 3. 测试建议

```bash
# 开发测试时，使用较短间隔
BATCH_EXECUTOR_INTERVAL_SEC=30

# 生产环境，使用合理间隔
BATCH_EXECUTOR_INTERVAL_SEC=600
```

---

## 🔧 可选优化

### 1. 添加手动触发接口

```go
// HTTP API
POST /api/v1/batches/check

// 立即检查并执行待触发批次
```

### 2. 批次执行通知

```go
// 批次执行成功后发送通知
func (be *BatchExecutor) notifyBatchExecuted(batch PendingBatch) {
	// 发送邮件/Webhook/推送通知
	log.Printf("📧 批次执行通知: %s 第%d批已执行", batch.Pair, batch.BatchNo)
}
```

### 3. 批次执行统计

```go
// 记录批次执行统计
type BatchStats struct {
	TotalBatches    int
	ExecutedBatches int
	FailedBatches   int
	AvgExecutionTime time.Duration
}
```

---

## 总结

### 对于你的定时交易场景

**推荐配置**：
```bash
BATCH_EXECUTOR_ENABLED=true
BATCH_EXECUTOR_INTERVAL_SEC=600  # 10分钟
```

**理由**：
- ✅ 每小时交易一次，10分钟检查足够
- ✅ 节省95%的资源消耗
- ✅ 不会错过价格触发机会
- ✅ 实现简单，易于维护

**下一步**：
1. 实现 BatchExecutor（10分钟间隔）
2. 添加配置选项
3. 集成到主程序
4. 测试验证

需要我现在开始实现吗？
