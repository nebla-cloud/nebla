// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackoffZeroValuesUseDefaults(t *testing.T) {
	tr := retryTransport{policy: RetryPolicy{MaxAttempts: 5}}
	assert.Equal(t, 500*time.Millisecond, tr.backoff(0, nil))
	assert.Equal(t, time.Second, tr.backoff(1, nil))
	assert.Equal(t, 5*time.Second, tr.backoff(4, nil))
	tr = retryTransport{policy: RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second}}
	assert.Equal(t, 5*time.Second, tr.backoff(3, nil))
}

func TestBackoffNoOverflow(t *testing.T) {
	tr := retryTransport{policy: RetryPolicy{MaxAttempts: 100, BaseDelay: time.Hour, MaxDelay: time.Minute}}
	for _, a := range []int{0, 1, 20, 29, 30, 62, 99} {
		assert.Equal(t, time.Minute, tr.backoff(a, nil), a)
	}
	tr = retryTransport{policy: RetryPolicy{MaxAttempts: 100, BaseDelay: time.Second, MaxDelay: time.Hour}}
	assert.Equal(t, time.Hour, tr.backoff(40, nil))
}

func TestListAllWithoutMetaPagesUntilShort(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		n := map[int]int{1: 50, 2: 50, 3: 7}[page]
		acts := []map[string]any{}
		for i := 0; i < n; i++ {
			acts = append(acts, map[string]any{"id": (page-1)*50 + i + 1, "command": "x", "status": "success"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"actions": acts})
	})
	got, err := c.Actions.All(context.Background(), ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, got, 107)
	assert.Equal(t, 3, f.count())
}

func TestUpdateNilLabelsMapSendsEmptyObject(t *testing.T) {
	c, f := newFake(t)
	f.handle("PUT", "/v1/volumes/3", 200, "putVolume")
	var nilMap map[string]string
	_, err := c.Volumes.Update(context.Background(), 3, VolumeUpdateOpts{Labels: &nilMap})
	require.NoError(t, err)
	assert.JSONEq(t, `{"labels":{}}`, bodyString(f))
}

func TestListStatusSlicesRepeatParameter(t *testing.T) {
	q := ServerListOpts{Status: []ServerStatus{ServerStatusRunning, ServerStatusOff}}.values()
	assert.Equal(t, []string{"running", "off"}, q["status"])
	q = ActionListOpts{Status: []ActionStatus{ActionStatusRunning, ActionStatusError}}.values()
	assert.Equal(t, []string{"running", "error"}, q["status"])
}
