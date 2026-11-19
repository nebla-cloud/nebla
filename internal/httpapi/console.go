package httpapi

import (
	"context"

	"github.com/Hexeption/nebla/internal/apigen"
)

// RequestServerConsole returns only wss_url and password; it is not an Action.
func (s *server) RequestServerConsole(ctx context.Context, req apigen.RequestServerConsoleRequestObject) (apigen.RequestServerConsoleResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.console.Request(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.RequestServerConsole201JSONResponse{WssUrl: g.URL, Password: g.Password}, nil
}
