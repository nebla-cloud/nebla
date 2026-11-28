// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/url"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// actionCall sends a request whose reply is {action}.
func (s *ServersService) actionCall(ctx context.Context, method, path string, body any) (*Action, error) {
	var w struct {
		Action wire.Action `json:"action"`
	}
	if err := s.c.do(ctx, method, path, nil, body, &w); err != nil {
		return nil, err
	}
	a := actionFromWire(w.Action)
	return &a, nil
}

func (s *ServersService) simple(ctx context.Context, id int64, name string) (*Action, error) {
	return s.actionCall(ctx, "POST", serverPath(id, "/actions/"+name), nil)
}

// PowerOn starts a server.
func (s *ServersService) PowerOn(ctx context.Context, id int64) (*Action, error) {
	return s.simple(ctx, id, "poweron")
}

// PowerOff stops a server immediately.
func (s *ServersService) PowerOff(ctx context.Context, id int64) (*Action, error) {
	return s.simple(ctx, id, "poweroff")
}

// Shutdown shuts a server down gracefully.
func (s *ServersService) Shutdown(ctx context.Context, id int64) (*Action, error) {
	return s.simple(ctx, id, "shutdown")
}

// Reboot reboots a server gracefully.
func (s *ServersService) Reboot(ctx context.Context, id int64) (*Action, error) {
	return s.simple(ctx, id, "reboot")
}

// Reset resets a server.
func (s *ServersService) Reset(ctx context.Context, id int64) (*Action, error) {
	return s.simple(ctx, id, "reset")
}

// ServerRebuildOpts reinstalls a server; give Image or Snapshot.
type ServerRebuildOpts struct {
	Image, Snapshot string
}

type serverRebuildBody struct {
	Image    string `json:"image,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}

// Rebuild reinstalls a server from an image or snapshot, keeping its IPs and MAC.
func (s *ServersService) Rebuild(ctx context.Context, id int64, opts ServerRebuildOpts) (*Action, error) {
	return s.actionCall(ctx, "POST", serverPath(id, "/actions/rebuild"), serverRebuildBody{Image: opts.Image, Snapshot: opts.Snapshot})
}

// ServerChangeTypeOpts changes a stopped server's type.
type ServerChangeTypeOpts struct {
	ServerType  string
	UpgradeDisk bool
}

type serverChangeTypeBody struct {
	ServerType  string `json:"server_type"`
	UpgradeDisk bool   `json:"upgrade_disk"`
}

// ChangeType changes a stopped server's type.
func (s *ServersService) ChangeType(ctx context.Context, id int64, opts ServerChangeTypeOpts) (*Action, error) {
	return s.actionCall(ctx, "POST", serverPath(id, "/actions/change_type"), serverChangeTypeBody{ServerType: opts.ServerType, UpgradeDisk: opts.UpgradeDisk})
}

// RestoreBackup restores a server in place from one of its backups.
func (s *ServersService) RestoreBackup(ctx context.Context, id, backupID int64) (*Action, error) {
	body := struct {
		Backup int64 `json:"backup"`
	}{backupID}
	return s.actionCall(ctx, "POST", serverPath(id, "/actions/restore_backup"), body)
}

func (s *ServersService) networkCall(ctx context.Context, id int64, name string, body any) (*Action, *NetworkGuestConfig, error) {
	var w wire.NetworkActionCreated
	if err := s.c.do(ctx, "POST", serverPath(id, "/actions/"+name), nil, body, &w); err != nil {
		return nil, nil, err
	}
	a := actionFromWire(w.Action)
	return &a, guestConfigFromWire(w.GuestConfig), nil
}

// ServerAttachNetworkOpts attaches a server to a private network; IP is optional.
type ServerAttachNetworkOpts struct {
	Network, IP string
}

type serverNetworkBody struct {
	Network string `json:"network"`
	IP      string `json:"ip,omitempty"`
}

// AttachToNetwork attaches a server to a private network in its location.
func (s *ServersService) AttachToNetwork(ctx context.Context, id int64, opts ServerAttachNetworkOpts) (*Action, *NetworkGuestConfig, error) {
	return s.networkCall(ctx, id, "attach_to_network", serverNetworkBody{Network: opts.Network, IP: opts.IP})
}

// DetachFromNetwork detaches a server from a private network.
func (s *ServersService) DetachFromNetwork(ctx context.Context, id int64, network string) (*Action, *NetworkGuestConfig, error) {
	return s.networkCall(ctx, id, "detach_from_network", serverNetworkBody{Network: network})
}

// ConsoleGrant is a one-time console URL and its VNC password.
type ConsoleGrant struct {
	WSSURL   string
	Password string
}

// RequestConsole returns a single-use console URL for a running server.
func (s *ServersService) RequestConsole(ctx context.Context, id int64) (*ConsoleGrant, error) {
	var w wire.ConsoleGrant
	if err := s.c.do(ctx, "POST", serverPath(id, "/actions/request_console"), nil, nil, &w); err != nil {
		return nil, err
	}
	return &ConsoleGrant{WSSURL: w.WssURL, Password: w.Password}, nil
}

// MetricPoint is one sample; Value is nil where there is no sample.
type MetricPoint struct {
	Time  time.Time
	Value *float64
}

// MetricSeries is the samples of one metric in time order.
type MetricSeries []MetricPoint

// Metrics is a server's resource usage over a timeframe.
type Metrics struct {
	Start time.Time
	End   time.Time
	// Step is the seconds between points.
	Step int
	// TimeSeries is keyed cpu, disk.read, disk.write, network.in and network.out.
	TimeSeries map[string]MetricSeries
}

// ServerMetricsOpts selects metrics; Types are cpu, disk and/or network, Timeframe is hour, day, week or month.
type ServerMetricsOpts struct {
	Types     []string
	Timeframe string
}

func (o ServerMetricsOpts) values() url.Values {
	q := url.Values{}
	for _, t := range o.Types {
		q.Add("type", t)
	}
	if o.Timeframe != "" {
		q.Set("timeframe", o.Timeframe)
	}
	return q
}

func metricsFromWire(w wire.Metrics) Metrics {
	m := Metrics{Start: w.Start, End: w.End, Step: w.Step, TimeSeries: make(map[string]MetricSeries, len(w.TimeSeries))}
	for k, pts := range w.TimeSeries {
		series := make(MetricSeries, 0, len(pts))
		for _, p := range pts {
			if len(p) < 2 || p[0] == nil {
				continue
			}
			series = append(series, MetricPoint{Time: time.Unix(int64(*p[0]), 0).UTC(), Value: p[1]})
		}
		m.TimeSeries[k] = series
	}
	return m
}

// Metrics returns a server's CPU, disk and network metrics.
func (s *ServersService) Metrics(ctx context.Context, id int64, opts ServerMetricsOpts) (*Metrics, error) {
	var w struct {
		Metrics wire.Metrics `json:"metrics"`
	}
	if err := s.c.do(ctx, "GET", serverPath(id, "/metrics"), opts.values(), nil, &w); err != nil {
		return nil, err
	}
	m := metricsFromWire(w.Metrics)
	return &m, nil
}
