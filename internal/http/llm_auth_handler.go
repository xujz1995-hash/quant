package httpapi

import (
	"net/http"

	"ai_quant/internal/auth"

	"github.com/gin-gonic/gin"
)

type LLMAuthHandler struct {
	authManager *auth.LLMAuthManager
}

func NewLLMAuthHandler(authManager *auth.LLMAuthManager) *LLMAuthHandler {
	return &LLMAuthHandler{
		authManager: authManager,
	}
}

// getAuthStatus 获取当前认证状态
func (h *LLMAuthHandler) getAuthStatus(c *gin.Context) {
	status := h.authManager.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status": status,
	})
}

// setAuthMode 设置认证模式
func (h *LLMAuthHandler) setAuthMode(c *gin.Context) {
	var req struct {
		Mode string `json:"mode" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mode := auth.AuthMode(req.Mode)
	if mode != auth.AuthModeAPIKey && mode != auth.AuthModeOAuth && mode != auth.AuthModeAuto {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode, must be: api_key, oauth, or auto"})
		return
	}

	h.authManager.SetMode(mode)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"mode":    mode,
		"message": "认证模式已切换",
	})
}

// setAuthProvider 设置 OAuth 提供商
func (h *LLMAuthHandler) setAuthProvider(c *gin.Context) {
	var req struct {
		Provider string `json:"provider" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider := auth.Provider(req.Provider)
	if provider != auth.ProviderOpenAI && provider != auth.ProviderAnthropic {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider, must be: openai or anthropic"})
		return
	}

	h.authManager.SetProvider(provider)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"provider": provider,
		"message":  "OAuth 提供商已切换",
	})
}
