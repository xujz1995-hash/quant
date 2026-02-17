# OpenAI OAuth 限制说明

## 问题说明

当你尝试使用 OpenAI OAuth 登录时，可能会遇到以下错误：

```
身份验证错误
验证过程中出错。请重试。
```

## 原因

OpenAI 的 OAuth 需要：
1. **注册的 Client ID** - 需要在 OpenAI 开发者平台注册应用
2. **回调 URL 白名单** - 需要在应用配置中添加回调地址
3. **企业账号** - 某些 OAuth 功能可能需要企业账号

我们当前使用的 `openclaw-codex` 是 OpenClaw 项目的 Client ID，不能直接使用。

## 解决方案

### 方案一：使用 API Key 模式（推荐）

你已经配置了智谱 AI 的 API Key，直接使用 API Key 模式即可：

```bash
# 在 .env 文件中设置
LLM_AUTH_MODE=api_key
```

或在 Web UI 中点击 **"🔑 API Key"** 按钮切换。

### 方案二：使用 Anthropic OAuth

Anthropic (Claude) 的 OAuth 相对更容易使用：

1. 安装 Claude CLI：
   ```bash
   npm install -g @anthropic-ai/claude-cli
   ```

2. 生成 setup-token：
   ```bash
   claude setup-token
   ```

3. 在 Web UI 中选择 Anthropic 提供商并登录

### 方案三：注册 OpenAI OAuth 应用（高级）

如果你确实需要使用 OpenAI OAuth：

1. 访问 [OpenAI Platform](https://platform.openai.com/)
2. 创建 OAuth 应用
3. 获取 Client ID 和 Client Secret
4. 配置回调 URL：`http://127.0.0.1:1455/auth/callback`
5. 更新代码中的 Client ID

## 当前配置建议

根据你的 `.env.example` 配置：

```bash
# 使用智谱 AI API Key
OPENAI_API_KEY=0b033df46a31482695111f4ed4cec2b5.Gtvih8m3ONExHPcH
OPENAI_MODEL=glm-4.7
OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4/

# 设置为 API Key 模式
LLM_AUTH_MODE=api_key
```

这样配置后，系统会直接使用智谱 AI 的 API，无需 OAuth 登录。

## OAuth 适用场景

OAuth 主要适用于：
- ✅ 使用 ChatGPT Plus/Pro 订阅的用户
- ✅ 使用 Claude Pro/Max 订阅的用户
- ✅ 想要节省 API 费用的用户
- ❌ 使用第三方 LLM API（如智谱 AI）的用户

## 总结

**对于你的情况**：
- 你使用的是智谱 AI（glm-4.7）
- 建议使用 **API Key 模式**
- 无需配置 OAuth

如果将来需要使用 OpenAI 或 Anthropic 的 OAuth，可以参考上述方案二或方案三。
