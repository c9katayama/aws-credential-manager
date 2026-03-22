package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/yaman/aws-credential-manager/core-go/internal/generator"
	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
)

const refreshThreshold = 10 * time.Minute

type Service struct {
	store     *metadata.Store
	generator *generator.Service
	interval  time.Duration
}

func New(store *metadata.Store, generator *generator.Service, interval time.Duration) *Service {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Service{
		store:     store,
		generator: generator,
		interval:  interval,
	}
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	index, err := s.store.Load()
	if err != nil {
		log.Printf("scheduler: failed to load metadata: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, config := range index.Configs {
		if !config.AutoRefreshEnabled {
			continue
		}
		if !shouldRefresh(now, config.LastKnownExpiration) {
			continue
		}

		configID := config.ID
		go func() {
			generateCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			if _, err := s.generator.Generate(generateCtx, configID); err != nil {
				log.Printf("scheduler: auto-refresh failed for %s: %v", configID, err)
			}
		}()
	}
}

func shouldRefresh(now time.Time, expiration *time.Time) bool {
	if expiration == nil {
		return false
	}
	return expiration.Before(now.Add(refreshThreshold))
}
