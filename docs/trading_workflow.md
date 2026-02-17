# AI Quant 交易流程说明

## 📋 完整交易流程

### 1️⃣ 启动阶段

```
启动服务
  ↓
加载配置 (.env)
  ↓
初始化数据库 (SQLite)
  ↓
初始化 OAuth 服务
  ↓
初始化 LLM 认证管理器 (API Key/OAuth)
  ↓
初始化各个 Agent:
  - Signal Agent (信号生成)
  - Risk Agent (风控评估)
  - Position Agent (建仓策略) ← 新增！
  - Execution Agent (订单执行)
  ↓
同步持仓数据
  ↓
启动定时器 (可选)
  ↓
启动 HTTP API 服务器
```

**日志示例**：
```
🔐 OAuth 服务已启动
🔑 LLM 认证管理器已初始化 模式=api_key 提供商=openai
[信号] 大模型已就绪 模型=glm-4.7
📈 交易模式: 现货交易
[持仓] 已有 1 条持仓记录
[定时器] 已启动 间隔=1h0m0s 交易对=[DOGE/USDT]
AI Quant 服务启动 地址=:8080 模式=spot 模拟=false
```

---

### 2️⃣ 交易周期执行流程

当触发交易（手动或定时器）时，执行以下步骤：

#### **Step 1: 创建周期**
```
创建 Cycle 记录
  - ID: 唯一标识
  - Pair: 交易对 (如 DOGE/USDT)
  - Status: running
  - 保存到数据库
```

#### **Step 2: 获取行情快照**
```
从 Binance 获取实时行情
  - 当前价格
  - 24h 涨跌幅
  - 成交量等
```

**日志示例**：
```
[周期:abc12345] ▶ 开始执行 交易对=DOGE/USDT
[周期:abc12345] 📊 行情快照 价格=0.106901 24h涨跌=-2.34%
```

#### **Step 3: 信号生成 (Signal Agent)** 🤖
```
调用 LLM (大模型)
  ↓
输入:
  - 交易对信息
  - 实时行情
  - 账户余额
  - 历史持仓
  - 市场趋势
  ↓
LLM 分析并生成:
  - 方向: long (买入) / close (卖出) / hold (观望)
  - 置信度: 0.0 ~ 1.0
  - 理由: 详细分析
  - 思维链: AI 推理过程
  ↓
保存 Signal 到数据库
```

**日志示例**：
```
[周期:abc12345] 🤖 信号: 正在调用大模型分析 DOGE/USDT ...
[周期:abc12345] ✔ 信号: 方向=long 置信度=0.78 理由="技术面突破关键阻力位" (耗时3.2s)
```

#### **Step 4: 风控评估 (Risk Agent)** 🛡️
```
检查风控规则:
  ✓ 置信度是否 >= MIN_CONFIDENCE (0.55)
  ✓ 单笔金额是否 <= MAX_SINGLE_STAKE_USDT (50)
  ✓ 总仓位是否 <= MAX_EXPOSURE_USDT (200)
  ✓ 当日亏损是否 <= MAX_DAILY_LOSS_USDT (100)
  ↓
决策:
  - Approved: true/false
  - MaxStakeUSDT: 允许的最大金额
  - RejectReason: 拒绝原因（如果拒绝）
  ↓
保存 RiskDecision 到数据库
```

**日志示例**：
```
[周期:abc12345] 🛡️ 风控: 正在评估 ...
[周期:abc12345] ✔ 风控: 已通过 最大仓位=50.00 USDT
```

或者拒绝：
```
[周期:abc12345] ⚠️ 风控: 已拒绝 原因="置信度不足 (0.52 < 0.55)"
```

#### **Step 5: 建仓策略生成 (Position Agent)** 📊 ← **新增步骤！**
```
根据信号置信度选择策略:
  ↓
置信度 >= 0.75 → 全仓策略 (full)
  - 一次性建仓 100%
  - 止盈 5%, 止损 2%
  ↓
置信度 0.60~0.75 → 金字塔策略 (pyramid)
  - 第1批: 50% (当前价)
  - 第2批: 30% (下跌 2% 时)
  - 第3批: 20% (下跌 4% 时)
  - 止盈 8%, 止损 3%
  ↓
置信度 < 0.60 → 网格策略 (grid)
  - 分5批，每批 20%
  - 价格间隔 1%
  - 止盈 10%, 止损 4%
  ↓
保存 PositionStrategy 到数据库
```

**日志示例**：
```
[周期:abc12345] 📊 建仓策略: 正在生成 ...
[周期:abc12345] ✔ 建仓策略: pyramid 分批=3 止盈=8.0% 止损=3.0%
[建仓策略] DOGE/USDT 策略=pyramid 总金额=50.00 分批=3 止盈=8.0% 止损=3.0%
```

#### **Step 6: 订单执行 (Execution Agent)** 💰
```
如果是买入 (long):
  ↓
  执行第1批建仓
  - 金额: 根据策略 (如 50% = 25 USDT)
  - 检查 USDT 余额是否充足
  - 调用 Binance API 下单
  ↓
  
如果是卖出 (close):
  ↓
  查询持仓数量
  - 模拟盘: 从本地数据库
  - 实盘: 从 Binance API
  ↓
  全部卖出
  ↓

下单成功:
  - 保存 Order 到数据库
  - 更新 Holdings 持仓
  - 周期状态 → success
  ↓
下单失败:
  - 记录错误信息
  - 周期状态 → failed
```

**日志示例**：
```
[周期:abc12345] 📦 执行第1批: 25.00 USDT (共3批)
[周期:abc12345] 💰 余额调整: 计划=25.00 可用=30.00 → 实际下单=25.00
[周期:abc12345] 💸 执行: 正在下单 DOGE/USDT long 25.00 USDT ...
[周期:abc12345] ✔ 执行: 订单已提交 ID=abc123 价格=0.106901 数量=233.78
[周期:abc12345] ■ 执行完毕 状态=成功 总耗时=5.8s
```

#### **Step 7: 更新持仓**
```
买入后:
  - 增加持仓数量
  - 更新平均成本
  - 更新总成本
  ↓
卖出后:
  - 减少持仓数量
  - 按比例减少成本
  - 如果全部卖出，数量归零
```

---

## 🔄 完整流程图

```
┌─────────────────────────────────────────────────────────┐
│                    交易周期开始                          │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
            ┌────────────────┐
            │  获取行情快照   │
            └────────┬───────┘
                     │
                     ▼
            ┌────────────────┐
            │  信号生成 🤖   │ ← LLM 分析
            │  (Signal)      │
            └────────┬───────┘
                     │
                     ▼
            ┌────────────────┐
            │  风控评估 🛡️   │
            │  (Risk)        │
            └────────┬───────┘
                     │
                ┌────┴────┐
                │ 通过？   │
                └────┬────┘
                     │
        ┌────────────┼────────────┐
        │ 否                      │ 是
        ▼                         ▼
   ┌─────────┐         ┌──────────────────┐
   │ 拒绝    │         │ 建仓策略生成 📊  │ ← 新增！
   │ 结束    │         │ (Position)       │
   └─────────┘         └────────┬─────────┘
                                │
                                ▼
                       ┌─────────────────┐
                       │ 订单执行 💰     │
                       │ (Execution)     │
                       └────────┬────────┘
                                │
                                ▼
                       ┌─────────────────┐
                       │ 更新持仓        │
                       │ (Holdings)      │
                       └────────┬────────┘
                                │
                                ▼
                       ┌─────────────────┐
                       │ 周期完成 ✅     │
                       └─────────────────┘
```

---

## 📊 数据库表结构

### 核心表

1. **cycles** - 交易周期
   - id, pair, status, created_at, updated_at

2. **signals** - 信号记录
   - id, cycle_id, side, confidence, reason, thinking

3. **risk_checks** - 风控决策
   - id, cycle_id, approved, max_stake_usdt, reject_reason

4. **position_strategies** - 建仓策略 ← **新增！**
   - id, cycle_id, strategy, total_amount, entry_levels, batches, take_profit_percent, stop_loss_percent

5. **orders** - 订单记录
   - id, cycle_id, pair, side, stake_usdt, status, filled_price, filled_qty

6. **holdings** - 持仓汇总
   - id, pair, symbol, quantity, avg_price, total_cost

7. **cycle_logs** - 周期日志
   - id, cycle_id, stage, message, created_at

---

## 🎯 建仓策略详解

### 三种策略对比

| 策略 | 置信度 | 分批 | 止盈 | 止损 | 适用场景 |
|------|--------|------|------|------|----------|
| **全仓 (full)** | ≥0.75 | 1批 100% | 5% | 2% | 高确定性机会 |
| **金字塔 (pyramid)** | 0.60-0.75 | 3批 50%+30%+20% | 8% | 3% | 中等确定性，逢低加仓 |
| **网格 (grid)** | <0.60 | 5批 各20% | 10% | 4% | 低确定性，震荡行情 |

### 金字塔策略示例

假设风控批准 50 USDT，当前价格 0.10：

```
第1批: 25 USDT @ 0.10 (立即执行)
第2批: 15 USDT @ 0.098 (下跌 2% 时触发)
第3批: 10 USDT @ 0.096 (下跌 4% 时触发)

止盈: 0.108 (+8%)
止损: 0.097 (-3%)
```

**优势**：
- 降低平均成本
- 分散风险
- 适应市场波动

---

## 🚀 如何触发交易

### 方法一：手动触发（Web UI）

1. 访问 `http://localhost:8080`
2. 点击 **"运行周期"** 按钮
3. 选择交易对（如 DOGE/USDT）
4. 点击 **"执行"**

### 方法二：API 调用

```bash
curl -X POST http://localhost:8080/api/v1/cycles/run \
  -H "Content-Type: application/json" \
  -d '{"pair": "DOGE/USDT"}'
```

### 方法三：定时器自动执行

在 `.env` 中配置：
```bash
AUTO_RUN_ENABLED=true
AUTO_RUN_INTERVAL_SEC=3600  # 每小时
AUTO_RUN_PAIRS=DOGE/USDT,BTC/USDT
```

---

## 📈 查看结果

### 1. 查看周期列表
```bash
curl http://localhost:8080/api/v1/cycles
```

### 2. 查看周期详情
```bash
curl http://localhost:8080/api/v1/cycles/{cycle_id}
```

返回完整信息：
- 信号 (Signal)
- 风控决策 (Risk)
- **建仓策略 (Position Strategy)** ← 新增！
- 订单 (Order)
- 日志 (Logs)

### 3. 查看持仓
```bash
curl http://localhost:8080/api/v1/holdings
```

### 4. 查看账户余额
```bash
curl http://localhost:8080/api/v1/balance
```

---

## 🔧 配置说明

### 关键配置项

```bash
# LLM 配置
OPENAI_API_KEY=你的密钥
OPENAI_MODEL=glm-4.7
OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4/

# LLM 认证模式
LLM_AUTH_MODE=api_key  # api_key, oauth, auto

# 风控参数
MAX_SINGLE_STAKE_USDT=50     # 单笔最大下单金额上限
MAX_EXPOSURE_USDT=200        # 最大总仓位
MAX_DAILY_LOSS_USDT=100      # 每日最大亏损
MIN_CONFIDENCE=0.55          # 最低置信度

# 交易模式
TRADING_MODE=spot            # spot (现货) 或 futures (合约)
DRY_RUN=true                 # 模拟模式

# 定时器
AUTO_RUN_ENABLED=false
AUTO_RUN_INTERVAL_SEC=3600
AUTO_RUN_PAIRS=DOGE/USDT
```

---

## 💡 最佳实践

### 1. 初次使用
- ✅ 使用 `DRY_RUN=true` 模拟模式
- ✅ 设置较高的 `MIN_CONFIDENCE` (如 0.65)
- ✅ 设置较小的 `MAX_SINGLE_STAKE_USDT` (如 20)

### 2. 观察周期
- ✅ 运行 1-2 周，观察信号质量
- ✅ 查看建仓策略是否合理
- ✅ 统计胜率和盈亏比

### 3. 实盘前
- ✅ 确保 API Key 权限正确
- ✅ 充值足够的 USDT
- ✅ 设置合理的风控参数
- ✅ 准备好止损止盈策略

### 4. 实盘运行
- ✅ 从小金额开始
- ✅ 定期检查持仓
- ✅ 关注异常日志
- ✅ 及时调整参数

---

## 🎓 示例：完整的交易周期

```
[周期:a1b2c3d4] ▶ 开始执行 交易对=DOGE/USDT
[周期:a1b2c3d4] 📊 行情快照 价格=0.106901 24h涨跌=-2.34%
[周期:a1b2c3d4] 🤖 信号: 正在调用大模型分析 DOGE/USDT ...
[周期:a1b2c3d4] ✔ 信号: 方向=long 置信度=0.72 理由="技术面突破，成交量放大" (耗时3.2s)
[周期:a1b2c3d4] 🛡️ 风控: 正在评估 ...
[周期:a1b2c3d4] ✔ 风控: 已通过 最大仓位=50.00 USDT
[周期:a1b2c3d4] 📊 建仓策略: 正在生成 ...
[建仓策略] DOGE/USDT 策略=pyramid 总金额=50.00 分批=3 止盈=8.0% 止损=3.0%
[周期:a1b2c3d4] ✔ 建仓策略: pyramid 分批=3 止盈=8.0% 止损=3.0%
[周期:a1b2c3d4] 📦 执行第1批: 25.00 USDT (共3批)
[周期:a1b2c3d4] 💰 余额调整: 计划=25.00 可用=100.00 → 实际下单=25.00
[周期:a1b2c3d4] 💸 执行: 正在下单 DOGE/USDT long 25.00 USDT ...
[周期:a1b2c3d4] ✔ 执行: 订单已提交 ID=abc123 价格=0.106901 数量=233.78
[周期:a1b2c3d4] ■ 执行完毕 状态=成功 总耗时=5.8s
```

---

## 🆕 新增功能总结

### 建仓策略 (Position Strategy)

**之前**：
- 风控通过后直接执行全部金额

**现在**：
- 风控通过后先生成建仓策略
- 根据置信度智能选择策略
- 支持分批建仓，降低风险
- 自动计算止盈止损
- 只执行第一批，后续批次待实现

**数据库新增**：
- `position_strategies` 表
- 存储策略类型、分批计划、止盈止损等

**日志新增**：
- 建仓策略生成步骤
- 批次执行信息

---

## 📚 相关文档

- [配置说明](../README.md)
- [API 文档](./api_documentation.md)
- [OAuth 使用指南](./oauth_usage.md)
- [LLM 认证切换](./llm_auth_switch.md)
