package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/catalogbuild"
	"github.com/nebla-cloud/nebla/internal/ent/imagetemplate"
	"github.com/nebla-cloud/nebla/internal/ent/ippool"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/servertype"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// NodeMissing is the status of a node Proxmox no longer reports; placement needs Enabled and status "online".
const NodeMissing = "missing"

type Location struct {
	ID            int64
	Name          string
	Description   string
	PVEURL        string
	TokenID       string
	HasSecret     bool
	CAFingerprint string
	InsecureTLS   bool
	DiskStorage   string
	ISOStorage    string
	ImportStorage string
	Nodes         []Node
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Node is a Proxmox node as last synced; MaxMem and Mem are bytes, CPU is a 0-1 load fraction.
type Node struct {
	ID                  int64
	LocationID          int64
	Name                string
	Status              string
	Enabled             bool
	DiskStorageOverride string
	MaxCPU              int
	MaxMem              int64
	CPU                 float64
	Mem                 int64
	UpdatedAt           time.Time
}

// DiskStorage is the storage new disks on n go to.
func (n Node) DiskStorage(loc Location) string {
	if n.DiskStorageOverride != "" {
		return n.DiskStorageOverride
	}
	return loc.DiskStorage
}

type PVEInput struct {
	URL     string
	TokenID string
	// TokenSecret is required on create; nil on update keeps the stored secret.
	TokenSecret   *string
	CAFingerprint string
	InsecureTLS   bool
}

type LocationInput struct {
	Name        string
	Description string
	PVE         PVEInput
	DiskStorage string
	ISOStorage  string
	// ImportStorage is optional; when set it must allow the import content type.
	ImportStorage string
}

type NodeUpdate struct {
	Enabled *bool
	// DiskStorageOverride "" clears the override.
	DiskStorageOverride *string
}

var nameRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func toNode(n *ent.Node) Node {
	return Node{ID: n.ID, LocationID: n.LocationID, Name: n.Name, Status: n.Status, Enabled: n.Enabled,
		DiskStorageOverride: n.DiskStorageOverride, MaxCPU: n.Maxcpu, MaxMem: n.Maxmem, CPU: n.CPU, Mem: n.Mem, UpdatedAt: n.UpdatedAt}
}

func toLocation(l *ent.Location) Location {
	out := Location{ID: l.ID, Name: l.Name, Description: l.Description, PVEURL: l.PveURL, TokenID: l.TokenID,
		HasSecret: len(l.TokenSecret) > 0, CAFingerprint: l.CaFingerprint, InsecureTLS: l.InsecureTLS,
		DiskStorage: l.DiskStorage, ISOStorage: l.IsoStorage, ImportStorage: l.ImportStorage, Nodes: []Node{}, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt}
	for _, n := range l.Edges.Nodes {
		out.Nodes = append(out.Nodes, toNode(n))
	}
	return out
}

func withNodes(q *ent.NodeQuery) { q.Order(node.ByName()) }

// validLocation normalises in; the returned conn has no secret yet.
func (s *Service) validLocation(in *LocationInput) (pve.Conn, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.DiskStorage = strings.TrimSpace(in.DiskStorage)
	in.ISOStorage = strings.TrimSpace(in.ISOStorage)
	in.ImportStorage = strings.TrimSpace(in.ImportStorage)
	in.PVE.TokenID = strings.TrimSpace(in.PVE.TokenID)
	in.PVE.CAFingerprint = strings.TrimSpace(in.PVE.CAFingerprint)
	if !nameRE.MatchString(in.Name) {
		return pve.Conn{}, invalid("name must match %s", nameRE)
	}
	if len(in.Description) > 500 {
		return pve.Conn{}, invalid("description must be at most 500 characters")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(in.PVE.URL), "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" {
		return pve.Conn{}, invalid("pve.url must be a base URL like https://host:8006")
	}
	in.PVE.URL = u.String()
	if in.PVE.TokenID == "" || !strings.Contains(in.PVE.TokenID, "!") {
		return pve.Conn{}, invalid("pve.token_id must look like user@realm!tokenname")
	}
	if in.DiskStorage == "" || in.ISOStorage == "" {
		return pve.Conn{}, invalid("disk_storage and iso_storage are required")
	}
	return pve.Conn{BaseURL: in.PVE.URL, TokenID: in.PVE.TokenID, CAFingerprint: in.PVE.CAFingerprint,
		InsecureTLS: in.PVE.InsecureTLS, PollInterval: s.opts.PollInterval}, nil
}

// probe connects with conn, checks the storages (imp may be empty) and returns the discovered nodes.
func (s *Service) probe(ctx context.Context, conn pve.Conn, disk, iso, imp string) ([]pve.Node, error) {
	ctx, cancel := context.WithTimeout(ctx, pveTimeout)
	defer cancel()
	c := s.opts.NewClient(conn)
	if _, err := c.Version(ctx); err != nil {
		return nil, unreachable(err)
	}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return nil, unreachable(err)
	}
	if len(nodes) == 0 {
		return nil, invalid("Proxmox reports no nodes")
	}
	var diskSeen, diskOK, isoSeen, isoOK, impSeen, impOK bool
	for _, n := range nodes {
		if n.Status != "online" {
			continue
		}
		sts, err := c.Storages(ctx, n.Name)
		if err != nil {
			return nil, unreachable(fmt.Errorf("list storages on %s: %w", n.Name, err))
		}
		for _, st := range sts {
			if st.ID == disk {
				diskSeen, diskOK = true, diskOK || slices.Contains(st.Content, "images")
			}
			if st.ID == iso {
				isoSeen, isoOK = true, isoOK || slices.Contains(st.Content, "iso")
			}
			if imp != "" && st.ID == imp {
				impSeen, impOK = true, impOK || slices.Contains(st.Content, "import")
			}
		}
	}
	switch {
	case !diskSeen:
		return nil, invalid("disk_storage %q does not exist on any online node", disk)
	case !diskOK:
		return nil, invalid("disk_storage %q does not allow the images content type", disk)
	case !isoSeen:
		return nil, invalid("iso_storage %q does not exist on any online node", iso)
	case !isoOK:
		return nil, invalid("iso_storage %q does not allow the iso content type", iso)
	case imp != "" && !impSeen:
		return nil, invalid("import_storage %q does not exist on any online node", imp)
	case imp != "" && !impOK:
		return nil, invalid("import_storage %q does not allow the import content type", imp)
	}
	return nodes, nil
}

// CreateLocation verifies the Proxmox connection and storages, then stores the location and its nodes.
func (s *Service) CreateLocation(ctx context.Context, in LocationInput) (Location, error) {
	if s.boxErr != nil {
		return Location{}, s.boxErr
	}
	conn, err := s.validLocation(&in)
	if err != nil {
		return Location{}, err
	}
	if in.PVE.TokenSecret == nil || *in.PVE.TokenSecret == "" {
		return Location{}, invalid("pve.token_secret is required")
	}
	conn.Secret = *in.PVE.TokenSecret
	nodes, err := s.probe(ctx, conn, in.DiskStorage, in.ISOStorage, in.ImportStorage)
	if err != nil {
		return Location{}, err
	}
	var id int64
	err = s.withTx(ctx, func(tx *ent.Client) error {
		l, err := tx.Location.Create().SetName(in.Name).SetDescription(in.Description).SetPveURL(conn.BaseURL).
			SetTokenID(conn.TokenID).SetTokenSecret(s.box.Seal([]byte(conn.Secret))).SetCaFingerprint(conn.CAFingerprint).
			SetInsecureTLS(conn.InsecureTLS).SetDiskStorage(in.DiskStorage).SetIsoStorage(in.ISOStorage).SetImportStorage(in.ImportStorage).Save(ctx)
		if err != nil {
			return mapWriteErr(err, "location")
		}
		id = l.ID
		return upsertNodes(ctx, tx, id, nodes)
	})
	if err != nil {
		return Location{}, err
	}
	return s.GetLocation(ctx, id)
}

// UpdateLocation re-verifies Proxmox when connection or storage settings change.
func (s *Service) UpdateLocation(ctx context.Context, id int64, in LocationInput) (Location, error) {
	if s.boxErr != nil {
		return Location{}, s.boxErr
	}
	conn, err := s.validLocation(&in)
	if err != nil {
		return Location{}, err
	}
	cur, err := s.db.Location.Get(ctx, id)
	if err != nil {
		return Location{}, notFound(err)
	}
	newSecret := in.PVE.TokenSecret != nil && *in.PVE.TokenSecret != ""
	if !newSecret && (conn.BaseURL != cur.PveURL || conn.TokenID != cur.TokenID ||
		pve.NormFP(conn.CAFingerprint) != pve.NormFP(cur.CaFingerprint) || conn.InsecureTLS != cur.InsecureTLS) {
		return Location{}, invalid("pve.token_secret is required when url, token_id, ca_fingerprint or insecure_tls changes")
	}
	if newSecret {
		conn.Secret = *in.PVE.TokenSecret
	} else if conn.Secret, err = s.openSecret(cur); err != nil {
		return Location{}, err
	}
	changed := newSecret || conn.BaseURL != cur.PveURL || conn.TokenID != cur.TokenID || conn.CAFingerprint != cur.CaFingerprint ||
		conn.InsecureTLS != cur.InsecureTLS || in.DiskStorage != cur.DiskStorage || in.ISOStorage != cur.IsoStorage ||
		in.ImportStorage != cur.ImportStorage
	var nodes []pve.Node
	if changed {
		if nodes, err = s.probe(ctx, conn, in.DiskStorage, in.ISOStorage, in.ImportStorage); err != nil {
			return Location{}, err
		}
	}
	err = s.withTx(ctx, func(tx *ent.Client) error {
		u := tx.Location.UpdateOneID(id).SetName(in.Name).SetDescription(in.Description).SetPveURL(conn.BaseURL).
			SetTokenID(conn.TokenID).SetCaFingerprint(conn.CAFingerprint).SetInsecureTLS(conn.InsecureTLS).
			SetDiskStorage(in.DiskStorage).SetIsoStorage(in.ISOStorage).SetImportStorage(in.ImportStorage)
		if newSecret {
			u.SetTokenSecret(s.box.Seal([]byte(conn.Secret)))
		}
		if err := u.Exec(ctx); err != nil {
			return mapWriteErr(err, "location")
		}
		if changed {
			return syncNodes(ctx, tx, id, nodes)
		}
		return nil
	})
	if err != nil {
		return Location{}, err
	}
	s.forget(id)
	return s.GetLocation(ctx, id)
}

func (s *Service) GetLocation(ctx context.Context, id int64) (Location, error) {
	l, err := s.db.Location.Query().Where(location.ID(id)).WithNodes(withNodes).Only(ctx)
	if err != nil {
		return Location{}, notFound(err)
	}
	return toLocation(l), nil
}

// LocationByRef finds a location by numeric id or by name.
func (s *Service) LocationByRef(ctx context.Context, ref string) (Location, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		if l, err := s.GetLocation(ctx, id); err == nil {
			return l, nil
		}
	}
	l, err := s.db.Location.Query().Where(location.Name(ref)).WithNodes(withNodes).Only(ctx)
	if err != nil {
		return Location{}, notFound(err)
	}
	return toLocation(l), nil
}

func (s *Service) ListLocations(ctx context.Context, page, perPage int) ([]Location, int, error) {
	total, err := s.db.Location.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	off, lim := pageBounds(page, perPage)
	rows, err := s.db.Location.Query().Order(location.ByID()).Offset(off).Limit(lim).WithNodes(withNodes).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Location, len(rows))
	for i, r := range rows {
		out[i] = toLocation(r)
	}
	return out, total, nil
}

func locationInUse(why string) error {
	return apierr.New(http.StatusConflict, "location_in_use", "location is in use: "+why)
}

// DeleteLocation refuses while images, catalog builds or their template jobs, restricted server types or LocationInUse reference the location.
func (s *Service) DeleteLocation(ctx context.Context, id int64) error {
	err := s.withTx(ctx, func(tx *ent.Client) error {
		if _, err := tx.Location.Query().Where(location.ID(id)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		imgs, err := tx.ImageTemplate.Query().Where(imagetemplate.LocationID(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if imgs {
			return locationInUse("images still have templates in it")
		}
		builds, err := tx.CatalogBuild.Query().Where(catalogbuild.LocationID(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if builds {
			return locationInUse("OS catalog images are still enabled in it")
		}
		// Checked after catalog_builds: Disable deletes the rows and queues these jobs in one commit.
		pending, err := s.opts.TemplatesPending(ctx, tx, id)
		if err != nil {
			return err
		}
		if pending {
			return locationInUse("OS catalog templates are still being removed from it")
		}
		// Dropping the last location of a restricted type would silently offer it everywhere.
		types, err := tx.ServerType.Query().Where(servertype.HasLocationsWith(location.ID(id))).Exist(ctx)
		if err != nil {
			return err
		}
		if types {
			return locationInUse("server types are restricted to it")
		}
		pools, err := tx.IPPool.Query().Where(ippool.LocationID(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if pools {
			return locationInUse("IP pools still exist in it")
		}
		busy, err := s.opts.LocationInUse(ctx, tx, id)
		if err != nil {
			return err
		}
		if busy {
			return locationInUse("servers or IP pools still use it")
		}
		if err := tx.Location.DeleteOneID(id).Exec(ctx); err != nil {
			if pgCode(err) == "23503" {
				return locationInUse("other resources reference it")
			}
			return notFound(err)
		}
		return nil
	})
	if err == nil {
		s.forget(id)
	}
	return err
}

// SyncNodes refreshes status, capacity and load; vanished nodes become status missing and enabled is left alone.
func (s *Service) SyncNodes(ctx context.Context, id int64) (Location, error) {
	c, err := s.ProxmoxFor(ctx, id)
	if err != nil {
		return Location{}, err
	}
	tctx, cancel := context.WithTimeout(ctx, pveTimeout)
	defer cancel()
	nodes, err := c.Nodes(tctx)
	if err != nil {
		return Location{}, unreachable(err)
	}
	if len(nodes) == 0 {
		return Location{}, invalid("Proxmox reports no nodes")
	}
	if err := s.withTx(ctx, func(tx *ent.Client) error { return syncNodes(ctx, tx, id, nodes) }); err != nil {
		return Location{}, err
	}
	return s.GetLocation(ctx, id)
}

// syncNodes locks the location's nodes in name order first, the order servers.Create uses.
func syncNodes(ctx context.Context, tx *ent.Client, locationID int64, nodes []pve.Node) error {
	if _, err := tx.Node.Query().Where(node.LocationID(locationID)).Order(node.ByName()).ForUpdate().All(ctx); err != nil {
		return err
	}
	if err := upsertNodes(ctx, tx, locationID, nodes); err != nil {
		return err
	}
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return tx.Node.Update().Where(node.LocationID(locationID), node.NameNotIn(names...)).
		SetStatus(NodeMissing).Exec(ctx)
}

// upsertNodes inserts new nodes and refreshes stats of known ones, keeping enabled and the override.
func upsertNodes(ctx context.Context, tx *ent.Client, locationID int64, nodes []pve.Node) error {
	now := time.Now()
	nodes = slices.SortedFunc(slices.Values(nodes), func(a, b pve.Node) int { return strings.Compare(a.Name, b.Name) })
	for _, n := range nodes {
		err := tx.Node.Create().SetLocationID(locationID).SetName(n.Name).SetStatus(n.Status).SetMaxcpu(n.MaxCPU).
			SetMaxmem(n.MaxMem).SetCPU(n.CPU).SetMem(n.Mem).SetUpdatedAt(now).
			OnConflictColumns(node.FieldLocationID, node.FieldName).
			Update(func(u *ent.NodeUpsert) {
				u.UpdateStatus().UpdateMaxcpu().UpdateMaxmem().UpdateCPU().UpdateMem().UpdateUpdatedAt()
			}).Exec(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Nodes(ctx context.Context, locationID int64) ([]Node, error) {
	rows, err := s.db.Node.Query().Where(node.LocationID(locationID)).Order(node.ByName()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Node, len(rows))
	for i, r := range rows {
		out[i] = toNode(r)
	}
	return out, nil
}

// UpdateNode toggles a node or sets its disk storage override, which must hold images on that node.
func (s *Service) UpdateNode(ctx context.Context, locationID, nodeID int64, in NodeUpdate) (Node, error) {
	n, err := s.db.Node.Query().Where(node.ID(nodeID), node.LocationID(locationID)).Only(ctx)
	if err != nil {
		return Node{}, notFound(err)
	}
	u := n.Update()
	if in.Enabled != nil {
		u.SetEnabled(*in.Enabled)
	}
	if in.DiskStorageOverride != nil {
		st := strings.TrimSpace(*in.DiskStorageOverride)
		if st != "" {
			if err := s.checkNodeStorage(ctx, locationID, n.Name, st); err != nil {
				return Node{}, err
			}
		}
		u.SetDiskStorageOverride(st)
	}
	n, err = u.Save(ctx)
	if err != nil {
		return Node{}, notFound(err)
	}
	return toNode(n), nil
}

func (s *Service) checkNodeStorage(ctx context.Context, locationID int64, nodeName, storage string) error {
	c, err := s.ProxmoxFor(ctx, locationID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, pveTimeout)
	defer cancel()
	sts, err := c.Storages(ctx, nodeName)
	if err != nil {
		return unreachable(err)
	}
	for _, st := range sts {
		if st.ID == storage {
			if !slices.Contains(st.Content, "images") {
				return invalid("storage %q on %s does not allow the images content type", storage, nodeName)
			}
			return nil
		}
	}
	return invalid("storage %q does not exist on %s", storage, nodeName)
}

func (s *Service) openSecret(l *ent.Location) (string, error) {
	if s.boxErr != nil {
		return "", s.boxErr
	}
	b, err := s.box.Open(l.TokenSecret)
	if err != nil {
		return "", fmt.Errorf("location %d: %w", l.ID, err)
	}
	return string(b), nil
}

// ProxmoxFor returns a client for a location, cached until the location changes.
func (s *Service) ProxmoxFor(ctx context.Context, locationID int64) (pve.Client, error) {
	l, err := s.db.Location.Get(ctx, locationID)
	if err != nil {
		return nil, notFound(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[locationID]; ok && c.updated.Equal(l.UpdatedAt) {
		return c.client, nil
	}
	conn, err := s.connOf(l)
	if err != nil {
		return nil, err
	}
	c := s.opts.NewClient(conn)
	s.clients[locationID] = cachedClient{updated: l.UpdatedAt, client: c}
	return c, nil
}

// ConnFor returns a location's Proxmox connection settings, e.g. for pve.TLSConfig.
func (s *Service) ConnFor(ctx context.Context, locationID int64) (pve.Conn, error) {
	l, err := s.db.Location.Get(ctx, locationID)
	if err != nil {
		return pve.Conn{}, notFound(err)
	}
	return s.connOf(l)
}

func (s *Service) connOf(l *ent.Location) (pve.Conn, error) {
	secret, err := s.openSecret(l)
	if err != nil {
		return pve.Conn{}, err
	}
	return pve.Conn{BaseURL: l.PveURL, TokenID: l.TokenID, Secret: secret, CAFingerprint: l.CaFingerprint,
		InsecureTLS: l.InsecureTLS, PollInterval: s.opts.PollInterval}, nil
}

func (s *Service) forget(locationID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, locationID)
}
