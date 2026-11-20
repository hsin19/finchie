package api

import (
	"context"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
	"github.com/hsin19/finchie/apps/backend/internal/push"
)

// GetSettings tells the web app what the deployment decides, so that it never states its own copy.
func (s *Server) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	keys, err := push.Keys(ctx, dbq.New(s.pool))
	if err != nil {
		return nil, err
	}
	settings := GetSettings200JSONResponse{
		MaxUploadBytes:      s.maxUploadBytes,
		SourceIntervalHours: int(jobs.SourceInterval / time.Hour),
		BaseCurrency:        domain.BaseCurrency,
		LapseSoonDays:       domain.LapseSoonDays,
		PushPublicKey:       keys.PublicKey,
	}
	if s.faroURL != "" {
		settings.FaroUrl = ptr(s.faroURL)
	}
	return settings, nil
}
