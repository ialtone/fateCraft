package campaign

import "github.com/ialtone/fatecraft/internal/storage"

// RegisterInitializer installs a trusted adapter that runs within the same
// transaction as the new Campaign row. The repository does not choose rules.
func (s *Service) RegisterInitializer(key string, initialize storage.CampaignInitializer) {
	if !validStableKey(key) || initialize == nil {
		panic("invalid campaign initializer")
	}
	if s.initializers == nil {
		s.initializers = map[string]storage.CampaignInitializer{}
	}
	if _, exists := s.initializers[key]; exists {
		panic("duplicate campaign initializer: " + key)
	}
	s.initializers[key] = initialize
}
