package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestEndpointChangeRequiresSecret(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	in := testutil.LocationInput(pv, "eu-1")
	loc, err := svc.CreateLocation(ctx, in)
	require.NoError(t, err)

	var hits atomic.Int32
	atk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(500)
	}))
	defer atk.Close()

	up := testutil.LocationInput(pv, "eu-1")
	up.PVE.URL = atk.URL
	up.PVE.TokenSecret = nil
	_, err = svc.UpdateLocation(ctx, loc.ID, up)
	requireCode(t, err, 422, "invalid_input")
	require.Zero(t, hits.Load())
}

func TestRenameKeepsSecret(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	up := testutil.LocationInput(pv, "eu-renamed")
	up.Description = "new description"
	up.PVE.TokenSecret = nil
	got, err := svc.UpdateLocation(ctx, loc.ID, up)
	require.NoError(t, err)
	require.Equal(t, "eu-renamed", got.Name)
	require.Equal(t, "new description", got.Description)
}
