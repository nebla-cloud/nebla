package osimages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/catalogbuild"
	"github.com/Hexeption/nebla/internal/ent/image"
	"github.com/Hexeption/nebla/internal/ent/imagetemplate"
	"github.com/Hexeption/nebla/internal/ent/location"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/jackc/pgx/v5/pgconn"
)

// TagTemplate marks every catalog template VM; never the server tag "nebla".
const TagTemplate = "nebla-tpl"

// templateTag is the per-build proof tag a template VM carries next to TagTemplate.
func templateTag(buildID int64, nonce string) string {
	return TagTemplate + "-" + strconv.FormatInt(buildID, 10) + "-" + nonce
}

const (
	reasonNoImport    = "location has no import_storage"
	reasonUnreachable = "Proxmox is unreachable"
	reasonVersion     = "no online node runs Proxmox VE 8.4 or later"
	pveCallTimeout    = 10 * time.Second
)

var (
	errNoEntry  = apierr.New(http.StatusNotFound, "not_found", "not found")
	errRetired  = apierr.New(http.StatusConflict, "catalog_retired", "this catalog entry was removed; it can only be disabled")
	errBuilding = apierr.New(http.StatusConflict, "conflict", "a build is already running")
	errNoReady  = apierr.New(http.StatusConflict, "conflict", "no ready template to refresh")
	errNoJobs   = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
)

func unavailable(reason string) error {
	return apierr.New(http.StatusUnprocessableEntity, "catalog_unavailable", reason)
}

type Options struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Jobs may be nil until SetJobs; actions fail with 503 until then.
	Jobs    *jobs.Jobs
	Entries []Entry
	HTTP    *http.Client
	Logger  *slog.Logger
}

type Service struct {
	o    Options
	jobs atomic.Pointer[jobs.Jobs]
	// afterStep is a test hook run after each build step is saved.
	afterStep func(buildID int64, step int)
}

func New(o Options) *Service {
	if o.Entries == nil {
		o.Entries = Entries()
	}
	if o.HTTP == nil {
		hc := *http.DefaultClient
		hc.Timeout = 30 * time.Second
		o.HTTP = &hc
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	s := &Service{o: o}
	s.jobs.Store(o.Jobs)
	return s
}

// SetJobs wires the job runtime, which is built after the workers are registered.
func (s *Service) SetJobs(j *jobs.Jobs) { s.jobs.Store(j) }

type NodeState struct {
	Node, Status string
	VMID         *int
	File         string
	BuiltAt      *time.Time
	Error        string
}

// LocationState.State is one of unavailable, off, building, ready, failed, partial or retired.
type LocationState struct {
	LocationID int64
	Available  bool
	Reason     string
	State      string
	Nodes      []NodeState
}

type EntryState struct {
	Key, Description, OSFlavor, OSVersion string
	Retired                               bool
	Locations                             []LocationState
}

// target is one build row to have: Node "" for shared disk storage, built on BuildNode.
type target struct{ Node, BuildNode string }

type availability struct {
	reason  string
	targets []target
}

// availability checks import storage and per-node PVE 8.4+, and lists the build targets.
func (s *Service) availability(ctx context.Context, loc catalog.Location) availability {
	if loc.ImportStorage == "" {
		return availability{reason: reasonNoImport}
	}
	c, err := s.o.Catalog.ProxmoxFor(ctx, loc.ID)
	if err != nil {
		s.o.Logger.Warn("catalog availability", "location_id", loc.ID, "err", err)
		return availability{reason: reasonUnreachable}
	}
	var nodes []catalog.Node
	for _, n := range loc.Nodes {
		if n.Enabled && n.Status == "online" {
			nodes = append(nodes, n)
		}
	}
	type result struct {
		ok, shared bool
		err        error
	}
	res := make([]result, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Go(func() {
			ok, shared, err := nodeCheck(ctx, c, n.Name, n.DiskStorage(loc))
			res[i] = result{ok, shared, err}
		})
	}
	wg.Wait()
	var out availability
	failed, sharedSeen := false, false
	for i, n := range nodes {
		r := res[i]
		if r.err != nil {
			s.o.Logger.Warn("catalog availability", "location_id", loc.ID, "node", n.Name, "err", r.err)
			failed = true
			continue
		}
		switch {
		case !r.ok:
		case !r.shared:
			out.targets = append(out.targets, target{Node: n.Name, BuildNode: n.Name})
		case !sharedSeen:
			sharedSeen = true
			out.targets = append([]target{{BuildNode: n.Name}}, out.targets...)
		}
	}
	if len(out.targets) == 0 {
		out.reason = reasonVersion
		if failed {
			out.reason = reasonUnreachable
		}
	}
	return out
}

// nodeCheck reports whether node runs PVE 8.4+ and whether its disk storage is shared.
func nodeCheck(ctx context.Context, c pve.Client, node, disk string) (ok, shared bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, pveCallTimeout)
	defer cancel()
	v, err := c.NodeVersion(ctx, node)
	if err != nil {
		return false, false, err
	}
	if !pve.AtLeast(v, 8, 4) {
		return false, false, nil
	}
	sts, err := c.Storages(ctx, node)
	if err != nil {
		return false, false, err
	}
	for _, st := range sts {
		if st.ID == disk {
			return true, st.Shared, nil
		}
	}
	return true, false, nil
}

// known reports whether key still has build rows or a catalog image.
func (s *Service) known(ctx context.Context, key string) (bool, error) {
	if ok, err := s.o.Ent.CatalogBuild.Query().Where(catalogbuild.CatalogKey(key)).Exist(ctx); err != nil || ok {
		return ok, err
	}
	return s.o.Ent.Image.Query().Where(image.CatalogKey(key)).Exist(ctx)
}

// entry returns the catalog entry for an Enable or Refresh, or not_found / catalog_retired.
func (s *Service) entry(ctx context.Context, key string) (Entry, error) {
	if e, ok := Lookup(s.o.Entries, key); ok {
		return e, nil
	}
	ok, err := s.known(ctx, key)
	if err != nil {
		return Entry{}, err
	}
	if ok {
		return Entry{}, errRetired
	}
	return Entry{}, errNoEntry
}

// prepare loads the location and checks the job runtime and availability for a build action.
func (s *Service) prepare(ctx context.Context, locationID int64) (catalog.Location, availability, *jobs.Jobs, error) {
	loc, err := s.o.Catalog.GetLocation(ctx, locationID)
	if err != nil {
		return loc, availability{}, nil, err
	}
	j := s.jobs.Load()
	if j == nil {
		return loc, availability{}, nil, errNoJobs
	}
	av := s.availability(ctx, loc)
	if av.reason != "" {
		return loc, av, nil, unavailable(av.reason)
	}
	return loc, av, j, nil
}

// lockRows locks key's build rows in the location and fails if any is building.
func lockRows(ctx context.Context, etx *ent.Client, key string, locationID int64) ([]*ent.CatalogBuild, error) {
	rows, err := etx.CatalogBuild.Query().Where(catalogbuild.CatalogKey(key), catalogbuild.LocationID(locationID)).
		Order(catalogbuild.ByNode()).ForUpdate().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Status == catalogbuild.StatusBuilding {
			return nil, errBuilding
		}
	}
	return rows, nil
}

// restart puts a row back to building from step 0, dropping any in-flight build state.
func restart(u *ent.CatalogBuildUpdateOne) *ent.CatalogBuildUpdateOne {
	return u.SetStatus(catalogbuild.StatusBuilding).SetStep(0).ClearErrorCode().ClearErrorMessage().
		ClearNewVmid().ClearNewNonce().ClearNewFile().ClearNewChecksum().ClearUpid().ClearBuildNode()
}

// cleanupArgs is the cleanup for a row's leftover half-built VM or import volume, if it kept either.
func cleanupArgs(entries []Entry, loc catalog.Location, r *ent.CatalogBuild) (CleanupArgs, bool) {
	a := CleanupArgs{BuildID: r.ID, LocationID: loc.ID}
	if r.NewVmid != nil && r.NewNonce != nil {
		a.VMID, a.Tag = *r.NewVmid, templateTag(r.ID, *r.NewNonce)
	}
	if r.BuildNode != nil {
		a.Node = *r.BuildNode
	}
	if e, ok := Lookup(entries, r.CatalogKey); ok && r.NewChecksum != nil && r.BuildNode != nil && loc.ImportStorage != "" {
		a.Volume = importVolume(loc.ImportStorage, r.CatalogKey, *r.NewChecksum, r.ID, e.Format)
	}
	return a, a.VMID != 0 || a.Volume != ""
}

func enqueueBuilds(ctx context.Context, j *jobs.Jobs, tx *sql.Tx, etx *ent.Client, ids []int64) error {
	for _, id := range ids {
		res, err := j.InsertTx(ctx, tx, BuildArgs{BuildID: id}, nil)
		if err != nil {
			return err
		}
		if err := etx.CatalogBuild.UpdateOneID(id).SetRiverJobID(res.Job.ID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// Enable inserts building rows for targets without one and retries failed rows; ready rows are left alone.
func (s *Service) Enable(ctx context.Context, key string, locationID int64) (LocationState, error) {
	if _, err := s.entry(ctx, key); err != nil {
		return LocationState{}, err
	}
	loc, av, j, err := s.prepare(ctx, locationID)
	if err != nil {
		return LocationState{}, err
	}
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		rows, err := lockRows(ctx, etx, key, loc.ID)
		if err != nil {
			return err
		}
		taken, err := etx.Image.Query().Where(image.Name(key), image.CatalogKeyIsNil()).Exist(ctx)
		if err != nil {
			return err
		}
		if taken {
			return apierr.New(http.StatusConflict, "conflict", fmt.Sprintf("an image named %q already exists", key))
		}
		byNode := map[string]*ent.CatalogBuild{}
		for _, r := range rows {
			byNode[r.Node] = r
		}
		var changed []int64
		for _, t := range av.targets {
			r, ok := byNode[t.Node]
			switch {
			case !ok:
				r, err = etx.CatalogBuild.Create().SetCatalogKey(key).SetLocationID(loc.ID).SetNode(t.Node).Save(ctx)
				if isUniqueViolation(err) {
					return errBuilding
				}
			case r.Status == catalogbuild.StatusFailed:
				if a, ok := cleanupArgs(s.o.Entries, loc, r); ok {
					if _, err := j.InsertTx(ctx, tx, a, nil); err != nil {
						return err
					}
				}
				err = restart(r.Update()).Exec(ctx)
			default:
				continue
			}
			if err != nil {
				return err
			}
			changed = append(changed, r.ID)
		}
		return enqueueBuilds(ctx, j, tx, etx, changed)
	})
	if err != nil {
		return LocationState{}, err
	}
	return s.locationState(ctx, key, loc, av, false)
}

// Refresh rebuilds every ready row; the live template keeps serving until the build swaps it.
func (s *Service) Refresh(ctx context.Context, key string, locationID int64) (LocationState, error) {
	if _, err := s.entry(ctx, key); err != nil {
		return LocationState{}, err
	}
	loc, av, j, err := s.prepare(ctx, locationID)
	if err != nil {
		return LocationState{}, err
	}
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		rows, err := lockRows(ctx, etx, key, loc.ID)
		if err != nil {
			return err
		}
		var changed []int64
		for _, r := range rows {
			if r.Status != catalogbuild.StatusReady {
				continue
			}
			if err := restart(r.Update()).Exec(ctx); err != nil {
				return err
			}
			changed = append(changed, r.ID)
		}
		if len(changed) == 0 {
			return errNoReady
		}
		return enqueueBuilds(ctx, j, tx, etx, changed)
	})
	if err != nil {
		return LocationState{}, err
	}
	return s.locationState(ctx, key, loc, av, false)
}

// Disable drops the entry from a location, retires its template VMs and deletes the image once unused.
func (s *Service) Disable(ctx context.Context, key string, locationID int64) (LocationState, error) {
	_, inCatalog := Lookup(s.o.Entries, key)
	if !inCatalog {
		ok, err := s.known(ctx, key)
		if err != nil {
			return LocationState{}, err
		}
		if !ok {
			return LocationState{}, errNoEntry
		}
	}
	loc, err := s.o.Catalog.GetLocation(ctx, locationID)
	if err != nil {
		return LocationState{}, err
	}
	j := s.jobs.Load()
	if j == nil {
		return LocationState{}, errNoJobs
	}
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		// Serialize with finalize, which takes the same lock.
		if _, err := tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext('osimages:' || $1))`, key); err != nil {
			return err
		}
		rows, err := lockRows(ctx, etx, key, loc.ID)
		if err != nil {
			return err
		}
		img, err := etx.Image.Query().Where(image.CatalogKey(key)).ForUpdate().Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if img != nil {
			if _, err := etx.ImageTemplate.Delete().
				Where(imagetemplate.ImageID(img.ID), imagetemplate.LocationID(loc.ID)).Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := etx.CatalogBuild.Delete().
			Where(catalogbuild.CatalogKey(key), catalogbuild.LocationID(loc.ID)).Exec(ctx); err != nil {
			return err
		}
		for _, r := range rows {
			if a, ok := cleanupArgs(s.o.Entries, loc, r); ok {
				if _, err := j.InsertTx(ctx, tx, a, nil); err != nil {
					return err
				}
			}
			if r.Vmid == nil {
				continue
			}
			if r.Nonce == nil {
				s.o.Logger.Warn("catalog disable: template has no nonce; not retiring it", "build_id", r.ID, "vmid", *r.Vmid)
				continue
			}
			args := RetireArgs{LocationID: loc.ID, Node: r.Node, VMID: *r.Vmid, Tag: templateTag(r.ID, *r.Nonce)}
			if _, err := j.InsertTx(ctx, tx, args, retireOpts()); err != nil {
				return err
			}
		}
		if img == nil {
			return nil
		}
		used, err := etx.ImageTemplate.Query().Where(imagetemplate.ImageID(img.ID)).Exist(ctx)
		if err != nil || used {
			return err
		}
		if used, err = etx.Server.Query().Where(server.ImageID(img.ID)).Exist(ctx); err != nil || used {
			return err
		}
		return etx.Image.DeleteOneID(img.ID).Exec(ctx)
	})
	if err != nil {
		return LocationState{}, err
	}
	// No Proxmox probe here, so Disable answers while Proxmox is down.
	av := availability{}
	if loc.ImportStorage == "" {
		av.reason = reasonNoImport
	}
	return s.locationState(ctx, key, loc, av, !inCatalog)
}

func (s *Service) locationState(ctx context.Context, key string, loc catalog.Location, av availability, retired bool) (LocationState, error) {
	rows, err := s.o.Ent.CatalogBuild.Query().Where(catalogbuild.CatalogKey(key), catalogbuild.LocationID(loc.ID)).
		Order(catalogbuild.ByNode()).All(ctx)
	if err != nil {
		return LocationState{}, err
	}
	return buildState(loc.ID, av, rows, retired), nil
}

func buildState(locationID int64, av availability, rows []*ent.CatalogBuild, retired bool) LocationState {
	ls := LocationState{LocationID: locationID, Available: av.reason == "", Reason: av.reason, Nodes: []NodeState{}}
	counts := map[catalogbuild.Status]int{}
	have := map[string]bool{}
	for _, r := range rows {
		counts[r.Status]++
		have[r.Node] = true
		ns := NodeState{Node: r.Node, Status: r.Status.String(), VMID: r.Vmid, BuiltAt: r.BuiltAt}
		if r.File != nil {
			ns.File = *r.File
		}
		if r.ErrorMessage != nil {
			ns.Error = *r.ErrorMessage
		}
		ls.Nodes = append(ls.Nodes, ns)
	}
	gap := slices.ContainsFunc(av.targets, func(t target) bool { return !have[t.Node] })
	switch {
	case len(rows) == 0 && !ls.Available:
		ls.State = "unavailable"
	case len(rows) == 0:
		ls.State = "off"
	case retired:
		ls.State = "retired"
	case counts[catalogbuild.StatusBuilding] > 0:
		ls.State = "building"
	case gap:
		ls.State = "partial"
	case counts[catalogbuild.StatusReady] == len(rows):
		ls.State = "ready"
	case counts[catalogbuild.StatusFailed] == len(rows):
		ls.State = "failed"
	default:
		ls.State = "partial"
	}
	if retired && len(rows) == 0 {
		ls.State = "off"
	}
	return ls
}

// List gives every catalog entry, then retired keys that still have build rows, with each location's state.
func (s *Service) List(ctx context.Context) ([]EntryState, error) {
	ids, err := s.o.Ent.Location.Query().Order(location.ByID()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	locs := make([]catalog.Location, len(ids))
	avs := make([]availability, len(ids))
	for i, id := range ids {
		if locs[i], err = s.o.Catalog.GetLocation(ctx, id); err != nil {
			return nil, err
		}
	}
	var wg sync.WaitGroup
	for i := range locs {
		wg.Go(func() { avs[i] = s.availability(ctx, locs[i]) })
	}
	wg.Wait()
	all, err := s.o.Ent.CatalogBuild.Query().Order(catalogbuild.ByNode()).All(ctx)
	if err != nil {
		return nil, err
	}
	type cell struct {
		key string
		loc int64
	}
	byCell := map[cell][]*ent.CatalogBuild{}
	var retired []string
	for _, r := range all {
		byCell[cell{r.CatalogKey, r.LocationID}] = append(byCell[cell{r.CatalogKey, r.LocationID}], r)
		if _, ok := Lookup(s.o.Entries, r.CatalogKey); !ok && !slices.Contains(retired, r.CatalogKey) {
			retired = append(retired, r.CatalogKey)
		}
	}
	slices.Sort(retired)

	out := make([]EntryState, 0, len(s.o.Entries)+len(retired))
	for _, e := range s.o.Entries {
		out = append(out, EntryState{Key: e.Key, Description: e.Description, OSFlavor: e.OSFlavor, OSVersion: e.OSVersion})
	}
	for _, key := range retired {
		es := EntryState{Key: key, Retired: true}
		img, err := s.o.Ent.Image.Query().Where(image.CatalogKey(key)).Only(ctx)
		switch {
		case err == nil:
			es.Description, es.OSFlavor, es.OSVersion = img.Description, img.OsFlavor, img.OsVersion
		case !ent.IsNotFound(err):
			return nil, err
		}
		out = append(out, es)
	}
	for i := range out {
		out[i].Locations = make([]LocationState, len(locs))
		for k, loc := range locs {
			out[i].Locations[k] = buildState(loc.ID, avs[k], byCell[cell{out[i].Key, loc.ID}], out[i].Retired)
		}
	}
	return out, nil
}
