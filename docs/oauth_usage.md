# OAuth 使用指南

## 快速开始

### 1. 启动服务

```bash
# 确保已配置 .env 文件
go run main.go
```

服务启动后会看到：
```
🔐 OAuth 服务已启动
AI Quant 服务启动 地址=:8080 模式=spot 模拟=true
```

### 2. 访问 OAuth 管理页面

打开浏览器访问：
```
http://localhost:8080/static/oauth.html
```

### 3. 登录 OpenAI 账号

1. 点击 **"🤖 使用 OpenAI 账号登录"** 按钮
2. 系统会打开新窗口跳转到 OpenAI 授权页面
3. 在 OpenAI 页面完成登录和授权
4. 授权成功后会自动跳转回回调页面
5. 返回原页面，点击 **"🔄 刷新已登录账号"** 查看登录状态

### 4. 登录 Anthropic 账号

1. 点击 **"🦾 使用 Anthropic 账号登录"** 按钮
2. 在新窗口完成 Claude 账号授权
3. 授权完成后返回查看

## API 使用示例

### 使用 curl 测试

#### 1. 启动 OAuth 流程

```bash
curl -X GET "http://localhost:8080/auth/start?provider=openai"
```

响应：
```json
{
  "auth_url": "https://auth.openai.com/oauth/authorize?...",
  "state": "abc123...",
  "provider": "openai",
  "message": "Please visit the auth_url to authorize"
}
```

#### 2. 手动提交授权码（如果自动回调失败）

```bash
curl -X POST "http://localhost:8080/auth/callback/manual" \
  -H "Content-Type: application/json" \
  -d '{
    "state": "abc123...",
    "code": "authorization_code_from_callback"
  }'
```

#### 3. 查看所有已登录账号

```bash
curl -X GET "http://localhost:8080/auth/profiles"
```

响应：
```json
{
  "profiles": [
    {
      "provider": "openai",
      "account_id": "user-xxx",
      "expires_at": "2026-02-16T12:00:00Z",
      "created_at": "2026-02-15T12:00:00Z",
      "updated_at": "2026-02-15T12:00:00Z"
    }
  ],
  "count": 1
}
```

#### 4. 获取有效的 Access Token

```bash
curl -X GET "http://localhost:8080/auth/profiles/openai/token"
```

响应：
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "provider": "openai"
}
```

#### 5. 刷新 Token

```bash
curl -X POST "http://localhost:8080/auth/profiles/openai/refresh"
```

#### 6. 删除账号

```bash
curl -X DELETE "http://localhost:8080/auth/profiles/openai"
```

## 在代码中使用 OAuth Token

### 示例：使用 OpenAI Token 调用 API

```go
package main

import (
    "ai_quant/internal/auth"
    "fmt"
    "net/http"
)

func main() {
    // 初始化 OAuth 服务
    authService, err := auth.NewService("")
    if err != nil {
        panic(err)
    }

    // 获取有效的 access token（自动刷新）
    token, err := authService.GetValidToken(auth.ProviderOpenAI)
    if err != nil {
        panic(err)
    }

    // 使用 token 调用 OpenAI API
    req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.Status)
}
```

### 示例：检查 Token 是否过期

```go
profile, err := authService.GetProfile(auth.ProviderOpenAI)
if err != nil {
    // 未登录
    fmt.Println("请先登录 OpenAI 账号")
    return
}

if time.Now().After(profile.ExpiresAt) {
    // Token 已过期，尝试刷新
    profile, err = authService.RefreshToken(auth.ProviderOpenAI)
    if err != nil {
        fmt.Println("Token 刷新失败，请重新登录")
        return
    }
}

fmt.Println("Access Token:", profile.AccessToken)
```

## 存储文件位置

OAuth Token 存储在：
```
~/.ai_quant/auth-profiles.json
```

文件格式：
```json
{
  "profiles": {
    "openai": {
      "provider": "openai",
      "access_token": "eyJhbGci...",
      "refresh_token": "refresh_token_xxx",
      "expires_at": "2026-02-16T12:00:00Z",
      "account_id": "user-xxx",
      "created_at": "2026-02-15T12:00:00Z",
      "updated_at": "2026-02-15T12:00:00Z"
    }
  },
  "updated_at": "2026-02-15T12:00:00Z"
}
```

## 安全建议

1. **保护存储文件**：`auth-profiles.json` 包含敏感信息，确保文件权限为 0600
2. **不要提交到 Git**：已在 `.gitignore` 中排除
3. **定期刷新 Token**：系统会在过期前 5 分钟自动刷新
4. **使用 HTTPS**：生产环境必须使用 HTTPS
5. **备份 Refresh Token**：如果丢失需要重新登录

## 故障排查

### 问题 1：回调地址无法访问

**症状**：点击登录后，回调页面显示 "无法访问此网站"

**解决方案**：
- 检查端口 1455 是否被占用
- 使用手动回调方式：复制浏览器地址栏的完整 URL，提取 `code` 和 `state` 参数
- 使用 API 手动提交：`POST /auth/callback/manual`

### 问题 2：Token 刷新失败

**症状**：`OAuth token refresh failed`

**原因**：
- Refresh token 已过期或失效
- 在其他地方（如 Claude CLI）登录导致 token 失效

**解决方案**：
- 删除旧的 profile：`DELETE /auth/profiles/:provider`
- 重新登录

### 问题 3：State 参数不匹配

**症状**：`invalid or expired state`

**原因**：
- Session 已过期（超过 10 分钟）
- 使用了错误的 state 参数

**解决方案**：
- 重新启动 OAuth 流程
- 确保在 10 分钟内完成授权

## 高级配置

### 自定义存储路径

在 `.env` 文件中设置：
```bash
OAUTH_STORAGE_PATH=/custom/path/to/auth-profiles.json
```

### 自定义回调端口

修改 `internal/auth/oauth.go` 中的 `GetDefaultConfig` 函数：
```go
RedirectURI: "http://127.0.0.1:YOUR_PORT/auth/callback",
```

### 添加新的 OAuth 提供商

1. 在 `oauth.go` 中添加新的 Provider 常量
2. 在 `GetDefaultConfig` 中添加配置
3. 更新前端页面添加登录按钮

## 参考链接

- [OpenClaw OAuth 实现](https://docs.openclaw.ai/concepts/oauth)
- [OAuth 2.0 PKCE 规范](https://tools.ietf.org/html/rfc7636)
- [OpenAI OAuth 文档](https://platform.openai.com/docs/guides/oauth)
- [Anthropic API 文档](https://docs.anthropic.com/)
