# AI Quant 系统架构文档

## 一、项目概述

AI Quant 是一个基于 Go 语言的 AI 驱动加密货币现货交易系统。
核心思路：LLM（大语言模型）分析市场数据 → 生成交易信号 → 风控审核 → 直接调 Binance API 下单。

- **语言**: Go 1.22
- **Web 框架**: Gin
- **数据库**: SQLite（via modernc.org/sqlite，纯 Go 实现）
- **LLM**: 智谱 GLM-4（通过 OpenAI 兼容接口，via langchaingo）
- **行情数据**: Binance 公开 API（无需 API Key）
- **下单**: Binance REST API（需要 API Key + Secret，仅实盘模式）
- **前端**: 纯 HTML/CSS/JS 单页应用

## 二、目录结构

```
ai_quant/
├── main.go                          # 入口：加载配置、初始化各模块、启动定时器和 HTTP 服务
├── SystemPrompt.md                  # LLM 系统提示词（现货交易策略指引）
├── UserPrompt.md                    # LLM 用户提示词模板（Go template 语法，动态填充市场数据）
├── .env                             # 环境变量配置（敏感信息，已 gitignore）
├── .env.example                     # 环境变量模板
├── go.mod / go.sum                  # Go 依赖管理
├── Makefile                         # 构建脚本（tidy/fmt/build/run）
│
├── client/                          # 前端（Go agent 管理界面）
│   ├── index.html                   # HTML 结构
│   ├── style.css                    # 样式（深色主题）
│   └── app.js                       # JS 逻辑（健康检查、运行周期、仓位列表）
│
├── docker/                          # Freqtrade Docker 环境（已不再作为核心依赖）
│   ├── docker-compose.yml
│   ├── dashboard.html               # Freqtrade 独立 Dashboard
│   └── freqtrade/user_data/
│       ├── config.json              # Freqtrade 配置（Binance 交易所）
│       └── strategies/ManualStrategy.py  # 空策略（不自动交易）
│
└── internal/                        # Go 业务代码
    ├── config/config.go             # 配置加载（从 .env 读取）
    ├── domain/types.go              # 核心数据结构定义
    ├── market/                      # 市场数据模块
    │   ├── binance.go               # Binance API 客户端（行情+情绪因子）
    │   ├── indicator.go             # 技术指标计算（EMA/MACD/RSI/ATR）
    │   └── prompt.go                # 提示词模板填充
    ├── agent/                       # 三个 Agent
    │   ├── signal/signal.go         # 信号 Agent（LLM + 规则引擎降级）
    │   ├── risk/risk.go             # 风控 Agent（规则引擎）
    │   └── execution/execution.go   # 执行 Agent（Binance 下单）
    ├── orchestrator/service.go      # 编排器（串联 信号→风控→执行 完整周期）
    ├── scheduler/scheduler.go       # 定时调度器（自动周期性运行）
    ├── store/sqlite.go              # SQLite 持久化层
    └── http/server.go               # HTTP API（Gin 路由和处理器）
```

## 三、核心流程

### 3.1 启动流程 (main.go)

```
1. config.Load()         → 从 .env 加载所有配置
2. store.NewSQLiteRepository() → 初始化 SQLite 数据库 + 建表
3. signal.New(cfg)       → 初始化信号 Agent（加载 SystemPrompt.md + UserPrompt.md + LLM 客户端）
4. risk.New(cfg)         → 初始化风控 Agent
5. execution.New(cfg)    → 初始化执行 Agent（Binance API 客户端）
6. orchestrator.New()    → 创建编排器（串联三个 Agent + 数据库）
7. scheduler.New()       → 如果 AUTO_RUN_ENABLED=true，启动定时任务
8. httpapi.NewRouter()   → 启动 Gin HTTP 服务
```

### 3.2 交易周期流程 (orchestrator.RunCycle)

每次执行一个交易周期（手动 API 调用或定时器触发）：

```
┌─────────────────────────────────────────────────────┐
│                  交易周期 (Cycle)                      │
│                                                       │
│  1. 创建 Cycle 记录 → SQLite                           │
│                                                       │
│  2. 信号生成 (Signal Agent)                            │
│     ├─ 从 Binance 获取实时行情（价格/K线/资金费率/OI）    │
│     ├─ 获取情绪因子（多空比/买卖比/恐惧贪婪指数）         │
│     ├─ 计算技术指标（EMA/MACD/RSI/ATR）                 │
│     ├─ 用 UserPrompt.md 模板填充数据生成用户提示词        │
│     ├─ 调用 LLM（SystemPrompt + UserPrompt）           │
│     ├─ 解析 LLM 返回的 JSON → signal/confidence/reason  │
│     └─ 失败时降级为规则引擎                              │
│     → 输出: Signal { side=long/close/none, confidence }  │
│                                                       │
│  3. 风控评估 (Risk Agent)                              │
│     ├─ 检查信号方向（none → 拒绝）                      │
│     ├─ 检查置信度（< MIN_CONFIDENCE → 拒绝）            │
│     ├─ close 信号：只检查置信度                         │
│     ├─ long 信号：检查每日亏损限额 + 敞口限额            │
│     └─ 计算允许的最大下单金额                            │
│     → 输出: RiskDecision { approved, maxStakeUSDT }     │
│                                                       │
│  4. 下单执行 (Execution Agent)                         │
│     ├─ DRY_RUN=true → 模拟成交，不调交易所              │
│     ├─ DRY_RUN=false →                                │
│     │   ├─ long → Binance POST /api/v3/order BUY      │
│     │   │   (quoteOrderQty=USDT金额, MARKET)           │
│     │   └─ close → Binance POST /api/v3/order SELL     │
│     │       (quantity=币数量, MARKET)                   │
│     └─ HMAC-SHA256 签名认证                             │
│     → 输出: Order { status, filledPrice }               │
│                                                       │
│  5. 保存所有记录到 SQLite                               │
│     cycles / signals / risk_decisions / orders / logs   │
└─────────────────────────────────────────────────────┘
```

### 3.3 信号类型

| 信号 | 含义 | 执行动作 |
|------|------|---------|
| `long` | 看多/买入 | 用 USDT 买入加密货币 |
| `close` | 卖出/平仓 | 卖出持有的加密货币换回 USDT |
| `hold` | 观望 | 不执行任何操作 |
| `none` | 无信号 | 不执行任何操作 |

## 四、模块详解

### 4.1 配置 (config.go)

自动从 `.env` 文件加载（使用 godotenv）。主要配置项：

| 配置 | 说明 | 默认值 |
|------|------|--------|
| HTTP_ADDR | HTTP 服务监听地址 | :8080 |
| SQLITE_DSN | SQLite 数据库路径 | file:./ai_quant.db |
| REQUEST_TIMEOUT_SEC | API 请求超时 | 15s |
| OPENAI_API_KEY | LLM API Key | 空（使用规则引擎） |
| OPENAI_MODEL | LLM 模型名 | gpt-4o-mini |
| OPENAI_BASE_URL | LLM API 基础 URL | 空（默认 OpenAI） |
| EXCHANGE_BASE_URL | 交易所 API 地址 | https://api.binance.com |
| EXCHANGE_API_KEY | Binance API Key | 空 |
| EXCHANGE_SECRET_KEY | Binance Secret Key | 空 |
| MAX_SINGLE_STAKE_USDT | 默认单笔下单金额 | 50 USDT |
| MAX_DAILY_LOSS_USDT | 每日最大亏损限额 | 100 USDT |
| MAX_EXPOSURE_USDT | 最大持仓敞口 | 200 USDT |
| MIN_CONFIDENCE | 最小置信度阈值 | 0.55 |
| DRY_RUN | 模拟模式开关 | true |
| AUTO_RUN_ENABLED | 定时器开关 | false |
| AUTO_RUN_INTERVAL_SEC | 定时执行间隔 | 60s |
| AUTO_RUN_PAIRS | 自动交易币对 | BTC/USDT |

### 4.2 市场数据 (market/)

**binance.go** - 从 Binance 公开 API 获取数据（无需 API Key）：
- 24h 行情（价格、涨跌幅）: `GET /api/v3/ticker/24hr`
- K 线数据（5m×50根 + 4h×30根）: `GET /api/v3/klines`
- 资金费率: `GET /fapi/v1/fundingRate`
- 持仓量: `GET /fapi/v1/openInterest`
- 多空比（全网/大户）: `GET /futures/data/globalLongShortAccountRatio` 等
- 主动买卖比: `GET /futures/data/takerlongshortRatio`
- 恐惧贪婪指数: `GET https://api.alternative.me/fng/`

**indicator.go** - 技术指标计算：
- EMA (指数移动平均)
- MACD (移动平均收敛散度)
- RSI (相对强弱指标)
- ATR (平均真实波幅)

**prompt.go** - 提示词构建：
- `PromptData` 结构体包含所有模板字段
- `BuildPrompt()` 用 Go template 填充 UserPrompt.md 模板
- 包含主交易对数据 + 额外关联交易对 + 账户状态 + 情绪因子

### 4.3 信号 Agent (signal/signal.go)

两种模式：
1. **LangChainAgent**（主模式）: 配置了 OPENAI_API_KEY 时使用
   - 加载 SystemPrompt.md 和 UserPrompt.md
   - 调用 Binance 获取实时行情 → 填充模板 → 调用 LLM
   - 解析 LLM 返回的 JSON（signal/confidence/reason）
   - 失败时自动降级为规则引擎
2. **RuleBasedAgent**（降级模式）: 简单的基于阈值的规则引擎

LLM 返回格式：
```json
{
  "signal": "long" | "close" | "hold" | "none",
  "confidence": 0.0-1.0,
  "reason": "中文解释",
  "ttl_seconds": 60-1800
}
```

### 4.4 风控 Agent (risk/risk.go)

纯规则引擎，检查项：
- 信号方向不为 none
- 置信度 >= MIN_CONFIDENCE
- long 信号额外检查：
  - 每日亏损未超限
  - 持仓敞口未超限
  - 计算允许的最大下单金额 = min(DEFAULT_STAKE, 剩余敞口)
- close 信号只检查置信度

### 4.5 执行 Agent (execution/execution.go)

**BinanceExecutor**，两种模式：
- **DRY_RUN=true**: 模拟成交，不调交易所 API
- **DRY_RUN=false**: 调用 Binance REST API
  - 买入 (long): `POST /api/v3/order` with side=BUY, quoteOrderQty=USDT金额
  - 卖出 (close): `POST /api/v3/order` with side=SELL, quantity=币数量
  - 使用 HMAC-SHA256 签名认证

### 4.6 编排器 (orchestrator/service.go)

串联完整交易周期：信号 → 风控 → 执行
- 每步都写 SQLite 日志
- 支持 `RunCycle` (执行交易)、`GetCycleReport` (查看报告)、`ListPositions` (查看仓位)

### 4.7 定时调度器 (scheduler/scheduler.go)

- 后台 goroutine + time.Ticker
- 按配置间隔遍历所有交易对，逐个调用 RunCycle
- 支持 Start/Stop

### 4.8 数据库 (store/sqlite.go)

SQLite 数据库，5 张表：
- `cycles` - 交易周期记录
- `signals` - LLM 生成的信号
- `risk_decisions` - 风控决策
- `orders` - 下单记录
- `cycle_logs` - 日志

### 4.9 HTTP API (http/server.go)

Gin 路由：
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 前端页面 |
| GET | `/static/*` | 静态资源 |
| GET | `/api/v1/health` | 健康检查 |
| POST | `/api/v1/cycles/run` | 手动触发交易周期 |
| GET | `/api/v1/cycles/:id` | 查看周期报告 |
| GET | `/api/v1/positions` | 查看仓位列表 |

### 4.10 前端 (client/)

纯前端单页应用，功能：
- 健康检查（心跳）
- 手动触发交易（选择交易对 → 调用 run API）
- 查看执行结果（信号/风控/下单详情）
- 查看仓位列表

## 五、数据流图

```
                                    ┌──────────────┐
                                    │  Binance API │
                                    │ (公开行情)    │
                                    └──────┬───────┘
                                           │ 价格/K线/资金费率/OI
                                           │ 多空比/买卖比
                                           ▼
┌──────────┐    ┌──────────┐    ┌──────────────────┐
│ 定时器    │───▶│ 编排器    │───▶│ 信号 Agent       │
│ Scheduler │    │Orchestr. │    │ ├─ 获取行情       │
└──────────┘    │          │    │ ├─ 算技术指标      │
                │          │    │ ├─ 填充提示词模板   │
┌──────────┐    │          │    │ ├─ 调用 LLM       │
│ HTTP API │───▶│          │    │ └─ 解析返回 JSON   │
│ /cycles/ │    │          │    └────────┬─────────┘
│   run    │    │          │             │ Signal
└──────────┘    │          │             ▼
                │          │    ┌──────────────────┐
                │          │───▶│ 风控 Agent       │
                │          │    │ ├─ 检查置信度     │
                │          │    │ ├─ 检查敞口       │
                │          │    │ └─ 计算最大仓位   │
                │          │    └────────┬─────────┘
                │          │             │ RiskDecision
                │          │             ▼
                │          │    ┌──────────────────┐
                │          │───▶│ 执行 Agent       │
                │          │    │ ├─ DRY_RUN:模拟   │
                │          │    │ └─ 实盘:Binance   │
                │          │    │     POST /order   │
                │          │    └────────┬─────────┘
                │          │             │ Order
                │          │             ▼
                │          │    ┌──────────────────┐
                │          │───▶│ SQLite 数据库     │
                └──────────┘    │ cycles/signals/  │
                                │ orders/risks/logs │
                                └──────────────────┘

┌──────────┐
│ 前端 UI  │◀── GET /api/v1/positions
│ client/  │◀── GET /api/v1/cycles/:id
└──────────┘
```

## 六、提示词体系

### SystemPrompt.md
- 定义 AI 角色：现货交易 Agent
- 交易环境：Binance 现货、无杠杆、只做多
- 四种信号：long（买入）/ close（卖出）/ hold / none
- 风险管理协议
- JSON 输出格式规范
- 技术指标解读指南
- 买入/卖出/观望条件

### UserPrompt.md（Go template）
动态填充内容：
- 当前快照（价格、24h涨跌、资金费率、OI）
- 短周期数据（5m K线 + EMA/MACD/RSI/Volume）
- 长周期数据（4h K线 + EMA20/EMA50/MACD/RSI/ATR）
- 情绪因子（恐惧贪婪指数、多空比、大户比、买卖比）
- 关联交易对数据
- 账户状态（净值、可用资金、收益率、夏普比率）
- 当前持仓

## 七、当前运行配置

- LLM: 智谱 GLM-4（通过 OpenAI 兼容接口）
- 行情数据: Binance 公开 API
- 交易执行: Binance REST API（已配置 API Key）
- 模式: DRY_RUN=true（模拟盘）
- 定时器: 每 4 小时执行一次
- 交易对: ETH/USDT
- 单笔下单: 50 USDT
- 最大敞口: 1000 USDT
- 最小置信度: 0.55

## 八、Docker 环境（可选，非核心）

docker/ 目录包含 Freqtrade 的 Docker 部署，但当前架构已不依赖 Freqtrade。
Go agent 直接对接 Binance API 下单，无需中间层。
Freqtrade 仅保留作为可选的辅助工具。
