package httpapi

import (
	"fmt"
	"log"
	"net/http"

	"ai_quant/internal/auth"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService *auth.Service
}

func NewAuthHandler(authService *auth.Service) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

func (h *AuthHandler) startOAuth(c *gin.Context) {
	provider := auth.Provider(c.Query("provider"))
	if provider == "" {
		provider = auth.ProviderOpenAI
	}

	session, authURL, err := h.authService.StartOAuthFlow(provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[OAuth] Started %s OAuth flow, state=%s", provider, session.State)

	c.JSON(http.StatusOK, gin.H{
		"auth_url": authURL,
		"state":    session.State,
		"provider": provider,
		"message":  "Please visit the auth_url to authorize",
	})
}

func (h *AuthHandler) callback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")
	errorParam := c.Query("error")

	if errorParam != "" {
		errorDesc := c.Query("error_description")
		log.Printf("[OAuth] Callback error: %s - %s", errorParam, errorDesc)
		c.HTML(http.StatusBadRequest, "oauth_error.html", gin.H{
			"error":       errorParam,
			"description": errorDesc,
		})
		return
	}

	if state == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state or code"})
		return
	}

	profile, err := h.authService.HandleCallback(state, code)
	if err != nil {
		log.Printf("[OAuth] Callback failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[OAuth] Successfully authenticated with %s", profile.Provider)

	c.HTML(http.StatusOK, "oauth_success.html", gin.H{
		"provider":   profile.Provider,
		"account_id": profile.AccountID,
		"expires_at": profile.ExpiresAt.Format("2006-01-02 15:04:05"),
	})
}

func (h *AuthHandler) manualCallback(c *gin.Context) {
	var req struct {
		State string `json:"state" binding:"required"`
		Code  string `json:"code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	profile, err := h.authService.HandleCallback(req.State, req.Code)
	if err != nil {
		log.Printf("[OAuth] Manual callback failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[OAuth] Successfully authenticated with %s (manual)", profile.Provider)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"provider":   profile.Provider,
		"account_id": profile.AccountID,
		"expires_at": profile.ExpiresAt,
	})
}

func (h *AuthHandler) listProfiles(c *gin.Context) {
	profiles := h.authService.ListProfiles()

	result := make([]gin.H, 0, len(profiles))
	for _, p := range profiles {
		result = append(result, gin.H{
			"provider":   p.Provider,
			"account_id": p.AccountID,
			"expires_at": p.ExpiresAt,
			"created_at": p.CreatedAt,
			"updated_at": p.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"profiles": result,
		"count":    len(result),
	})
}

func (h *AuthHandler) getProfile(c *gin.Context) {
	provider := auth.Provider(c.Param("provider"))

	profile, err := h.authService.GetProfile(provider)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"provider":   profile.Provider,
		"account_id": profile.AccountID,
		"expires_at": profile.ExpiresAt,
		"created_at": profile.CreatedAt,
		"updated_at": profile.UpdatedAt,
	})
}

func (h *AuthHandler) deleteProfile(c *gin.Context) {
	provider := auth.Provider(c.Param("provider"))

	if err := h.authService.DeleteProfile(provider); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[OAuth] Deleted profile for %s", provider)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Profile for %s deleted", provider),
	})
}

func (h *AuthHandler) refreshToken(c *gin.Context) {
	provider := auth.Provider(c.Param("provider"))

	profile, err := h.authService.RefreshToken(provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[OAuth] Refreshed token for %s", provider)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"provider":   profile.Provider,
		"expires_at": profile.ExpiresAt,
	})
}

func (h *AuthHandler) getToken(c *gin.Context) {
	provider := auth.Provider(c.Param("provider"))

	token, err := h.authService.GetValidToken(provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": token,
		"provider":     provider,
	})
}
