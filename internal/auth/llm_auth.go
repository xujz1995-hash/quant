package auth

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// AuthMode 认证模式
type AuthMode string

const (
	AuthModeAPIKey AuthMode = "api_key" // 使用 API Key
	AuthModeOAuth  AuthMode = "oauth"   // 使用 OAuth Token
	AuthModeAuto   AuthMode = "auto"    // 自动选择（优先 OAuth）
)

// LLMAuthManager LLM 认证管理器
type LLMAuthManager struct {
	authService *Service
	apiKey      string
	mode        AuthMode
	provider    Provider
	mu          sync.RWMutex
}

// NewLLMAuthManager 创建 LLM 认证管理器
func NewLLMAuthManager(authService *Service, apiKey string, mode AuthMode, provider Provider) *LLMAuthManager {
	if mode == "" {
		mode = AuthModeAuto
	}
	if provider == "" {
		provider = ProviderOpenAI
	}

	return &LLMAuthManager{
		authService: authService,
		apiKey:      apiKey,
		mode:        mode,
		provider:    provider,
	}
}

// GetToken 获取认证 token（根据模式自动选择）
func (m *LLMAuthManager) GetToken() (string, error) {
	m.mu.RLock()
	mode := m.mode
	m.mu.RUnlock()

	switch mode {
	case AuthModeAPIKey:
		return m.getAPIKey()
	case AuthModeOAuth:
		return m.getOAuthToken()
	case AuthModeAuto:
		return m.getAutoToken()
	default:
		return "", fmt.Errorf("unsupported auth mode: %s", mode)
	}
}

// SetMode 设置认证模式
func (m *LLMAuthManager) SetMode(mode AuthMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
	log.Printf("[LLM Auth] 认证模式已切换为: %s", mode)
}

// GetMode 获取当前认证模式
func (m *LLMAuthManager) GetMode() AuthMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

// SetProvider 设置 OAuth 提供商
func (m *LLMAuthManager) SetProvider(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = provider
	log.Printf("[LLM Auth] OAuth 提供商已切换为: %s", provider)
}

// GetProvider 获取当前 OAuth 提供商
func (m *LLMAuthManager) GetProvider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

// GetStatus 获取认证状态
func (m *LLMAuthManager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := map[string]interface{}{
		"mode":     m.mode,
		"provider": m.provider,
		"api_key":  m.apiKey != "",
	}

	// 检查 OAuth 状态
	if m.authService != nil {
		profile, err := m.authService.GetProfile(m.provider)
		if err == nil {
			status["oauth_available"] = true
			status["oauth_expires_at"] = profile.ExpiresAt
			status["oauth_account_id"] = profile.AccountID
		} else {
			status["oauth_available"] = false
		}
	}

	return status
}

func (m *LLMAuthManager) getAPIKey() (string, error) {
	if strings.TrimSpace(m.apiKey) == "" {
		return "", fmt.Errorf("API Key 未配置")
	}
	log.Printf("[LLM Auth] 使用 API Key 认证")
	return m.apiKey, nil
}

func (m *LLMAuthManager) getOAuthToken() (string, error) {
	if m.authService == nil {
		return "", fmt.Errorf("OAuth 服务未初始化")
	}

	token, err := m.authService.GetValidToken(m.provider)
	if err != nil {
		return "", fmt.Errorf("获取 OAuth token 失败: %w", err)
	}

	log.Printf("[LLM Auth] 使用 OAuth Token 认证 (provider=%s)", m.provider)
	return token, nil
}

func (m *LLMAuthManager) getAutoToken() (string, error) {
	// 优先尝试 OAuth
	if m.authService != nil {
		token, err := m.authService.GetValidToken(m.provider)
		if err == nil {
			log.Printf("[LLM Auth] 自动模式: 使用 OAuth Token (provider=%s)", m.provider)
			return token, nil
		}
		log.Printf("[LLM Auth] OAuth 不可用: %v，尝试使用 API Key", err)
	}

	// 降级到 API Key
	if strings.TrimSpace(m.apiKey) != "" {
		log.Printf("[LLM Auth] 自动模式: 使用 API Key")
		return m.apiKey, nil
	}

	return "", fmt.Errorf("无可用的认证方式（OAuth 和 API Key 均不可用）")
}
