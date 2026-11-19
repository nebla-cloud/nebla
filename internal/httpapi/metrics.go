package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/pve"
)

var (
	metricTypes = []string{"cpu", "disk", "network"}
	timeframes  = []string{"hour", "day", "week", "month"}

	errVMMissing = apierr.New(http.StatusConflict, "server_missing", "the server's VM is missing in Proxmox")
	errPVE       = apierr.New(http.StatusBadGateway, "pve_unreachable", "could not read metrics from Proxmox")
)

// series maps RRD points to [unix_ts, value] pairs; a gap stays null.
func series(pts []pve.RRDPoint, scale float64, f func(pve.RRDPoint) *float64) [][]*float64 {
	out := make([][]*float64, len(pts))
	for i, p := range pts {
		ts := float64(p.Time)
		var v *float64
		if x := f(p); x != nil {
			v = new(*x * scale)
		}
		out[i] = []*float64{&ts, v}
	}
	return out
}

func (s *server) GetServerMetrics(ctx context.Context, req apigen.GetServerMetricsRequestObject) (apigen.GetServerMetricsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	types := splitList(req.Params.Type)
	if len(types) == 0 {
		return nil, invalidInput("type is required: cpu, disk or network")
	}
	for _, t := range types {
		if !slices.Contains(metricTypes, t) {
			return nil, invalidInput("unknown metric type " + t + ": want cpu, disk or network")
		}
	}
	tf := deref(req.Params.Timeframe)
	if !slices.Contains(timeframes, tf) {
		return nil, invalidInput("timeframe must be hour, day, week or month")
	}
	vm, err := s.servers.VM(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	pc, err := s.catalog.ProxmoxFor(ctx, vm.LocationID)
	if err != nil {
		return nil, err
	}
	pts, err := pc.RRD(ctx, vm.Node, vm.VMID, tf)
	if errors.Is(err, pve.ErrNotFound) {
		return nil, errVMMissing
	}
	if err != nil {
		s.log.Warn("metrics: rrd", "server_id", req.Id, "err", err)
		return nil, errPVE
	}
	m := apigen.Metrics{TimeSeries: map[string][][]*float64{}}
	if len(pts) > 0 {
		m.Start, m.End = time.Unix(pts[0].Time, 0).UTC(), time.Unix(pts[len(pts)-1].Time, 0).UTC()
	}
	if len(pts) > 1 {
		m.Step = int(pts[1].Time - pts[0].Time)
	}
	for _, t := range types {
		switch t {
		case "cpu":
			// Proxmox reports cpu as a 0-1 fraction of the VM's cores; the API uses percent.
			m.TimeSeries["cpu"] = series(pts, 100, func(p pve.RRDPoint) *float64 { return p.CPU })
		case "disk":
			m.TimeSeries["disk.read"] = series(pts, 1, func(p pve.RRDPoint) *float64 { return p.DiskRead })
			m.TimeSeries["disk.write"] = series(pts, 1, func(p pve.RRDPoint) *float64 { return p.DiskWrite })
		case "network":
			m.TimeSeries["network.in"] = series(pts, 1, func(p pve.RRDPoint) *float64 { return p.NetIn })
			m.TimeSeries["network.out"] = series(pts, 1, func(p pve.RRDPoint) *float64 { return p.NetOut })
		}
	}
	return apigen.GetServerMetrics200JSONResponse{Metrics: m}, nil
}
