package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/stretchr/testify/require"
)

func TestResponseErrorMatchesPointerAndWrapped(t *testing.T) {
	s := &server{log: nil}
	var ptr error = &apierr.E{Status: http.StatusConflict, Code: "dup", Msg: "x"}
	for _, err := range []error{
		apierr.New(http.StatusConflict, "dup", "x"),
		ptr,
		fmt.Errorf("wrap: %w", ptr),
	} {
		w := httptest.NewRecorder()
		s.responseError(w, nil, err)
		require.Equal(t, http.StatusConflict, w.Code)
	}
}
