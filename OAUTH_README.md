# OAuth 登录功能说明

本项目参考 OpenClaw 的实现方式，集成了 Codex OAuth 登录功能，支持 OpenAI 和 Anthropic 两个提供商。

## 功能特性

- ✅ **PKCE 流程**: 使用 OAuth 2.0 PKCE (Proof Key for Code Exchange) 标准，提高安全性
- ✅ **多提供商支持**: 支持 OpenAI (ChatGPT/Codex) 和 Anthropic (Claude)
- ✅ **Token 管理**: 自动存储和刷新 access token
- ✅ **本地存储**: Token 安全存储在本地文件系统 (`~/.ai_quant/auth-profiles.json`)
- ✅ **Web UI**: 提供友好的 Web 界面进行 OAuth 登录和管理

## 架构设计

参考 OpenClaw 的设计理念：

```
┌─────────────────┐
│   Web UI        │  用户界面
└────────┬────────┘
         │
┌────────▼────────┐
│  HTTP Handler   │  /auth/* 路由
└────────┬────────┘
         │
┌────────▼────────┐
│  Auth Service   │  OAuth 流程管理
└────────┬────────┘
         │
┌────────▼────────┐
│ Profile Store   │  Token 持久化存储
└─────────────────┘
```

## OAuth 流程

### OpenAI Codex OAuth 流程

1. **生成 PKCE 参数**
   - 生成随机 code_verifier (32 字节)
   - 计算 code_challenge = SHA256(verifier)
   - 生成随机 state (16 字节)

2. **打开授权 URL**
   ```
   https://auth.openai.com/oauth/authorize?
     client_id=openclaw-codex
     &response_type=code
     &redirect_uri=http://127.0.0.1:1455/auth/callback
     &state={random_state}
     &code_challenge={challenge}
     &code_challenge_method=S256
     &scope=openid profile email offline_access
   ```

3. **捕获回调**
   - 自动捕获: `http://127.0.0.1:1455/auth/callback?code=xxx&state=xxx`
   - 手动粘贴: 如果无法自动捕获，用户可手动粘贴回调 URL

4. **交换 Token**
   ```
   POST https://auth.openai.com/oauth/token
   {
     grant_type: "authorization_code",
     code: "{authorization_code}",
     redirect_uri: "http://127.0.0.1:1455/auth/callback",
     client_id: "openclaw-codex",
     code_verifier: "{verifier}"
   }
   ```

5. **存储 Token**
   - 提取 accountId (如果可用)
   - 存储 { access_token, refresh_token, expires_at, account_id }

### Anthropic Setup-Token 流程

Anthropic 支持类似的 OAuth 流程，也可以使用 `claude setup-token` 生成的 token。

## API 端点

### 启动 OAuth 流程
```bash
GET /auth/start?provider=openai
# 返回: { auth_url, state, provider }
```

### OAuth 回调
```bash
GET /auth/callback?code=xxx&state=xxx
# 自动处理并显示结果页面
```

### 手动提交回调
```bash
POST /auth/callback/manual
{
  "state": "xxx",
  "code": "xxx"
}
```

### 列出所有已登录账号
```bash
GET /auth/profiles
# 返回: { profiles: [...], count: N }
```

### 获取特定账号信息
```bash
GET /auth/profiles/:provider
# 返回: { provider, account_id, expires_at, ... }
```

### 刷新 Token
```bash
POST /auth/profiles/:provider/refresh
# 返回: { success: true, expires_at: ... }
```

### 删除账号
```bash
DELETE /auth/profiles/:provider
# 返回: { success: true }
```

### 获取有效 Token
```bash
GET /auth/profiles/:provider/token
# 返回: { access_token, provider }
```

## 使用方法

### 1. 配置环境变量

在 `.env` 文件中添加（可选）：

```bash
# OAuth 存储路径（默认: ~/.ai_quant/auth-profiles.json）
OAUTH_STORAGE_PATH=
```

### 2. 启动服务

```bash
go run main.go
```

### 3. 访问 OAuth 页面

打开浏览器访问：
```
http://localhost:8080/static/oauth.html
```

### 4. 登录流程

1. 点击 "使用 OpenAI 账号登录" 或 "使用 Anthropic 账号登录"
2. 在新窗口中完成授权
3. 授权成功后，返回原页面查看已登录账号
4. 可以刷新 token 或删除账号

## 代码结构

```
internal/auth/
├── pkce.go          # PKCE 参数生成
├── oauth.go         # OAuth 配置和流程
├── storage.go       # Token 存储管理
└── service.go       # OAuth 服务封装

internal/http/
├── auth_handler.go  # OAuth HTTP 处理器
└── server.go        # 路由配置

client/
└── oauth.html       # OAuth Web UI
```

## 安全性

- ✅ 使用 PKCE 防止授权码拦截攻击
- ✅ State 参数防止 CSRF 攻击
- ✅ Token 文件权限设置为 0600 (仅所有者可读写)
- ✅ Session 自动过期 (10 分钟)
- ✅ Token 自动刷新机制

## 参考资料

- [OpenClaw OAuth 文档](https://docs.openclaw.ai/concepts/oauth)
- [OpenClaw Anthropic 配置](https://docs.openclaw.ai/providers/anthropic)
- [OAuth 2.0 PKCE RFC 7636](https://tools.ietf.org/html/rfc7636)

## 注意事项

1. **本地开发**: 回调地址使用 `http://127.0.0.1:1455`，确保端口未被占用
2. **生产环境**: 需要配置正确的回调 URL 和 HTTPS
3. **Token 刷新**: Access token 过期前 5 分钟会自动刷新
4. **多账号**: 支持同时登录多个提供商的账号
5. **手动回调**: 如果自动回调失败，可以使用手动粘贴 code 的方式

## 故障排查

### 1. 回调失败
- 检查端口 1455 是否被占用
- 确认回调 URL 配置正确
- 使用手动回调方式

### 2. Token 刷新失败
- 检查 refresh_token 是否有效
- 某些提供商可能会使旧的 refresh_token 失效
- 重新登录获取新的 token

### 3. 存储文件权限
- 确保 `~/.ai_quant/` 目录有写权限
- 检查 `auth-profiles.json` 文件权限为 0600

## 未来改进

- [ ] 支持更多 OAuth 提供商
- [ ] 实现 Token 加密存储
- [ ] 添加 OAuth 状态监控
- [ ] 支持企业版 OAuth (Corporate tokens)
- [ ] 实现 Token 使用统计
