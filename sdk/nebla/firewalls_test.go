// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirewallResourceHelpers(t *testing.T) {
	b, err := json.Marshal(FirewallServer(5))
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"server","server":{"id":5}}`, string(b))
	b, err = json.Marshal(FirewallLabels("env=prod"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"label_selector","label_selector":{"selector":"env=prod"}}`, string(b))
}

func TestFirewallsListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/firewalls", 200, "getFirewalls")
	f.handle("GET", "/v1/firewalls/2", 200, "getFirewall")
	fs, meta, err := c.Firewalls.List(ctx, FirewallListOpts{ListOpts: ListOpts{Page: 2}, Name: "web", LabelSelector: "env=prod"})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "web", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, fs, 1)

	all, err := c.Firewalls.All(ctx, FirewallListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	fw, err := c.Firewalls.Get(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), fw.ID)
	assert.Equal(t, "web", fw.Name)
	assert.Equal(t, map[string]string{"env": "prod"}, fw.Labels)
	require.Len(t, fw.Rules, 2)
	assert.Equal(t, FirewallRule{Direction: FirewallDirectionIn, Protocol: FirewallProtocolTCP, Port: "80", SourceIPs: []string{"0.0.0.0/0", "::/0"}, Description: "http"}, fw.Rules[0])
	assert.Equal(t, FirewallRule{Direction: FirewallDirectionOut, Protocol: FirewallProtocolICMP, DestinationIPs: []string{"0.0.0.0/0"}}, fw.Rules[1])
	require.Len(t, fw.AppliedTo, 2)
	assert.Equal(t, FirewallServer(7), fw.AppliedTo[0])
	assert.Equal(t, FirewallResourceTypeLabelSelector, fw.AppliedTo[1].Type)
	assert.Equal(t, "env=prod", fw.AppliedTo[1].LabelSelector.Selector)
	assert.Equal(t, []FirewallAppliedResource{{Type: FirewallResourceTypeServer, Server: FirewallResourceServer{ID: 8}}}, fw.AppliedTo[1].AppliedToResources)

	fw, err = c.Firewalls.GetByName(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, int64(2), fw.ID)
	_, err = c.Firewalls.GetByName(ctx, "we")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFirewallCreate(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/firewalls", 201, "postFirewalls")
	res, err := c.Firewalls.Create(ctx, FirewallCreateOpts{
		Name:    "web",
		Labels:  map[string]string{"a": "b"},
		Rules:   []FirewallRule{{Direction: FirewallDirectionIn, Protocol: FirewallProtocolTCP, Port: "80", SourceIPs: []string{"0.0.0.0/0"}}},
		ApplyTo: []FirewallResource{FirewallServer(7), FirewallLabels("env=prod")},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Firewall.ID)
	require.Len(t, res.Actions, 1)
	assert.Equal(t, "apply_firewalls", res.Actions[0].Command)
	assert.JSONEq(t, `{"name":"web","labels":{"a":"b"},"rules":[{"direction":"in","protocol":"tcp","port":"80","source_ips":["0.0.0.0/0"]}],"apply_to":[{"type":"server","server":{"id":7}},{"type":"label_selector","label_selector":{"selector":"env=prod"}}]}`, bodyString(f))

	_, err = c.Firewalls.Create(ctx, FirewallCreateOpts{Name: "web"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"web"}`, bodyString(f))
}

func TestFirewallUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/firewalls/2", 200, "putFirewall")
	f.handle("DELETE", "/v1/firewalls/2", 200, "deleteFirewall")
	f.handle("GET", "/v1/firewalls/2/actions", 200, "getFirewallActions")
	fw, err := c.Firewalls.Update(ctx, 2, FirewallUpdateOpts{Name: Ptr("new")})
	require.NoError(t, err)
	assert.Equal(t, int64(2), fw.ID)
	assert.JSONEq(t, `{"name":"new"}`, bodyString(f))

	as, err := c.Firewalls.Delete(ctx, 2)
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.Equal(t, "apply_firewalls", as[0].Command)
	r, _ := f.last()
	assert.Equal(t, "DELETE", r.Method)

	acts, meta, err := c.Firewalls.Actions(ctx, 2, ActionListOpts{Status: ActionStatusSuccess})
	require.NoError(t, err)
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, acts, 1)
	all, err := c.Firewalls.AllActions(ctx, 2, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestFirewallResourceActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/firewalls/2/actions/set_rules", 201, "setFirewallRules")
	f.handle("POST", "/v1/firewalls/2/actions/apply_to_resources", 201, "applyFirewallToResources")
	f.handle("POST", "/v1/firewalls/2/actions/remove_from_resources", 201, "removeFirewallFromResources")

	as, err := c.Firewalls.SetRules(ctx, 2, []FirewallRule{{Direction: FirewallDirectionOut, Protocol: FirewallProtocolUDP, Port: "53", DestinationIPs: []string{"::/0"}}})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.JSONEq(t, `{"rules":[{"direction":"out","protocol":"udp","port":"53","destination_ips":["::/0"]}]}`, bodyString(f))

	_, err = c.Firewalls.SetRules(ctx, 2, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rules":[]}`, bodyString(f))

	as, err = c.Firewalls.ApplyToResources(ctx, 2, []FirewallResource{FirewallServer(7)})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.JSONEq(t, `{"apply_to":[{"type":"server","server":{"id":7}}]}`, bodyString(f))

	as, err = c.Firewalls.RemoveFromResources(ctx, 2, []FirewallResource{FirewallLabels("env=prod")})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.JSONEq(t, `{"remove_from":[{"type":"label_selector","label_selector":{"selector":"env=prod"}}]}`, bodyString(f))
}
