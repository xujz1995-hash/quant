# LLM 认证模式切换指南

## 功能概述

系统现在支持在 **OAuth Token** 和 **API Key** 之间动态切换，无需重启服务。

## 三种认证模式

### 1. 🔑 API Key 模式
- 使用传统的 API Key 认证
- 适合：有 API Key 的用户
- 配置：在 `.env` 中设置 `OPENAI_API_KEY`

### 2. 🔐 OAuth 模式
- 使用 OAuth Token 认证
- 适合：使用 ChatGPT Plus/Pro 或 Claude Pro/Max 订阅的用户
- 需要：先通过 OAuth 登录获取 token

### 3. 🤖 自动模式（推荐）
- 优先使用 OAuth，如果不可用则降级到 API Key
- 适合：大多数用户
- 智能选择最佳认证方式

## 配置方式

### 方式一：环境变量配置（启动时）

在 `.env` 文件中设置：

```bash
# LLM 认证模式: api_key, oauth, auto（默认）
LLM_AUTH_MODE=auto

# OAuth 提供商: openai, anthropic（默认 openai）
LLM_AUTH_PROVIDER=openai

# OpenAI API Key（可选，auto 模式下作为备用）
OPENAI_API_KEY=sk-xxx
```

### 方式二：Web UI 动态切换（运行时）

1. 访问 `http://localhost:8080/static/oauth.html`
2. 在顶部的 **"🔑 LLM 认证模式"** 区域：
   - 点击 **"🔑 API Key"** - 切换到 API Key 模式
   - 点击 **"🔐 OAuth"** - 切换到 OAuth 模式
   - 点击 **"🤖 自动"** - 切换到自动模式
3. 选择提供商：
   - 点击 **"提供商: OpenAI"** - 使用 OpenAI
   - 点击 **"提供商: Anthropic"** - 使用 Anthropic

### 方式三：API 调用

#### 查看当前认证状态
```bash
curl http://localhost:8080/llm-auth/status
```

响应示例：
```json
{
  "status": {
    "mode": "auto",
    "provider": "openai",
    "api_key": true,
    "oauth_available": true,
    "oauth_expires_at": "2026-02-16T12:00:00Z",
    "oauth_account_id": "user-xxx"
  }
}
```

#### 切换认证模式
```bash
# 切换到 API Key 模式
curl -X POST http://localhost:8080/llm-auth/mode \
  -H "Content-Type: application/json" \
  -d '{"mode": "api_key"}'

# 切换到 OAuth 模式
curl -X POST http://localhost:8080/llm-auth/mode \
  -H "Content-Type: application/json" \
  -d '{"mode": "oauth"}'

# 切换到自动模式
curl -X POST http://localhost:8080/llm-auth/mode \
  -H "Content-Type: application/json" \
  -d '{"mode": "auto"}'
```

#### 切换 OAuth 提供商
```bash
# 切换到 OpenAI
curl -X POST http://localhost:8080/llm-auth/provider \
  -H "Content-Type: application/json" \
  -d '{"provider": "openai"}'

# 切换到 Anthropic
curl -X POST http://localhost:8080/llm-auth/provider \
  -H "Content-Type: application/json" \
  -d '{"provider": "anthropic"}'
```

## 使用场景

### 场景 1：使用 ChatGPT Plus 订阅

1. 设置模式为 `oauth` 或 `auto`
2. 访问 OAuth 页面登录 OpenAI 账号
3. 系统自动使用 OAuth Token 调用 API
4. 无需担心 API Key 费用

### 场景 2：使用 API Key

1. 在 `.env` 中配置 `OPENAI_API_KEY`
2. 设置模式为 `api_key`
3. 系统使用 API Key 调用 API

### 场景 3：混合使用（推荐）

1. 同时配置 API Key 和 OAuth
2. 设置模式为 `auto`
3. 系统优先使用 OAuth，失败时自动降级到 API Key
4. 最大化可用性

## 认证流程

```
┌─────────────────────┐
│  LLMAuthManager     │
│  (认证管理器)        │
└──────────┬──────────┘
           │
           ├─ mode = "api_key"
           │  └─> 返回 API Key
           │
           ├─ mode = "oauth"
           │  └─> 从 OAuth Service 获取 Token
           │      └─> 自动刷新过期 Token
           │
           └─ mode = "auto"
              ├─> 尝试 OAuth Token
              │   ├─ 成功 ✅
              │   └─ 失败 ❌
              └─> 降级到 API Key
```

## 日志输出

启动时会显示认证状态：
```
🔐 OAuth 服务已启动
🔑 LLM 认证管理器已初始化 模式=auto 提供商=openai
[信号] LLM 认证模式=auto 提供商=openai OAuth可用=true
[LLM Auth] 自动模式: 使用 OAuth Token (provider=openai)
[信号] 大模型已就绪 模型=gpt-4o-mini
```

切换模式时：
```
[LLM Auth] 认证模式已切换为: oauth
[LLM Auth] 使用 OAuth Token 认证 (provider=openai)
```

## 优势

### ✅ 成本节省
- 使用 ChatGPT Plus/Pro 订阅的 OAuth Token
- 无需额外购买 API Key
- 充分利用现有订阅

### ✅ 灵活切换
- 运行时动态切换，无需重启
- Web UI 一键切换
- API 调用切换

### ✅ 高可用性
- 自动模式提供故障转移
- OAuth 失败自动降级到 API Key
- Token 自动刷新

### ✅ 多提供商支持
- OpenAI (ChatGPT/Codex)
- Anthropic (Claude)
- 轻松扩展更多提供商

## 注意事项

1. **OAuth Token 有效期**
   - Token 会自动刷新
   - 过期前 5 分钟自动刷新
   - 刷新失败会在日志中提示

2. **API Key 备用**
   - 建议在 `auto` 模式下配置 API Key 作为备用
   - 确保服务的高可用性

3. **提供商选择**
   - OpenAI 和 Anthropic 使用不同的 API 端点
   - 确保选择正确的提供商

4. **安全性**
   - OAuth Token 和 API Key 都安全存储
   - Token 文件权限为 0600
   - 不要将认证信息提交到 Git

## 故障排查

### 问题 1：OAuth 模式下调用失败

**症状**：`获取 OAuth token 失败`

**解决方案**：
1. 检查是否已登录 OAuth：访问 `/static/oauth.html`
2. 查看 Token 是否过期：检查 `~/.ai_quant/auth-profiles.json`
3. 尝试刷新 Token：点击 "刷新" 按钮
4. 重新登录 OAuth

### 问题 2：自动模式不工作

**症状**：一直使用 API Key

**解决方案**：
1. 检查 OAuth 是否已登录
2. 查看日志确认 OAuth 状态
3. 手动切换到 OAuth 模式测试

### 问题 3：切换模式后不生效

**症状**：切换后仍使用旧模式

**原因**：LangChain 客户端在初始化时创建

**解决方案**：
- 当前实现：需要重启服务
- 未来改进：支持动态重新初始化客户端

## 未来改进

- [ ] 支持运行时重新初始化 LLM 客户端
- [ ] 添加认证模式切换的 Webhook 通知
- [ ] 支持多个 API Key 轮询
- [ ] 添加认证使用统计
- [ ] 支持更多 OAuth 提供商

## 相关文档

- [OAuth 使用指南](./oauth_usage.md)
- [OAuth 功能说明](../OAUTH_README.md)
