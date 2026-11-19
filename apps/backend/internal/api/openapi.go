package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	contract "github.com/hsin19/finchie/apps/backend/api"
)

// serveContract answers getOpenAPI, which the generator leaves out (api/oapi-codegen.yaml): it writes
// the JSON as converted, keys in openapi.yaml's order, where a generated handler would encode a map.
func serveContract(c *echo.Context) error {
	document, err := contract.JSON()
	if err != nil {
		return err
	}
	if err := c.Blob(http.StatusOK, echo.MIMEApplicationJSON, document); err != nil {
		return fmt.Errorf("write the contract: %w", err)
	}
	return nil
}
