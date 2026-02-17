package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ProfileStore struct {
	mu       sync.RWMutex
	profiles map[Provider]*AuthProfile
	filePath string
}

type profilesFile struct {
	Profiles map[Provider]*AuthProfile `json:"profiles"`
	UpdatedAt time.Time                `json:"updated_at"`
}

// NewProfileStore creates a new profile store
func NewProfileStore(storagePath string) (*ProfileStore, error) {
	if storagePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		storagePath = filepath.Join(home, ".ai_quant", "auth-profiles.json")
	}

	if err := os.MkdirAll(filepath.Dir(storagePath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	store := &ProfileStore{
		profiles: make(map[Provider]*AuthProfile),
		filePath: storagePath,
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load profiles: %w", err)
	}

	return store, nil
}

// SaveProfile saves an auth profile
func (s *ProfileStore) SaveProfile(profile *AuthProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile.UpdatedAt = time.Now()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now()
	}

	s.profiles[profile.Provider] = profile
	return s.persist()
}

// GetProfile retrieves an auth profile by provider
func (s *ProfileStore) GetProfile(provider Provider) (*AuthProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, exists := s.profiles[provider]
	if !exists {
		return nil, fmt.Errorf("no profile found for provider: %s", provider)
	}

	return profile, nil
}

// DeleteProfile removes an auth profile
func (s *ProfileStore) DeleteProfile(provider Provider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.profiles, provider)
	return s.persist()
}

// ListProfiles returns all stored profiles
func (s *ProfileStore) ListProfiles() []*AuthProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profiles := make([]*AuthProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		profiles = append(profiles, p)
	}
	return profiles
}

// IsExpired checks if a profile's access token is expired
func (s *ProfileStore) IsExpired(provider Provider) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, exists := s.profiles[provider]
	if !exists {
		return true
	}

	return time.Now().After(profile.ExpiresAt)
}

func (s *ProfileStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var pf profilesFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("failed to parse profiles file: %w", err)
	}

	s.profiles = pf.Profiles
	if s.profiles == nil {
		s.profiles = make(map[Provider]*AuthProfile)
	}

	return nil
}

func (s *ProfileStore) persist() error {
	pf := profilesFile{
		Profiles:  s.profiles,
		UpdatedAt: time.Now(),
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write profiles file: %w", err)
	}

	return nil
}
