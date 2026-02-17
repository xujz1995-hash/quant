package auth

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type Service struct {
	store    *ProfileStore
	sessions map[string]*OAuthSession
	mu       sync.RWMutex
}

func NewService(storagePath string) (*Service, error) {
	store, err := NewProfileStore(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile store: %w", err)
	}

	return &Service{
		store:    store,
		sessions: make(map[string]*OAuthSession),
	}, nil
}

// StartOAuthFlow initiates an OAuth flow for a provider
func (s *Service) StartOAuthFlow(provider Provider) (*OAuthSession, string, error) {
	config := GetDefaultConfig(provider)
	if config == nil {
		return nil, "", fmt.Errorf("unsupported provider: %s", provider)
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate verifier: %w", err)
	}

	challenge := GenerateCodeChallenge(verifier)

	state, err := GenerateState()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate state: %w", err)
	}

	session := &OAuthSession{
		State:       state,
		Verifier:    verifier,
		Challenge:   challenge,
		Provider:    provider,
		CreatedAt:   time.Now(),
		RedirectURI: config.RedirectURI,
	}

	s.mu.Lock()
	s.sessions[state] = session
	s.mu.Unlock()

	go s.cleanupExpiredSessions()

	authURL := config.GenerateAuthURL(state, challenge)
	return session, authURL, nil
}

// HandleCallback processes the OAuth callback
func (s *Service) HandleCallback(state, code string) (*AuthProfile, error) {
	s.mu.RLock()
	session, exists := s.sessions[state]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid or expired state")
	}

	if time.Since(session.CreatedAt) > 10*time.Minute {
		s.mu.Lock()
		delete(s.sessions, state)
		s.mu.Unlock()
		return nil, fmt.Errorf("session expired")
	}

	config := GetDefaultConfig(session.Provider)
	if config == nil {
		return nil, fmt.Errorf("unsupported provider: %s", session.Provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenResp, err := config.ExchangeCode(ctx, code, session.Verifier)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	profile := &AuthProfile{
		Provider:     session.Provider,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if session.Provider == ProviderOpenAI {
		accountID, err := extractAccountIDFromToken(tokenResp.AccessToken)
		if err != nil {
			log.Printf("Warning: failed to extract account ID: %v", err)
		} else {
			profile.AccountID = accountID
		}
	}

	if err := s.store.SaveProfile(profile); err != nil {
		return nil, fmt.Errorf("failed to save profile: %w", err)
	}

	s.mu.Lock()
	delete(s.sessions, state)
	s.mu.Unlock()

	return profile, nil
}

// GetProfile retrieves a stored auth profile
func (s *Service) GetProfile(provider Provider) (*AuthProfile, error) {
	return s.store.GetProfile(provider)
}

// RefreshToken refreshes an expired access token
func (s *Service) RefreshToken(provider Provider) (*AuthProfile, error) {
	profile, err := s.store.GetProfile(provider)
	if err != nil {
		return nil, err
	}

	if profile.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	config := GetDefaultConfig(provider)
	if config == nil {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenResp, err := config.RefreshAccessToken(ctx, profile.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	profile.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		profile.RefreshToken = tokenResp.RefreshToken
	}
	profile.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	profile.UpdatedAt = time.Now()

	if err := s.store.SaveProfile(profile); err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	return profile, nil
}

// DeleteProfile removes an auth profile
func (s *Service) DeleteProfile(provider Provider) error {
	return s.store.DeleteProfile(provider)
}

// ListProfiles returns all stored profiles
func (s *Service) ListProfiles() []*AuthProfile {
	return s.store.ListProfiles()
}

// GetValidToken returns a valid access token, refreshing if necessary
func (s *Service) GetValidToken(provider Provider) (string, error) {
	profile, err := s.store.GetProfile(provider)
	if err != nil {
		return "", err
	}

	if time.Now().Before(profile.ExpiresAt.Add(-5 * time.Minute)) {
		return profile.AccessToken, nil
	}

	refreshedProfile, err := s.RefreshToken(provider)
	if err != nil {
		return "", fmt.Errorf("token expired and refresh failed: %w", err)
	}

	return refreshedProfile.AccessToken, nil
}

func (s *Service) cleanupExpiredSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for state, session := range s.sessions {
		if now.Sub(session.CreatedAt) > 10*time.Minute {
			delete(s.sessions, state)
		}
	}
}

func extractAccountIDFromToken(accessToken string) (string, error) {
	return "", nil
}
