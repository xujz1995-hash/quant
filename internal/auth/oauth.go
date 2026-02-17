package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

type OAuthConfig struct {
	Provider     Provider
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	RedirectURI  string
	Scopes       []string
}

type OAuthSession struct {
	State        string
	Verifier     string
	Challenge    string
	Provider     Provider
	CreatedAt    time.Time
	RedirectURI  string
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

type AuthProfile struct {
	Provider     Provider  `json:"provider"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountID    string    `json:"account_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GetDefaultConfig returns default OAuth config for supported providers
func GetDefaultConfig(provider Provider) *OAuthConfig {
	switch provider {
	case ProviderOpenAI:
		return &OAuthConfig{
			Provider:    ProviderOpenAI,
			ClientID:    "openclaw-codex",
			AuthURL:     "https://auth.openai.com/oauth/authorize",
			TokenURL:    "https://auth.openai.com/oauth/token",
			RedirectURI: "http://127.0.0.1:1455/auth/callback",
			Scopes:      []string{"openid", "profile", "email", "offline_access"},
		}
	case ProviderAnthropic:
		return &OAuthConfig{
			Provider:    ProviderAnthropic,
			ClientID:    "openclaw-anthropic",
			AuthURL:     "https://api.anthropic.com/oauth/authorize",
			TokenURL:    "https://api.anthropic.com/oauth/token",
			RedirectURI: "http://127.0.0.1:1455/auth/callback",
			Scopes:      []string{"user:inference", "user:profile"},
		}
	default:
		return nil
	}
}

// GenerateAuthURL creates the OAuth authorization URL
func (c *OAuthConfig) GenerateAuthURL(state, challenge string) string {
	params := url.Values{}
	params.Set("client_id", c.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", c.RedirectURI)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("scope", strings.Join(c.Scopes, " "))
	
	return fmt.Sprintf("%s?%s", c.AuthURL, params.Encode())
}

// ExchangeCode exchanges authorization code for tokens
func (c *OAuthConfig) ExchangeCode(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", c.RedirectURI)
	data.Set("client_id", c.ClientID)
	data.Set("code_verifier", verifier)
	
	if c.ClientSecret != "" {
		data.Set("client_secret", c.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshAccessToken refreshes an expired access token
func (c *OAuthConfig) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", c.ClientID)
	
	if c.ClientSecret != "" {
		data.Set("client_secret", c.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}
