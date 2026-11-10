package api

import (
	"context"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
)

// GetSettings tells the web app what the deployment decides, so that it never states its own copy.
func (s *Server) GetSettings(context.Context, GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	return GetSettings200JSONResponse{
		MaxUploadBytes:      s.maxUploadBytes,
		SourceIntervalHours: int(jobs.SourceInterval / time.Hour),
		BaseCurrency:        domain.BaseCurrency,
	}, nil
}
