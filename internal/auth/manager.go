package auth

import "sync"

var (
	globalAuthManager *LLMAuthManager
	managerMu         sync.RWMutex
)

// InitGlobalAuthManager 初始化全局认证管理器
func InitGlobalAuthManager(authService *Service, apiKey string, mode AuthMode, provider Provider) {
	managerMu.Lock()
	defer managerMu.Unlock()
	globalAuthManager = NewLLMAuthManager(authService, apiKey, mode, provider)
}

// GetGlobalAuthManager 获取全局认证管理器
func GetGlobalAuthManager() *LLMAuthManager {
	managerMu.RLock()
	defer managerMu.RUnlock()
	return globalAuthManager
}
