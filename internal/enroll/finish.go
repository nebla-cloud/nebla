package enroll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/locationenrollment"
	"github.com/nebla-cloud/nebla/internal/pve"
)

type FinishInput struct {
	URL, DiskStorage, ISOStorage, ImportStorage string
}

// Finished is the connected location plus optional pveum commands for the admin to run.
type Finished struct {
	Location catalog.Location
	Cleanup  []string
}

// pveTokenNameRe is PVE's token name pattern; other old names get no cleanup command.
var pveTokenNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

func validStorages(rep Report, in FinishInput) error {
	find := func(id string) (ReportStorage, bool) {
		i := slices.IndexFunc(rep.Storages, func(s ReportStorage) bool { return s.ID == id })
		if i < 0 {
			return ReportStorage{}, false
		}
		return rep.Storages[i], true
	}
	if st, ok := find(in.DiskStorage); !ok || !slices.Contains(st.Content, "images") {
		return invalidInput(fmt.Sprintf("disk_storage %q is not a reported storage with images content", in.DiskStorage))
	}
	if st, ok := find(in.ISOStorage); !ok || !st.NeblaISO || !slices.Contains(st.Content, "iso") {
		return invalidInput(fmt.Sprintf("iso_storage %q is not a reported Nebla ISO storage with iso content", in.ISOStorage))
	}
	if in.ImportStorage != "" {
		if st, ok := find(in.ImportStorage); !ok || !st.NeblaISO || !slices.Contains(st.Content, "import") {
			return invalidInput(fmt.Sprintf("import_storage %q is not a reported Nebla ISO storage with import content", in.ImportStorage))
		}
	}
	return nil
}

func finishURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return nil, invalidInput("url must be a base URL like https://host:8006")
	}
	return u, nil
}

// Finish pins the node certificate and creates or updates the location; the row survives any failure.
func (s *Service) Finish(ctx context.Context, id int64, in FinishInput) (Finished, error) {
	if s.boxErr != nil {
		return Finished{}, s.boxErr
	}
	in.DiskStorage, in.ISOStorage = strings.TrimSpace(in.DiskStorage), strings.TrimSpace(in.ISOStorage)
	in.ImportStorage = strings.TrimSpace(in.ImportStorage)
	r, err := s.db.LocationEnrollment.Get(ctx, id)
	if err != nil {
		return Finished{}, notFound(err)
	}
	if r.State != locationenrollment.StateReported {
		return Finished{}, stateErr("the node has not reported yet")
	}
	if !s.now().Before(r.ExpiresAt) {
		return Finished{}, errExpired
	}
	var rep Report
	if err := json.Unmarshal(r.Report, &rep); err != nil {
		return Finished{}, fmt.Errorf("enroll: stored report: %w", err)
	}
	u, err := finishURL(in.URL)
	if err != nil {
		return Finished{}, err
	}
	if err := validStorages(rep, in); err != nil {
		return Finished{}, err
	}
	seen, err := s.o.LeafFingerprint(ctx, u.String())
	if err != nil {
		return Finished{}, apierr.New(http.StatusUnprocessableEntity, "pve_unreachable",
			"cannot read the certificate at "+u.Host+": "+err.Error())
	}
	seen = pve.NormFP(seen)
	if !slices.ContainsFunc(rep.Nodes, func(n ReportNode) bool { return n.Fingerprint == seen }) {
		return Finished{}, apierr.New(http.StatusUnprocessableEntity, "fingerprint_mismatch",
			fmt.Sprintf("the certificate at %s (%s) is not one the node reported", u.Host, seen))
	}
	secret, err := s.box.Open(r.TokenSecret)
	if err != nil {
		return Finished{}, err
	}
	sec := string(secret)
	li := catalog.LocationInput{Name: r.Name, Description: r.Description, DiskStorage: in.DiskStorage,
		ISOStorage: in.ISOStorage, ImportStorage: in.ImportStorage,
		PVE: catalog.PVEInput{URL: u.String(), TokenID: r.TokenID, TokenSecret: &sec, CAFingerprint: seen}}
	var cleanup []string
	for _, st := range rep.Storages {
		if st.NeblaISO && st.ID != in.ISOStorage && st.ID != in.ImportStorage {
			cleanup = append(cleanup, "pveum acl delete /storage/"+st.ID+" --users nebla@pve --roles NeblaISO")
		}
	}
	var loc catalog.Location
	if r.LocationID == nil {
		loc, err = s.o.Catalog.CreateLocation(ctx, li)
	} else {
		var cur catalog.Location
		if cur, err = s.o.Catalog.GetLocation(ctx, *r.LocationID); err != nil {
			return Finished{}, err
		}
		li.Name, li.Description = cur.Name, cur.Description
		if loc, err = s.o.Catalog.UpdateLocation(ctx, cur.ID, li); err == nil {
			if old, ok := strings.CutPrefix(cur.TokenID, "nebla@pve!"); ok && cur.TokenID != r.TokenID && pveTokenNameRe.MatchString(old) {
				cleanup = append(cleanup, "pveum user token remove nebla@pve "+old)
			}
		}
	}
	if err != nil {
		return Finished{}, err
	}
	// The location is done; a leftover row is reclaimed by the sweep.
	if err := s.db.LocationEnrollment.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		s.o.Logger.ErrorContext(ctx, "enroll: delete finished enrollment", "enrollment_id", id, "err", err)
	}
	return Finished{Location: loc, Cleanup: cleanup}, nil
}
