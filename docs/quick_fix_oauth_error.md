# 快速修复 OAuth 认证错误

## 你遇到的错误

```
身份验证错误
验证过程中出错。请重试。
Request ID: dac62a53-ae1f-9d58-b835-1ff8cc57af86
```

## 原因

OpenAI OAuth 需要官方注册的 Client ID，我们使用的测试 Client ID 无法通过认证。

## 🚀 立即修复（推荐）

### 步骤 1：切换到 API Key 模式

你已经配置了智谱 AI 的 API Key，直接使用即可：

**方法一：在 Web UI 切换**
1. 访问 `http://localhost:8080/static/oauth.html`
2. 在顶部点击 **"🔑 API Key"** 按钮
3. 完成！系统会使用你的智谱 AI API

**方法二：修改 .env 文件**

在 `.env` 文件中添加：
```bash
# 设置为 API Key 模式
LLM_AUTH_MODE=api_key
```

然后重启服务：
```bash
go run .
```

### 步骤 2：验证配置

启动后应该看到：
```
🔐 OAuth 服务已启动
🔑 LLM 认证管理器已初始化 模式=api_key 提供商=openai
[LLM Auth] 使用 API Key 认证
[信号] 大模型已就绪 模型=glm-4.7
```

## 你的当前配置

根据 `.env.example`，你使用的是：
- **LLM 提供商**: 智谱 AI (BigModel)
- **模型**: glm-4.7
- **API Key**: 已配置
- **Base URL**: https://open.bigmodel.cn/api/paas/v4/

这个配置**不需要 OAuth**，直接使用 API Key 即可。

## 为什么会有这个错误？

1. **OAuth 是为 OpenAI/Anthropic 设计的**
   - 适用于 ChatGPT Plus/Claude Pro 订阅用户
   - 可以使用订阅额度调用 API

2. **智谱 AI 不支持 OAuth**
   - 智谱 AI 只支持 API Key 认证
   - 需要使用 API Key 模式

3. **默认是 auto 模式**
   - 系统会尝试 OAuth，失败后降级到 API Key
   - 但 OAuth 尝试会显示错误页面

## 完整的 .env 配置示例

```bash
# HTTP 服务配置
HTTP_ADDR=:8080
SQLITE_DSN=file:./ai_quant.db?_pragma=busy_timeout(5000)
REQUEST_TIMEOUT_SEC=120

# LLM 配置（智谱 AI）
OPENAI_API_KEY=0b033df46a31482695111f4ed4cec2b5.Gtvih8m3ONExHPcH
OPENAI_MODEL=glm-4.7
OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4/

# LLM 认证模式（重要！）
LLM_AUTH_MODE=api_key
LLM_AUTH_PROVIDER=openai

# 交易所配置
EXCHANGE_BASE_URL=https://api.binance.com
EXCHANGE_API_KEY=
EXCHANGE_SECRET_KEY=

# 风控参数
MAX_SINGLE_STAKE_USDT=50
MAX_DAILY_LOSS_USDT=100
MAX_EXPOSURE_USDT=200
MIN_CONFIDENCE=0.55

# 模拟模式
DRY_RUN=true

# 定时任务
AUTO_RUN_ENABLED=false
AUTO_RUN_INTERVAL_SEC=3600
AUTO_RUN_PAIRS=DOGE/USDT
```

## 其他 LLM 提供商的配置

### 使用真正的 OpenAI API

```bash
OPENAI_API_KEY=sk-proj-xxx
OPENAI_MODEL=gpt-4o-mini
OPENAI_BASE_URL=

LLM_AUTH_MODE=api_key
```

### 使用 Anthropic API

```bash
OPENAI_API_KEY=sk-ant-xxx
OPENAI_MODEL=claude-3-5-sonnet-20241022
OPENAI_BASE_URL=https://api.anthropic.com

LLM_AUTH_MODE=api_key
LLM_AUTH_PROVIDER=anthropic
```

### 使用 ChatGPT Plus OAuth（需要订阅）

```bash
# 不需要 API Key
OPENAI_MODEL=gpt-4o

LLM_AUTH_MODE=oauth
LLM_AUTH_PROVIDER=openai
```

然后在 Web UI 登录 OpenAI 账号。

## 总结

**对于你的情况**：
1. ✅ 你使用智谱 AI，已有 API Key
2. ✅ 设置 `LLM_AUTH_MODE=api_key`
3. ✅ 无需使用 OAuth
4. ✅ 系统会正常工作

**OAuth 功能适用于**：
- 有 ChatGPT Plus/Pro 订阅的用户
- 有 Claude Pro/Max 订阅的用户
- 想节省 API 费用的用户

你不需要 OAuth，直接用 API Key 就好！
