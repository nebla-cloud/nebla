package osimages

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/catalogbuild"
	"github.com/Hexeption/nebla/internal/ent/imagetemplate"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Build steps, saved in catalog_builds.step once done.
const (
	stepResolve = iota + 1
	stepDownload
	stepAllocate
	stepCreate
	stepTemplate
	stepCleanup
	stepFinalize
)

var stepNames = map[int]string{stepResolve: "resolve", stepDownload: "download", stepAllocate: "allocate", stepCreate: "create",
	stepTemplate: "template", stepCleanup: "cleanup", stepFinalize: "finalize"}

const (
	// MaxAttempts is the logical retry cap; River gets spareAttempts more that only fail the row.
	MaxAttempts    = 5
	spareAttempts  = 2
	waitMargin     = 30 * time.Second
	buildTimeout   = 12 * time.Minute
	cleanupTimeout = 2 * time.Minute
	// cleanupPending prefixes a job error whose failure cleanup must run again.
	cleanupPending = "nebla: cleanup pending: "
	lockPoll       = 250 * time.Millisecond
	listPoll       = time.Second
	maxAllocations = 5
)

var (
	// waitBudget bounds one task wait; past it the job snoozes and resumes on the saved UPID.
	waitBudget = 10 * time.Minute
	snoozeFor  = 30 * time.Second
	// lockedForeignLimit is how long a locked untagged VM at new_vmid may block the build before it counts as foreign.
	lockedForeignLimit = time.Hour
)

// errStale means the row is no longer building under this job.
var errStale = errors.New("osimages: build row moved on")

type BuildWorker struct {
	river.WorkerDefaults[BuildArgs]
	s *Service
}

// Register adds the catalog build and retire workers.
func Register(w *river.Workers, s *Service) {
	river.AddWorker(w, &BuildWorker{s: s})
	river.AddWorker(w, &RetireWorker{s: s})
}

func (*BuildWorker) Timeout(*river.Job[BuildArgs]) time.Duration { return buildTimeout }

func (w *BuildWorker) Work(ctx context.Context, job *river.Job[BuildArgs]) error {
	row, err := w.s.o.Ent.CatalogBuild.Get(ctx, job.Args.BuildID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != catalogbuild.StatusBuilding || row.RiverJobID == nil || *row.RiverJobID != job.ID {
		return nil
	}
	b := &build{s: w.s, job: job.JobRow, row: row,
		log: w.s.o.Logger.With("build_id", row.ID, "catalog_key", row.CatalogKey, "job_id", job.ID, "attempt", job.Attempt)}
	b.entry, b.known = Lookup(w.s.o.Entries, row.CatalogKey)
	if code, msg, ok := pendingCleanup(job.JobRow); ok {
		return b.finish(ctx, code, msg, errors.New(msg))
	}
	if job.Attempt > MaxAttempts {
		msg := fmt.Sprintf("build did not finish after %d attempts", MaxAttempts)
		b.log.Error("catalog build attempts exhausted")
		return b.finish(ctx, "build_failed", msg, errors.New(msg))
	}
	err = b.run(ctx)
	var snooze *rivertype.JobSnoozeError
	switch {
	case err == nil, errors.Is(err, errStale):
		return nil
	case errors.As(err, &snooze), errors.Is(ctx.Err(), context.Canceled):
		return err
	}
	if (retryable(err) || errors.Is(ctx.Err(), context.DeadlineExceeded)) && job.Attempt < MaxAttempts {
		b.log.Warn("catalog build will retry", "err", err)
		return err
	}
	b.log.Error("catalog build failed", "err", err)
	code, msg := failure(err)
	return b.finish(ctx, code, msg, err)
}

func pendingCleanup(job *rivertype.JobRow) (code, msg string, ok bool) {
	n := len(job.Errors)
	if n == 0 {
		return "", "", false
	}
	rest, ok := strings.CutPrefix(job.Errors[n-1].Error, cleanupPending)
	if !ok {
		return "", "", false
	}
	code, msg, _ = strings.Cut(rest, ": ")
	return code, msg, true
}

func retryable(err error) bool {
	var te *transientError
	return pve.IsTransient(err) || db.IsTransient(err) || errors.As(err, &te)
}

// build is one attempt at one catalog_builds row.
type build struct {
	s     *Service
	job   *rivertype.JobRow
	row   *ent.CatalogBuild
	log   *slog.Logger
	entry Entry
	known bool
	loc   catalog.Location
	pc    pve.Client
	// node builds the template; disk is its disk storage.
	node, disk string
	res        *Resolved
}

func (b *build) run(ctx context.Context) error {
	if !b.known {
		return &stepError{stepNames[b.row.Step+1], permanent("this catalog entry was removed")}
	}
	if err := b.connect(ctx); err != nil {
		return &stepError{stepNames[b.row.Step+1], err}
	}
	for b.row.Step < stepFinalize {
		n := b.row.Step + 1
		err := b.do(ctx, n)
		var snooze *rivertype.JobSnoozeError
		var se *stepError
		switch {
		case err == nil:
		case errors.Is(err, errStale), errors.As(err, &snooze), errors.As(err, &se):
			return err
		default:
			return &stepError{stepNames[n], err}
		}
		if b.row.Status != catalogbuild.StatusBuilding {
			return nil
		}
		if b.s.afterStep != nil {
			b.s.afterStep(b.row.ID, b.row.Step)
		}
	}
	return nil
}

func (b *build) do(ctx context.Context, n int) error {
	switch n {
	case stepResolve:
		return b.resolve(ctx)
	case stepDownload:
		return b.download(ctx)
	case stepAllocate:
		return b.allocate(ctx)
	case stepCreate:
		return b.create(ctx)
	case stepTemplate:
		return b.template(ctx)
	case stepCleanup:
		if err := deleteVolume(ctx, b.pc, b.node, b.loc.ImportStorage, b.volid()); err != nil {
			return err
		}
		return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetStep(stepCleanup).ClearUpid() })
	case stepFinalize:
		return b.finalize(ctx)
	}
	return fmt.Errorf("unknown step %d", n)
}

// connect loads the location and client and picks the build node once per build; a shared row takes the first eligible node.
func (b *build) connect(ctx context.Context) error {
	if b.pc != nil {
		return nil
	}
	loc, err := b.s.o.Catalog.GetLocation(ctx, b.row.LocationID)
	if err != nil {
		return err
	}
	if loc.ImportStorage == "" {
		return permanent("%s", reasonNoImport)
	}
	pc, err := b.s.o.Catalog.ProxmoxFor(ctx, loc.ID)
	if err != nil {
		return err
	}
	node := b.row.Node
	if b.row.BuildNode != nil {
		node = *b.row.BuildNode
	} else if node == "" {
		av := b.s.availability(ctx, loc)
		i := slices.IndexFunc(av.targets, func(t target) bool { return t.Node == "" })
		switch {
		case i >= 0:
			node = av.targets[i].BuildNode
		case av.reason == reasonUnreachable:
			return transient("%s", reasonUnreachable)
		case av.reason != "":
			return permanent("%s", av.reason)
		default:
			return permanent("the location's disk storage is no longer shared")
		}
	}
	i := slices.IndexFunc(loc.Nodes, func(n catalog.Node) bool { return n.Name == node })
	if i < 0 {
		return permanent("node %s is no longer in the location", node)
	}
	b.loc, b.pc, b.node, b.disk = loc, pc, node, loc.Nodes[i].DiskStorage(loc)
	if b.row.BuildNode == nil {
		return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetBuildNode(node) })
	}
	return nil
}

// save updates the row only while this job still owns it, then reloads it.
func (b *build) save(ctx context.Context, f func(*ent.CatalogBuildUpdate)) error {
	u := b.s.o.Ent.CatalogBuild.Update().Where(catalogbuild.ID(b.row.ID), catalogbuild.StatusEQ(catalogbuild.StatusBuilding),
		catalogbuild.RiverJobID(b.job.ID))
	f(u)
	n, err := u.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errStale
	}
	b.row, err = b.s.o.Ent.CatalogBuild.Get(ctx, b.row.ID)
	return err
}

func (b *build) tag(nonce string) string { return templateTag(b.row.ID, nonce) }

func (b *build) volid() string {
	sum := *b.row.NewChecksum
	return fmt.Sprintf("%s:import/nebla-%s-%s-%d.%s", b.loc.ImportStorage, b.row.CatalogKey, sum[:min(12, len(sum))], b.row.ID, b.entry.Format)
}

// budgetFor is waitBudget, cut short so a wait ends before the job deadline.
func budgetFor(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		return min(waitBudget, time.Until(dl)-waitMargin)
	}
	return waitBudget
}

// wait waits on a task within waitBudget, snoozing the job when the budget runs out first.
func (b *build) wait(ctx context.Context, upid string) error {
	budget := budgetFor(ctx)
	if budget <= 0 {
		return river.JobSnooze(snoozeFor)
	}
	wctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	err := b.pc.WaitTask(wctx, upidNode(upid), pve.UPID(upid))
	if err != nil && wctx.Err() != nil && ctx.Err() == nil {
		b.log.Info("catalog build task still running; snoozing", "upid", upid)
		return river.JobSnooze(snoozeFor)
	}
	return err
}

func (b *build) resolve(ctx context.Context) error {
	r, err := Resolve(ctx, b.s.o.HTTP, b.entry)
	if err != nil {
		return &stepError{stepNames[stepResolve], err}
	}
	b.res = &r
	if b.row.Vmid != nil && b.row.Checksum != nil && *b.row.Checksum == r.Checksum {
		b.log.Info("catalog template already current")
		return b.save(ctx, func(u *ent.CatalogBuildUpdate) {
			u.SetStatus(catalogbuild.StatusReady).ClearErrorCode().ClearErrorMessage().ClearUpid().ClearBuildNode().
				ClearNewVmid().ClearNewNonce().ClearNewFile().ClearNewChecksum()
		})
	}
	return b.save(ctx, func(u *ent.CatalogBuildUpdate) {
		u.SetNewFile(r.File).SetNewChecksum(r.Checksum).SetStep(stepResolve).ClearUpid()
	})
}

func (b *build) download(ctx context.Context) error {
	if b.row.Upid == nil {
		// A resumed attempt has no resolved URL in memory, so it resolves again.
		if b.res == nil {
			if err := b.resolve(ctx); err != nil || b.row.Status != catalogbuild.StatusBuilding {
				return err
			}
		}
		imp, vol := b.loc.ImportStorage, b.volid()
		if err := deleteVolume(ctx, b.pc, b.node, imp, vol); err != nil {
			return err
		}
		up, err := b.pc.DownloadURL(ctx, b.node, imp, pve.DownloadParams{URL: b.res.URL, Filename: strings.TrimPrefix(vol, imp+":import/"),
			Content: "import", Checksum: *b.row.NewChecksum, Algorithm: b.res.Algorithm})
		if err != nil {
			return err
		}
		if err := b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetUpid(string(up)) }); err != nil {
			return err
		}
	}
	if err := b.wait(ctx, *b.row.Upid); err != nil {
		return err
	}
	return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetStep(stepDownload).ClearUpid() })
}

func (b *build) allocate(ctx context.Context) error {
	id, err := b.pc.NextID(ctx)
	if err != nil {
		return err
	}
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	return b.save(ctx, func(u *ent.CatalogBuildUpdate) {
		u.SetNewVmid(id).SetNewNonce(nonce).SetStep(stepAllocate).ClearUpid()
	})
}

func newNonce() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (b *build) createKV() map[string]string {
	kv := map[string]string{
		"name":    "nebla-" + b.row.CatalogKey,
		"ostype":  "l26",
		"scsihw":  "virtio-scsi-single",
		"scsi0":   b.disk + ":0,import-from=" + b.volid(),
		"boot":    "order=scsi0",
		"serial0": "socket",
		"agent":   "0",
		"tags":    TagTemplate + ";" + b.tag(*b.row.NewNonce),
	}
	if b.entry.CPU != "" {
		kv["cpu"] = b.entry.CPU
	}
	return kv
}

func (b *build) create(ctx context.Context) error {
	for range maxAllocations {
		err := b.createOnce(ctx)
		if !alreadyExists(err) {
			if err != nil {
				return err
			}
			return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetStep(stepCreate).ClearUpid() })
		}
		ours, err := b.claim(ctx)
		if err != nil {
			return err
		}
		if ours {
			return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetStep(stepCreate).ClearUpid() })
		}
		b.log.Info("vmid taken by another VM; allocating again", "vmid", *b.row.NewVmid)
		if err := b.allocate(ctx); err != nil {
			return err
		}
	}
	return transient("no free VMID after %d tries", maxAllocations)
}

func (b *build) createOnce(ctx context.Context) error {
	if b.row.Upid == nil {
		up, err := b.pc.CreateVM(ctx, b.node, *b.row.NewVmid, b.createKV())
		if err != nil {
			return err
		}
		if err := b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetUpid(string(up)) }); err != nil {
			return err
		}
	}
	return b.wait(ctx, *b.row.Upid)
}

func alreadyExists(err error) bool {
	var pe *pve.Error
	var te *pve.TaskError
	return (errors.As(err, &pe) || errors.As(err, &te)) && strings.Contains(err.Error(), "already exists")
}

// claim decides whether the VM at new_vmid is this build's: only its nonce tag proves it, read once no create holds it.
func (b *build) claim(ctx context.Context) (bool, error) {
	vmid, want := *b.row.NewVmid, b.tag(*b.row.NewNonce)
	deadline := time.Now().Add(budgetFor(ctx))
	for {
		v, found, err := findVM(ctx, b.pc, vmid)
		if err != nil {
			return false, err
		}
		if !found {
			return false, transient("VM %d reported as existing but not listed yet", vmid)
		}
		cfg, err := b.pc.Config(ctx, v.Node, vmid)
		if errors.Is(err, pve.ErrNotFound) {
			return false, transient("VM %d reported as existing but has no config yet", vmid)
		}
		if err != nil {
			return false, err
		}
		ours := hasTag(cfg["tags"], want)
		switch {
		case cfg["lock"] == "":
			return ours, nil
		case !ours && cfg["tags"] != "":
			return false, nil
		case time.Now().After(deadline):
			if !ours && b.lockedTooLong(ctx, vmid) {
				return false, nil
			}
			// Locked and ours or untagged: it may be our create, so keep new_vmid and come back.
			return false, river.JobSnooze(snoozeFor)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}

const lockedKey = "nebla_locked_untagged"

type lockedSeen struct {
	VMID  int       `json:"vmid"`
	Since time.Time `json:"since"`
}

// lockedTooLong reports whether vmid has been locked and untagged past lockedForeignLimit, noting when it was first seen.
func (b *build) lockedTooLong(ctx context.Context, vmid int) bool {
	var m map[string]json.RawMessage
	var seen lockedSeen
	if json.Unmarshal(b.job.Metadata, &m) == nil && json.Unmarshal(m[lockedKey], &seen) == nil && seen.VMID == vmid {
		return time.Since(seen.Since) > lockedForeignLimit
	}
	if err := river.MetadataSet(ctx, lockedKey, lockedSeen{VMID: vmid, Since: time.Now()}); err != nil {
		b.log.Warn("could not record locked vmid", "vmid", vmid, "err", err)
	}
	return false
}

func (b *build) template(ctx context.Context) error {
	if b.row.Upid == nil {
		vmid := *b.row.NewVmid
		v, found, err := findVM(ctx, b.pc, vmid)
		if err != nil {
			return err
		}
		if !found {
			return permanent("the new VM %d is gone", vmid)
		}
		cfg, err := b.pc.Config(ctx, v.Node, vmid)
		if err != nil {
			return err
		}
		if !hasTag(cfg["tags"], b.tag(*b.row.NewNonce)) {
			return permanent("VM %d does not carry this build's tag", vmid)
		}
		if cfg["template"] != "1" {
			up, err := b.pc.ConvertToTemplate(ctx, v.Node, vmid)
			if err != nil {
				return err
			}
			if up != "" {
				if err := b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetUpid(string(up)) }); err != nil {
					return err
				}
			}
		}
	}
	if b.row.Upid != nil {
		if err := b.wait(ctx, *b.row.Upid); err != nil {
			return err
		}
	}
	if err := b.awaitListed(ctx); err != nil {
		return err
	}
	return b.save(ctx, func(u *ent.CatalogBuildUpdate) { u.SetStep(stepTemplate).ClearUpid() })
}

// awaitListed waits until the cluster listing shows new_vmid as a template with this build's tag; server builds read that flag.
func (b *build) awaitListed(ctx context.Context) error {
	vmid, tag := *b.row.NewVmid, b.tag(*b.row.NewNonce)
	deadline := time.Now().Add(budgetFor(ctx))
	for {
		v, found, err := findVM(ctx, b.pc, vmid)
		if err != nil {
			return err
		}
		if !found {
			return permanent("the new VM %d is gone", vmid)
		}
		if !hasTag(v.Tags, tag) {
			return permanent("VM %d does not carry this build's tag", vmid)
		}
		if v.Template {
			return nil
		}
		if !time.Now().Add(listPoll).Before(deadline) {
			b.log.Info("template not listed yet; snoozing", "vmid", vmid)
			return river.JobSnooze(snoozeFor)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(listPoll):
		}
	}
}

// finalize registers the template and swaps it in, in one transaction, and retires the previous one.
func (b *build) finalize(ctx context.Context) error {
	j := b.s.jobs.Load()
	if j == nil {
		return transient("the job runtime is not running")
	}
	key := b.row.CatalogKey
	err := db.WithTx(ctx, b.s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		// Builds of one key on other nodes finalize under the same lock, so only one creates the image.
		if _, err := tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext('osimages:' || $1))`, key); err != nil {
			return err
		}
		row, err := etx.CatalogBuild.Query().Where(catalogbuild.ID(b.row.ID), catalogbuild.StatusEQ(catalogbuild.StatusBuilding),
			catalogbuild.RiverJobID(b.job.ID)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return errStale
		}
		if err != nil {
			return err
		}
		imgID, err := b.s.o.Catalog.CreateCatalogImage(ctx, etx, key, key, b.entry.Description, b.entry.OSFlavor, b.entry.OSVersion)
		if err != nil {
			return err
		}
		if err := etx.ImageTemplate.Create().SetImageID(imgID).SetLocationID(row.LocationID).SetNode(row.Node).SetVmid(*row.NewVmid).
			OnConflictColumns(imagetemplate.FieldImageID, imagetemplate.FieldLocationID, imagetemplate.FieldNode).UpdateVmid().
			Exec(ctx); err != nil {
			return err
		}
		if err := row.Update().SetStatus(catalogbuild.StatusReady).SetStep(stepFinalize).
			SetVmid(*row.NewVmid).SetNonce(*row.NewNonce).SetFile(*row.NewFile).SetChecksum(*row.NewChecksum).SetBuiltAt(time.Now()).
			ClearNewVmid().ClearNewNonce().ClearNewFile().ClearNewChecksum().ClearUpid().ClearErrorCode().ClearErrorMessage().
			ClearBuildNode().Exec(ctx); err != nil {
			return err
		}
		switch {
		case row.Vmid == nil || *row.Vmid == *row.NewVmid:
		case row.Nonce == nil:
			b.log.Warn("previous template has no nonce; not retiring it", "vmid", *row.Vmid)
		default:
			args := RetireArgs{LocationID: row.LocationID, Node: row.Node, VMID: *row.Vmid, Tag: templateTag(row.ID, *row.Nonce)}
			if _, err := j.InsertTx(ctx, tx, args, retireOpts()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.row, err = b.s.o.Ent.CatalogBuild.Get(ctx, b.row.ID)
	return err
}

// finish cleans up a failed build and fails the row, or leaves the cleanup for the next attempt.
func (b *build) finish(ctx context.Context, code, msg string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	last := b.job.Attempt >= b.job.MaxAttempts
	if err := b.cleanup(ctx); err != nil {
		b.log.Error("catalog build cleanup failed", "err", err, "new_vmid", b.row.NewVmid)
		if !last {
			return errors.New(cleanupPending + code + ": " + msg)
		}
	}
	err := b.save(ctx, func(u *ent.CatalogBuildUpdate) {
		u.SetStatus(catalogbuild.StatusFailed).SetErrorCode(code).SetErrorMessage(msg).
			ClearNewVmid().ClearNewNonce().ClearNewFile().ClearNewChecksum().ClearUpid().ClearBuildNode()
	})
	switch {
	case errors.Is(err, errStale):
		return nil
	case err != nil && !last:
		return errors.New(cleanupPending + code + ": " + msg)
	case err != nil:
		return err
	}
	return river.JobCancel(cause)
}

// cleanup destroys the partial VM only if it carries this build's nonce tag, and deletes the import volume.
func (b *build) cleanup(ctx context.Context) error {
	if b.row.NewVmid == nil && b.row.NewChecksum == nil {
		return nil
	}
	if err := b.connect(ctx); err != nil {
		return err
	}
	if b.row.Upid != nil {
		// Let a running create or download settle before acting on what it makes.
		_ = b.pc.WaitTask(ctx, upidNode(*b.row.Upid), pve.UPID(*b.row.Upid))
	}
	if b.row.NewVmid != nil && b.row.NewNonce != nil {
		if err := b.destroy(ctx, *b.row.NewVmid, b.tag(*b.row.NewNonce)); err != nil {
			return err
		}
	}
	if b.row.NewChecksum != nil && b.known {
		return deleteVolume(ctx, b.pc, b.node, b.loc.ImportStorage, b.volid())
	}
	return nil
}

func (b *build) destroy(ctx context.Context, vmid int, tag string) error {
	v, found, err := findVM(ctx, b.pc, vmid)
	if err != nil || !found {
		return err
	}
	cfg, err := b.pc.Config(ctx, v.Node, vmid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !hasTag(cfg["tags"], tag) {
		return nil
	}
	if cfg["lock"] != "" {
		return fmt.Errorf("vm %d is locked (%s)", vmid, cfg["lock"])
	}
	up, err := b.pc.Destroy(ctx, v.Node, vmid)
	if err == nil {
		err = b.pc.WaitTask(ctx, v.Node, up)
	}
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	return err
}

func deleteVolume(ctx context.Context, pc pve.Client, node, storage, volid string) error {
	if err := pc.DeleteVolume(ctx, node, storage, volid); err != nil && !errors.Is(err, pve.ErrNotFound) {
		return err
	}
	return nil
}

func findVM(ctx context.Context, pc pve.Client, vmid int) (pve.VM, bool, error) {
	vms, err := pc.VMs(ctx)
	if err != nil {
		return pve.VM{}, false, err
	}
	i := slices.IndexFunc(vms, func(v pve.VM) bool { return v.VMID == vmid })
	if i < 0 {
		return pve.VM{}, false, nil
	}
	return vms[i], true, nil
}

func hasTag(tags, want string) bool { return slices.Contains(servers.SplitTags(tags), want) }

// upidNode is the node field of a UPID ("UPID:node:...").
func upidNode(upid string) string {
	parts := strings.SplitN(upid, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// permanentError fails the build without retry; msg is stored on the row.
type permanentError struct{ msg string }

func (e *permanentError) Error() string { return e.msg }

func permanent(format string, a ...any) error { return &permanentError{fmt.Sprintf(format, a...)} }

type transientError struct{ msg string }

func (e *transientError) Error() string { return e.msg }

func transient(format string, a ...any) error { return &transientError{fmt.Sprintf(format, a...)} }

// stepError names the step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"]+`)

const downloadForbidden = "permission denied; the API token needs Datastore.AllocateTemplate and Datastore.Allocate on the import storage and Sys.AccessNetwork on the node"

// failure is the row's error code and a message free of URLs and Proxmox transport details.
func failure(err error) (code, msg string) {
	step, inner := "", err
	var se *stepError
	if errors.As(err, &se) {
		step, inner = se.step, se.err
	}
	code = "build_failed"
	switch {
	case errors.Is(inner, ErrUpstream):
		code = "upstream_unavailable"
	case step == stepNames[stepDownload]:
		code = "download_failed"
	}
	msg = "unexpected error"
	var pe *pve.Error
	var te *pve.TaskError
	var perm *permanentError
	var tr *transientError
	var ae apierr.E
	var ue *url.Error
	var ne net.Error
	switch {
	case errors.Is(inner, ErrUpstream):
		msg = "could not fetch the image checksum list from upstream"
	case errors.As(inner, &perm):
		msg = perm.msg
	case errors.As(inner, &tr):
		msg = tr.msg
	case errors.As(inner, &ae):
		msg = ae.Msg
	case errors.As(inner, &te):
		msg = te.ExitStatus
	case errors.As(inner, &pe) && pe.Status == http.StatusForbidden && code == "download_failed":
		msg = downloadForbidden
	case errors.As(inner, &pe):
		msg = pe.Message
	case errors.As(inner, &ue), errors.As(inner, &ne), errors.Is(inner, context.DeadlineExceeded):
		msg = "could not reach Proxmox"
	}
	msg = urlPattern.ReplaceAllString(msg, "[url]")
	if step != "" {
		msg = step + ": " + msg
	}
	return code, msg
}
