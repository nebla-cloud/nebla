package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/image"
	"github.com/nebla-cloud/nebla/internal/ent/imagetemplate"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// Template is a Proxmox template to clone from; Node "" means any node of the location.
type Template struct {
	Node string
	VMID int
}

// ImageTemplate places an image's template in a location; Node is "" for shared storage.
type ImageTemplate struct {
	LocationID int64
	Node       string
	VMID       int
}

type TemplateInput = ImageTemplate

type Image struct {
	ID          int64
	Name        string
	Description string
	OSFlavor    string
	OSVersion   string
	// CatalogKey is "" for hand-registered images.
	CatalogKey string
	Templates  []ImageTemplate
	CreatedAt  time.Time
}

// Locations lists the locations the image has a template in.
func (i Image) Locations() []int64 {
	out := []int64{}
	for _, t := range i.Templates {
		out = append(out, t.LocationID)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

type ImageInput struct {
	Name        string
	Description string
	OSFlavor    string
	OSVersion   string
	Templates   []TemplateInput
}

var imageNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var (
	volumeKeyRE = regexp.MustCompile(`^((scsi|virtio|sata|ide)\d+|efidisk0|tpmstate0|unused\d+)$`)
	busKeyRE    = regexp.MustCompile(`^(scsi|virtio|sata|ide)\d+$`)
	// bootFallback is tried in order when the config has no usable boot order.
	bootFallback = []string{"scsi0", "virtio0", "sata0", "ide0"}
)

func templateNotFound(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "template_not_found", fmt.Sprintf(format, a...))
}

func templateNotShared(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "template_not_shared", fmt.Sprintf(format, a...))
}

func toImage(i *ent.Image) Image {
	out := Image{ID: i.ID, Name: i.Name, Description: i.Description, OSFlavor: i.OsFlavor, OSVersion: i.OsVersion,
		Templates: []ImageTemplate{}, CreatedAt: i.CreatedAt}
	if i.CatalogKey != nil {
		out.CatalogKey = *i.CatalogKey
	}
	for _, t := range i.Edges.Templates {
		out.Templates = append(out.Templates, ImageTemplate{LocationID: t.LocationID, Node: t.Node, VMID: t.Vmid})
	}
	return out
}

func withTemplates(q *ent.ImageTemplateQuery) {
	q.Order(imagetemplate.ByLocationID(), imagetemplate.ByNode())
}

func validImage(in *ImageInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.OSFlavor = strings.TrimSpace(in.OSFlavor)
	in.OSVersion = strings.TrimSpace(in.OSVersion)
	switch {
	case !imageNameRE.MatchString(in.Name):
		return invalid("name must match %s", imageNameRE)
	case len(in.Description) > 500:
		return invalid("description must be at most 500 characters")
	case in.OSFlavor == "" || len(in.OSFlavor) > 32:
		return invalid("os_flavor must be 1-32 characters")
	case in.OSVersion == "" || len(in.OSVersion) > 32:
		return invalid("os_version must be 1-32 characters")
	case len(in.Templates) == 0:
		return invalid("templates needs at least one entry")
	}
	type key struct {
		loc  int64
		node string
	}
	seen := map[key]bool{}
	for i := range in.Templates {
		t := &in.Templates[i]
		t.Node = strings.TrimSpace(t.Node)
		if t.VMID < 1 {
			return invalid("templates[%d].vmid must be positive", i)
		}
		k := key{t.LocationID, t.Node}
		if seen[k] {
			return invalid("templates has two entries for location %d node %q", t.LocationID, t.Node)
		}
		seen[k] = true
	}
	return nil
}

// checkTemplates verifies each VMID is a template, and that node-less ones sit on shared storage.
func (s *Service) checkTemplates(ctx context.Context, tmpls []TemplateInput) error {
	byLoc := map[int64][]TemplateInput{}
	for _, t := range tmpls {
		byLoc[t.LocationID] = append(byLoc[t.LocationID], t)
	}
	for locID, ts := range byLoc {
		loc, err := s.db.Location.Get(ctx, locID)
		if ent.IsNotFound(err) {
			return invalid("location %d does not exist", locID)
		}
		if err != nil {
			return err
		}
		c, err := s.ProxmoxFor(ctx, locID)
		if err != nil {
			return err
		}
		if err := checkLocationTemplates(ctx, c, loc.Name, ts); err != nil {
			return err
		}
	}
	return nil
}

func checkLocationTemplates(ctx context.Context, c pve.Client, locName string, ts []TemplateInput) error {
	ctx, cancel := context.WithTimeout(ctx, pveTimeout)
	defer cancel()
	vms, err := c.VMs(ctx)
	if err != nil {
		return unreachable(err)
	}
	storages := map[string][]pve.Storage{}
	for _, t := range ts {
		i := slices.IndexFunc(vms, func(v pve.VM) bool { return v.VMID == t.VMID })
		if i < 0 || !vms[i].Template {
			return templateNotFound("VMID %d is not a template in location %s", t.VMID, locName)
		}
		vm := vms[i]
		if t.Node != "" && vm.Node != t.Node {
			return templateNotFound("VMID %d is not a template on node %s", t.VMID, t.Node)
		}
		cfg, err := c.Config(ctx, vm.Node, vm.VMID)
		if errors.Is(err, pve.ErrNotFound) {
			return templateNotFound("VMID %d is not a template in location %s", t.VMID, locName)
		}
		if err != nil {
			return unreachable(err)
		}
		vols := templateVolumes(cfg)
		if len(vols) == 0 {
			return templateNotFound("VMID %d has no disks", t.VMID)
		}
		if t.Node != "" {
			continue
		}
		if _, ok := storages[vm.Node]; !ok {
			if storages[vm.Node], err = c.Storages(ctx, vm.Node); err != nil {
				return unreachable(err)
			}
		}
		for _, v := range vols {
			if v.Storage == "" {
				return templateNotShared("VMID %d volume %s is not on a Proxmox storage; set node", t.VMID, v.Key)
			}
			j := slices.IndexFunc(storages[vm.Node], func(x pve.Storage) bool { return x.ID == v.Storage })
			if j < 0 || !storages[vm.Node][j].Shared {
				return templateNotShared("VMID %d volume %s is on storage %q, which is not shared; set node", t.VMID, v.Key, v.Storage)
			}
		}
	}
	return nil
}

type volume struct{ Key, Storage string }

// volumeOf splits a drive value into its volume id and whether it is a disk (not an ISO, empty or physical drive).
func volumeOf(v string) (string, bool) {
	vol, _, _ := strings.Cut(v, ",")
	_, path, _ := strings.Cut(vol, ":")
	return vol, vol != "" && vol != "none" && vol != "cdrom" && !strings.HasPrefix(path, "iso/")
}

// templateVolumes lists every disk-like volume in cfg, cloud-init drives included, sorted by key.
func templateVolumes(cfg map[string]string) []volume {
	var out []volume
	for k, v := range cfg {
		if !volumeKeyRE.MatchString(k) {
			continue
		}
		vol, ok := volumeOf(v)
		if !ok {
			continue
		}
		st, _, found := strings.Cut(vol, ":")
		if !found {
			st = ""
		}
		out = append(out, volume{Key: k, Storage: st})
	}
	slices.SortFunc(out, func(a, b volume) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func isBootDisk(cfg map[string]string, key string) bool {
	v, ok := cfg[key]
	if !ok || !busKeyRE.MatchString(key) || strings.Contains(v, "media=cdrom") {
		return false
	}
	_, disk := volumeOf(v)
	return disk
}

// BootDisk returns the boot disk key (e.g. "scsi0") from boot order=..., else the first of scsi0/virtio0/sata0/ide0; "" if none.
func BootDisk(cfg map[string]string) string {
	for _, part := range strings.Split(cfg["boot"], ",") {
		order, ok := strings.CutPrefix(strings.TrimSpace(part), "order=")
		if !ok {
			continue
		}
		for _, dev := range strings.Split(order, ";") {
			if dev = strings.TrimSpace(dev); isBootDisk(cfg, dev) {
				return dev
			}
		}
	}
	for _, k := range bootFallback {
		if isBootDisk(cfg, k) {
			return k
		}
	}
	return ""
}

func (s *Service) CreateImage(ctx context.Context, in ImageInput) (Image, error) {
	if err := validImage(&in); err != nil {
		return Image{}, err
	}
	if err := s.checkTemplates(ctx, in.Templates); err != nil {
		return Image{}, err
	}
	var id int64
	err := s.withTx(ctx, func(tx *ent.Client) error {
		img, err := tx.Image.Create().SetName(in.Name).SetDescription(in.Description).
			SetOsFlavor(in.OSFlavor).SetOsVersion(in.OSVersion).Save(ctx)
		if err != nil {
			return mapWriteErr(err, "image")
		}
		id = img.ID
		return insertTemplates(ctx, tx, id, in.Templates)
	})
	if err != nil {
		return Image{}, err
	}
	return s.GetImage(ctx, id)
}

// UpdateImage replaces the image's fields and templates.
func (s *Service) UpdateImage(ctx context.Context, id int64, in ImageInput) (Image, error) {
	if err := validImage(&in); err != nil {
		return Image{}, err
	}
	if err := s.guardManual(ctx, id); err != nil {
		return Image{}, err
	}
	if err := s.checkTemplates(ctx, in.Templates); err != nil {
		return Image{}, err
	}
	err := s.withTx(ctx, func(tx *ent.Client) error {
		if _, err := tx.Image.Query().Where(image.ID(id)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		err := tx.Image.UpdateOneID(id).SetName(in.Name).SetDescription(in.Description).
			SetOsFlavor(in.OSFlavor).SetOsVersion(in.OSVersion).Exec(ctx)
		if err != nil {
			return mapWriteErr(err, "image")
		}
		if _, err := tx.ImageTemplate.Delete().Where(imagetemplate.ImageID(id)).Exec(ctx); err != nil {
			return err
		}
		return insertTemplates(ctx, tx, id, in.Templates)
	})
	if err != nil {
		return Image{}, err
	}
	return s.GetImage(ctx, id)
}

func insertTemplates(ctx context.Context, tx *ent.Client, imageID int64, ts []TemplateInput) error {
	b := make([]*ent.ImageTemplateCreate, len(ts))
	for i, t := range ts {
		b[i] = tx.ImageTemplate.Create().SetImageID(imageID).SetLocationID(t.LocationID).SetNode(t.Node).SetVmid(t.VMID)
	}
	if err := tx.ImageTemplate.CreateBulk(b...).Exec(ctx); err != nil {
		if pgCode(err) == "23503" {
			return invalid("a template location no longer exists")
		}
		return err
	}
	return nil
}

func (s *Service) GetImage(ctx context.Context, id int64) (Image, error) {
	i, err := s.db.Image.Query().Where(image.ID(id)).WithTemplates(withTemplates).Only(ctx)
	if err != nil {
		return Image{}, notFound(err)
	}
	return toImage(i), nil
}

// ImageByRef finds an image by numeric id or by name.
func (s *Service) ImageByRef(ctx context.Context, ref string) (Image, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		if i, err := s.GetImage(ctx, id); err == nil {
			return i, nil
		}
	}
	i, err := s.db.Image.Query().Where(image.Name(ref)).WithTemplates(withTemplates).Only(ctx)
	if err != nil {
		return Image{}, notFound(err)
	}
	return toImage(i), nil
}

// ListImages lists all images, or with locationID != 0 only those with a template there.
func (s *Service) ListImages(ctx context.Context, locationID int64, page, perPage int) ([]Image, int, error) {
	q := s.db.Image.Query()
	if locationID != 0 {
		q = q.Where(image.HasTemplatesWith(imagetemplate.LocationID(locationID)))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	off, lim := pageBounds(page, perPage)
	rows, err := q.Order(image.ByID()).Offset(off).Limit(lim).WithTemplates(withTemplates).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Image, len(rows))
	for i, r := range rows {
		out[i] = toImage(r)
	}
	return out, total, nil
}

// DeleteImage fails with resource_in_use once servers reference the image.
func (s *Service) DeleteImage(ctx context.Context, id int64) error {
	if err := s.guardManual(ctx, id); err != nil {
		return err
	}
	return mapWriteErr(s.db.Image.DeleteOneID(id).Exec(ctx), "image")
}

// TemplatesFor returns an image's templates in a location, shared ones first; empty when it has none there.
func (s *Service) TemplatesFor(ctx context.Context, imageID, locationID int64) ([]Template, error) {
	if _, err := s.db.Image.Get(ctx, imageID); err != nil {
		return nil, notFound(err)
	}
	rows, err := s.db.ImageTemplate.Query().
		Where(imagetemplate.ImageID(imageID), imagetemplate.LocationID(locationID)).
		Order(imagetemplate.ByNode()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Template, len(rows))
	for i, r := range rows {
		out[i] = Template{Node: r.Node, VMID: r.Vmid}
	}
	return out, nil
}

// ErrCatalogManaged is returned when a hand edit targets a catalog-owned image.
var ErrCatalogManaged = apierr.New(http.StatusConflict, "catalog_managed", "this image is managed by the OS catalog; use the catalog to change it")

// guardManual returns not_found for a missing image and ErrCatalogManaged for a catalog-owned one.
func (s *Service) guardManual(ctx context.Context, id int64) error {
	img, err := s.db.Image.Get(ctx, id)
	if err != nil {
		return notFound(err)
	}
	if img.CatalogKey != nil {
		return ErrCatalogManaged
	}
	return nil
}

// CreateCatalogImage creates the catalog-owned image row, or returns the existing id for key.
func (s *Service) CreateCatalogImage(ctx context.Context, tx *ent.Client, key, name, desc, flavor, version string) (int64, error) {
	if img, err := tx.Image.Query().Where(image.CatalogKey(key)).Only(ctx); err == nil {
		return img.ID, nil
	} else if !ent.IsNotFound(err) {
		return 0, err
	}
	img, err := tx.Image.Create().SetName(name).SetDescription(desc).SetOsFlavor(flavor).SetOsVersion(version).
		SetCatalogKey(key).Save(ctx)
	if err != nil {
		if pgCode(err) == "23505" {
			return 0, apierr.New(http.StatusConflict, "conflict", fmt.Sprintf("an image named %q already exists", name))
		}
		return 0, err
	}
	return img.ID, nil
}
