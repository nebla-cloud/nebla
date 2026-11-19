package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// TestAdminOpsMatchSpec keeps adminOps equal to the operations under /admin/ in the spec.
func TestAdminOpsMatchSpec(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	require.NoError(t, err)
	want := map[string]bool{}
	all := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			id := strings.ToUpper(op.OperationID[:1]) + op.OperationID[1:]
			all[id] = true
			if strings.HasPrefix(path, "/admin/") {
				want[id] = true
			}
		}
	}
	require.NotEmpty(t, want)
	require.Equal(t, want, adminOps)
	for id := range publicOps {
		require.True(t, all[id], "publicOps has unknown op %s", id)
		require.False(t, adminOps[id], "%s is both public and admin", id)
	}
}

// TestProjectScopedOpsMatchSpec keeps projectScopedOps equal to the ops marked x-nebla-scope: project.
func TestProjectScopedOpsMatchSpec(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	require.NoError(t, err)
	want := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Extensions["x-nebla-scope"] == "project" {
				want[strings.ToUpper(op.OperationID[:1])+op.OperationID[1:]] = true
			}
		}
	}
	require.NotEmpty(t, want)
	require.Equal(t, want, projectScopedOps)
	for id := range projectScopedOps {
		require.False(t, adminOps[id] || publicOps[id], "%s is project scoped and admin or public", id)
	}
}

// TestScopedHandlersFailClosed checks scoped handlers return not_found when no scope is set.
func TestScopedHandlersFailClosed(t *testing.T) {
	s := &server{}
	ctx := context.Background()
	_, err := s.GetSshKeys(ctx, apigen.GetSshKeysRequestObject{})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.GetImages(ctx, apigen.GetImagesRequestObject{})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.DeleteSshKey(ctx, apigen.DeleteSshKeyRequestObject{Id: 1})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.GetActions(ctx, apigen.GetActionsRequestObject{})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.GetAction(ctx, apigen.GetActionRequestObject{Id: 1})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.PoweroffServer(ctx, apigen.PoweroffServerRequestObject{Id: 1})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.RebuildServer(ctx, apigen.RebuildServerRequestObject{Id: 1, Body: &apigen.RebuildInput{Image: "x"}})
	require.ErrorIs(t, err, errScopeNotFound)
	_, err = s.ChangeServerType(ctx, apigen.ChangeServerTypeRequestObject{Id: 1, Body: &apigen.ChangeTypeInput{ServerType: "x"}})
	require.ErrorIs(t, err, errScopeNotFound)
}
