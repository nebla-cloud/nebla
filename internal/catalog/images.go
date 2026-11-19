package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/image"
	"github.com/Hexeption/nebla/internal/ent/imagetemplate"
	"github.com/Hexeption/nebla/internal/pve"
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
	Templates   []ImageTemplate
	CreatedAt   time.Time
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

// bootDisks are checked in order to find a template's boot volume.
var bootDisks = []string{"scsi0", "virtio0", "sata0", "ide0"}

func templateNotFound(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "template_not_found", fmt.Sprintf(format, a...))
}

func templateNotShared(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "template_not_shared", fmt.Sprintf(format, a...))
}

func toImage(i *ent.Image) Image {
	out := Image{ID: i.ID, Name: i.Name, Description: i.Description, OSFlavor: i.OsFlavor, OSVersion: i.OsVersion,
		Templates: []ImageTemplate{}, CreatedAt: i.CreatedAt}
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
		if t.Node != "" {
			if vm.Node != t.Node {
				return templateNotFound("VMID %d is not a template on node %s", t.VMID, t.Node)
			}
			continue
		}
		cfg, err := c.Config(ctx, vm.Node, vm.VMID)
		if errors.Is(err, pve.ErrNotFound) {
			return templateNotFound("VMID %d is not a template in location %s", t.VMID, locName)
		}
		if err != nil {
			return unreachable(err)
		}
		st := bootStorage(cfg)
		if st == "" {
			return templateNotShared("VMID %d has no boot disk on shared storage; set node", t.VMID)
		}
		if _, ok := storages[vm.Node]; !ok {
			if storages[vm.Node], err = c.Storages(ctx, vm.Node); err != nil {
				return unreachable(err)
			}
		}
		j := slices.IndexFunc(storages[vm.Node], func(x pve.Storage) bool { return x.ID == st })
		if j < 0 || !storages[vm.Node][j].Shared {
			return templateNotShared("VMID %d is on storage %q, which is not shared; set node", t.VMID, st)
		}
	}
	return nil
}

// bootStorage returns the storage id of the first disk volume in bootDisks order.
func bootStorage(cfg map[string]string) string {
	for _, k := range bootDisks {
		v, ok := cfg[k]
		if !ok || strings.Contains(v, "media=cdrom") {
			continue
		}
		vol, _, _ := strings.Cut(v, ",")
		if st, _, ok := strings.Cut(vol, ":"); ok {
			return st
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
	if _, err := s.db.Image.Get(ctx, id); err != nil {
		return Image{}, notFound(err)
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
