package api

import (
	"bytes"
	"context"

	"github.com/hsin19/finchie/apps/backend/internal/mailfilter"
)

func (s *Server) GetMailFilters(_ context.Context, _ GetMailFiltersRequestObject) (GetMailFiltersResponseObject, error) {
	if s.mailboxAddress == "" {
		return GetMailFilters404JSONResponse{ProblemJSONResponse{Message: "no mailbox address: set BACKEND_MAILBOX_ADDRESS"}}, nil
	}
	filter := mailfilter.For(s.mailboxAddress)
	disposition := `attachment; filename="finchie-mail-filters.xml"`
	return GetMailFilters200ApplicationxmlResponse{
		Body:          bytes.NewReader(filter),
		ContentLength: int64(len(filter)),
		Headers:       GetMailFilters200ResponseHeaders{ContentDisposition: &disposition},
	}, nil
}
