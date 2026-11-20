package labels

import (
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	s, err := Parse("role=web,env!=prod,tier,!old")
	require.NoError(t, err)
	require.Equal(t, Selector{
		{Key: "role", Value: "web", Op: Eq},
		{Key: "env", Value: "prod", Op: Ne},
		{Key: "tier", Op: Has},
		{Key: "old", Op: Lacks},
	}, s)
	require.Equal(t, "role=web,env!=prod,tier,!old", s.String())

	s, err = Parse(" a = b , !c ")
	require.NoError(t, err)
	require.Equal(t, "a=b,!c", s.String())

	_, err = Parse("")
	requireMsg(t, err, "label_selector must not be empty")
	_, err = Parse("  ")
	requireMsg(t, err, "label_selector must not be empty")
	_, err = Parse("role=web,-bad=x")
	requireMsg(t, err, `label_selector: bad key in "-bad=x"`)
	_, err = Parse("a,,b")
	requireMsg(t, err, `label_selector: bad key in ""`)
	_, err = Parse("a=-x")
	requireMsg(t, err, `label_selector: bad value in "a=-x"`)
}

func TestMatches(t *testing.T) {
	s, err := Parse("role=web,env!=prod,tier,!old")
	require.NoError(t, err)
	require.True(t, s.Matches(map[string]string{"role": "web", "env": "dev", "tier": "x"}))
	require.True(t, s.Matches(map[string]string{"role": "web", "tier": "x"}))
	require.False(t, s.Matches(map[string]string{"role": "web", "env": "prod", "tier": "x"}))
	require.False(t, s.Matches(map[string]string{"role": "web", "tier": "x", "old": "y"}))
	require.False(t, s.Matches(nil))
}

func requireMsg(t *testing.T, err error, msg string) {
	t.Helper()
	var e apierr.E
	require.ErrorAs(t, err, &e)
	require.Equal(t, "invalid_input", e.Code)
	require.Equal(t, msg, e.Msg)
}
