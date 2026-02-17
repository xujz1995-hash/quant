# 如何获取 OAuth Client ID

## 重要提示 ⚠️

**对于你的情况（使用智谱 AI），不需要申请 Client ID！**

你只需要：
1. 在 `.env` 中设置 `LLM_AUTH_MODE=api_key`
2. 使用现有的智谱 AI API Key

OAuth 只适用于想使用 ChatGPT Plus/Claude Pro 订阅的用户。

---

## 如果你确实需要 OAuth（仅供参考）

### OpenAI OAuth Client ID 申请

#### 方法一：使用 OpenAI Platform（官方）

1. **访问 OpenAI Platform**
   - 网址：https://platform.openai.com/
   - 登录你的 OpenAI 账号

2. **创建 OAuth 应用**
   - 目前 OpenAI 的 OAuth 主要面向企业用户
   - 个人用户可能需要联系 OpenAI 支持

3. **配置应用**
   - **应用名称**：AI Quant Trading Bot
   - **回调 URL**：`http://127.0.0.1:1455/auth/callback`
   - **权限范围**：`openid`, `profile`, `email`, `offline_access`

4. **获取凭证**
   - Client ID：类似 `abc123xyz...`
   - Client Secret：类似 `secret_abc123...`

5. **更新代码**
   ```go
   // 在 internal/auth/oauth.go 中
   case ProviderOpenAI:
       return &OAuthConfig{
           Provider:     ProviderOpenAI,
           ClientID:     "你的_CLIENT_ID",      // 替换这里
           ClientSecret: "你的_CLIENT_SECRET",  // 添加这行
           AuthURL:      "https://auth.openai.com/oauth/authorize",
           TokenURL:     "https://auth.openai.com/oauth/token",
           RedirectURI:  "http://127.0.0.1:1455/auth/callback",
           Scopes:       []string{"openid", "profile", "email", "offline_access"},
       }
   ```

#### 方法二：使用第三方 OAuth 服务（不推荐）

某些第三方服务提供 OpenAI OAuth 代理，但存在安全风险，不建议使用。

---

### Anthropic (Claude) OAuth - 更简单！

Anthropic 提供了更简单的方式，不需要注册应用：

#### 使用 Claude CLI

1. **安装 Claude CLI**
   ```bash
   npm install -g @anthropic-ai/claude-cli
   ```

2. **生成 Setup Token**
   ```bash
   claude setup-token
   ```
   
   这会：
   - 打开浏览器进行授权
   - 生成一个长期有效的 token
   - 自动保存到本地

3. **在系统中使用**
   - 访问 `http://localhost:8080/static/oauth.html`
   - 选择 Anthropic 提供商
   - 点击登录按钮
   - 完成授权

#### 手动粘贴 Token

如果自动回调失败，可以：
1. 复制 `claude setup-token` 生成的 token
2. 使用 API 手动提交：
   ```bash
   curl -X POST http://localhost:8080/auth/callback/manual \
     -H "Content-Type: application/json" \
     -d '{
       "state": "your_state",
       "code": "your_token"
     }'
   ```

---

## 为什么 OpenAI OAuth 这么复杂？

1. **安全考虑**
   - OAuth 需要严格的应用审核
   - 防止滥用和钓鱼攻击

2. **企业导向**
   - OpenAI OAuth 主要面向企业客户
   - 个人用户建议使用 API Key

3. **订阅验证**
   - 需要验证 ChatGPT Plus/Pro 订阅
   - 确保只有付费用户可以使用

---

## 推荐方案对比

### 方案一：API Key（推荐给你）✅

**优点**：
- ✅ 立即可用，无需申请
- ✅ 配置简单
- ✅ 适用于所有 LLM 提供商
- ✅ 你已经有智谱 AI 的 API Key

**缺点**：
- ❌ 需要付费购买 API 额度

**配置**：
```bash
LLM_AUTH_MODE=api_key
OPENAI_API_KEY=你的密钥
```

### 方案二：Anthropic OAuth（如果你有 Claude Pro）

**优点**：
- ✅ 使用订阅额度，节省费用
- ✅ 申请简单（claude setup-token）
- ✅ 长期有效

**缺点**：
- ❌ 需要 Claude Pro/Max 订阅
- ❌ 仅限 Anthropic 模型

**配置**：
```bash
LLM_AUTH_MODE=oauth
LLM_AUTH_PROVIDER=anthropic
```

### 方案三：OpenAI OAuth（最复杂）

**优点**：
- ✅ 使用 ChatGPT Plus 订阅额度
- ✅ 节省 API 费用

**缺点**：
- ❌ 申请复杂，可能需要企业账号
- ❌ 需要 ChatGPT Plus/Pro 订阅
- ❌ 配置繁琐

---

## 你的最佳选择

根据你的配置（智谱 AI glm-4.7）：

### 立即可用的方案

在 `.env` 文件中添加：
```bash
# 使用 API Key 模式（推荐）
LLM_AUTH_MODE=api_key
LLM_AUTH_PROVIDER=openai

# 你的智谱 AI 配置（已有）
OPENAI_API_KEY=0b033df46a31482695111f4ed4cec2b5.Gtvih8m3ONExHPcH
OPENAI_MODEL=glm-4.7
OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4/
```

### 如果将来想用 OAuth

**选项 1：切换到 Anthropic**
- 注册 Claude Pro 订阅（$20/月）
- 使用 `claude setup-token` 获取 token
- 设置 `LLM_AUTH_MODE=oauth`

**选项 2：切换到 OpenAI**
- 注册 ChatGPT Plus（$20/月）
- 联系 OpenAI 申请 OAuth Client ID
- 等待审核通过

**选项 3：继续使用智谱 AI**
- 保持当前配置
- 使用 API Key 模式
- 最简单、最稳定

---

## 常见问题

### Q: 我可以使用别人的 Client ID 吗？

**A**: 不可以。每个 Client ID 都绑定到特定的回调 URL 和应用。使用别人的 Client ID 会导致：
- 认证失败
- 安全风险
- 违反服务条款

### Q: OpenClaw 的 Client ID 为什么不能用？

**A**: OpenClaw 的 Client ID 是为 OpenClaw 项目注册的，只能在 OpenClaw 的回调地址使用。我们的项目使用不同的回调地址，所以无法使用。

### Q: 有免费的 OAuth 方案吗？

**A**: 没有。OAuth 需要：
- ChatGPT Plus/Pro 订阅（$20-$200/月）
- 或 Claude Pro/Max 订阅（$20-$200/月）

如果预算有限，建议使用 API Key 按需付费。

### Q: 智谱 AI 支持 OAuth 吗？

**A**: 不支持。智谱 AI 只支持 API Key 认证。

---

## 总结

**对于你的情况**：
1. ✅ 不需要申请 Client ID
2. ✅ 使用 `LLM_AUTH_MODE=api_key`
3. ✅ 继续使用智谱 AI
4. ✅ 系统已经可以正常工作

**如果将来需要 OAuth**：
- 推荐 Anthropic（简单）
- OpenAI 需要企业账号（复杂）
- 两者都需要付费订阅

现在最重要的是：**在 .env 中添加 `LLM_AUTH_MODE=api_key`**，就可以避免 OAuth 错误了！
