// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package enroll

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/crypto"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/locationenrollment"
	"github.com/nebla-cloud/nebla/internal/pve"
)

const (
	waitingTTL  = 30 * time.Minute
	reportedTTL = 24 * time.Hour
	// sweepAfter is how long past expires_at a row lingers before Sweep deletes it.
	sweepAfter = 24 * time.Hour
)

// nameRE matches catalog location names.
var nameRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

type Options struct {
	SecretKey []byte
	// BaseURL is the panel URL the setup script calls back.
	BaseURL string
	Catalog *catalog.Service
	// LeafFingerprint defaults to pve.LeafFingerprint.
	LeafFingerprint func(ctx context.Context, url string) (string, error)
	// Now defaults to time.Now.
	Now func() time.Time
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

type Service struct {
	sql    *sql.DB
	db     *ent.Client
	box    *crypto.Box
	boxErr error
	urlErr error
	o      Options
}

// Enrollment is a row as the admin sees it; Report never carries the secret.
type Enrollment struct {
	ID                                  int64
	Name, Description, TokenName, State string
	LocationID                          *int64
	ExpiresAt, CreatedAt                time.Time
	Expired                             bool
	Report                              *Report
}

// Created carries the plaintext token, shown once.
type Created struct {
	Enrollment
	Token, Command, OfflineScript string
}

// CreateInput is a new location (Name) or a Re-run (LocationID).
type CreateInput struct {
	Name, Description string
	LocationID        *int64
}

func New(sqlDB *sql.DB, client *ent.Client, o Options) *Service {
	if o.LeafFingerprint == nil {
		o.LeafFingerprint = pve.LeafFingerprint
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	box, err := crypto.NewBox(o.SecretKey)
	var urlErr error
	if !panelURLRe.MatchString(o.BaseURL) {
		urlErr = apierr.New(http.StatusInternalServerError, "internal_error",
			"the panel base URL is not set or not usable for enrollment; check PANEL_BASE_URL")
	}
	return &Service{sql: sqlDB, db: client, box: box, boxErr: err, urlErr: urlErr, o: o}
}

var (
	errInvalid  = apierr.New(http.StatusUnauthorized, "enrollment_invalid", "enrollment token is not valid")
	errExpired  = apierr.New(http.StatusGone, "enrollment_expired", "enrollment has expired")
	errUsed     = apierr.New(http.StatusConflict, "enrollment_used", "enrollment has already been reported")
	errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNameUsed = apierr.New(http.StatusConflict, "name_taken", "a location or enrollment already uses this name")
)

func stateErr(msg string) error { return apierr.New(http.StatusConflict, "enrollment_state", msg) }

func invalidInput(msg string) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", msg)
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

// uniqueKey returns the violated constraint of a unique violation, or "".
func uniqueKey(err error) (string, bool) {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return pe.ConstraintName, true
	}
	return "", false
}

func (s *Service) now() time.Time { return s.o.Now() }

func hashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "nbe_" + base64.RawURLEncoding.EncodeToString(b)
}

const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func newTokenName(name string) string {
	out := make([]byte, 0, 6)
	var b [1]byte
	for len(out) < 6 {
		rand.Read(b[:])
		// Rejection keeps the 36 characters uniform.
		if b[0] < 252 {
			out = append(out, suffixChars[int(b[0])%len(suffixChars)])
		}
	}
	return "nebla-" + name + "-" + string(out)
}

func (s *Service) toEnrollment(r *ent.LocationEnrollment) Enrollment {
	e := Enrollment{ID: r.ID, Name: r.Name, Description: r.Description, TokenName: r.TokenName, State: string(r.State),
		LocationID: r.LocationID, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt, Expired: !s.now().Before(r.ExpiresAt)}
	if len(r.Report) > 0 {
		var rep Report
		if json.Unmarshal(r.Report, &rep) == nil {
			e.Report = &rep
		}
	}
	return e
}

func (s *Service) created(r *ent.LocationEnrollment, tok string) Created {
	return Created{Enrollment: s.toEnrollment(r), Token: tok, Command: Command(s.o.BaseURL, tok),
		OfflineScript: OfflineScript(s.o.BaseURL, tok, r.TokenName)}
}

func (s *Service) withTx(ctx context.Context, fn func(tx *ent.Client) error) error {
	return db.WithTx(ctx, s.sql, func(_ *sql.Tx, etx *ent.Client) error { return fn(etx) })
}

// Create starts an enrollment for a new location or a Re-run of an existing one.
func (s *Service) Create(ctx context.Context, in CreateInput, userID int64) (Created, error) {
	if s.urlErr != nil {
		return Created{}, s.urlErr
	}
	name, desc := strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.LocationID != nil {
		loc, err := s.o.Catalog.GetLocation(ctx, *in.LocationID)
		if err != nil {
			return Created{}, err
		}
		name, desc = loc.Name, loc.Description
	} else {
		if !nameRE.MatchString(name) {
			return Created{}, invalidInput("name must match " + nameRE.String())
		}
		if len(desc) > 500 {
			return Created{}, invalidInput("description must be at most 500 characters")
		}
	}
	tok := newToken()
	now := s.now()
	var row *ent.LocationEnrollment
	err := s.withTx(ctx, func(tx *ent.Client) error {
		if in.LocationID != nil {
			// One live Re-run per location: the new one replaces it unless it awaits Finish.
			prev, err := tx.LocationEnrollment.Query().Where(locationenrollment.LocationID(*in.LocationID)).ForUpdate().Only(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			if prev != nil && prev.State == locationenrollment.StateReported && now.Before(prev.ExpiresAt) {
				return stateErr("a re-run for this location is waiting to be finished; finish or cancel it first")
			}
			if _, err := tx.LocationEnrollment.Delete().Where(locationenrollment.LocationID(*in.LocationID)).Exec(ctx); err != nil {
				return err
			}
		} else {
			taken, err := tx.Location.Query().Where(location.Name(name)).Exist(ctx)
			if err != nil {
				return err
			}
			if taken {
				return errNameUsed
			}
			// An expired new-enrollment no longer holds its name.
			if _, err := tx.LocationEnrollment.Delete().Where(locationenrollment.Name(name), locationenrollment.LocationIDIsNil(),
				locationenrollment.ExpiresAtLTE(now)).Exec(ctx); err != nil {
				return err
			}
		}
		c := tx.LocationEnrollment.Create().SetName(name).SetDescription(desc).SetTokenName(newTokenName(name)).
			SetTokenHash(hashToken(tok)).SetExpiresAt(now.Add(waitingTTL)).SetCreatedBy(userID).SetCreatedAt(now)
		if in.LocationID != nil {
			c.SetLocationID(*in.LocationID)
		}
		var err error
		if row, err = c.Save(ctx); err != nil {
			if key, ok := uniqueKey(err); ok {
				if key == "location_enrollments_location_id_key" {
					return stateErr("a re-run for this location is already in progress")
				}
				return errNameUsed
			}
			return notFound(err)
		}
		return nil
	})
	if err != nil {
		return Created{}, err
	}
	return s.created(row, tok), nil
}

// NewToken replaces a waiting enrollment's token, token name and expiry.
func (s *Service) NewToken(ctx context.Context, id int64) (Created, error) {
	if s.urlErr != nil {
		return Created{}, s.urlErr
	}
	tok := newToken()
	var row *ent.LocationEnrollment
	err := s.withTx(ctx, func(tx *ent.Client) error {
		r, err := tx.LocationEnrollment.Query().Where(locationenrollment.ID(id)).ForUpdate().Only(ctx)
		if err != nil {
			return notFound(err)
		}
		if r.State != locationenrollment.StateWaiting {
			return stateErr("a new command can only be issued while waiting for the node")
		}
		row, err = tx.LocationEnrollment.UpdateOne(r).SetTokenHash(hashToken(tok)).SetTokenName(newTokenName(r.Name)).
			SetExpiresAt(s.now().Add(waitingTTL)).Save(ctx)
		return err
	})
	if err != nil {
		return Created{}, err
	}
	return s.created(row, tok), nil
}

// List returns rows the sweep has not reached yet, expired ones included.
func (s *Service) List(ctx context.Context) ([]Enrollment, error) {
	rows, err := s.db.LocationEnrollment.Query().Where(locationenrollment.ExpiresAtGTE(s.now().Add(-sweepAfter))).
		Order(locationenrollment.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Enrollment, len(rows))
	for i, r := range rows {
		out[i] = s.toEnrollment(r)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id int64) (Enrollment, error) {
	r, err := s.db.LocationEnrollment.Get(ctx, id)
	if err != nil {
		return Enrollment{}, notFound(err)
	}
	return s.toEnrollment(r), nil
}

// usable rejects a row that is expired or no longer waiting.
func (s *Service) usable(r *ent.LocationEnrollment) error {
	if !s.now().Before(r.ExpiresAt) {
		return errExpired
	}
	if r.State != locationenrollment.StateWaiting {
		return errUsed
	}
	return nil
}

// Check validates a token for the setup script's preflight.
func (s *Service) Check(ctx context.Context, token string) (Enrollment, error) {
	r, err := s.db.LocationEnrollment.Query().Where(locationenrollment.TokenHash(hashToken(token))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return Enrollment{}, errInvalid
		}
		return Enrollment{}, err
	}
	if err := s.usable(r); err != nil {
		return Enrollment{}, err
	}
	return s.toEnrollment(r), nil
}

// Report stores the script's report for the enrollment the token belongs to.
func (s *Service) Report(ctx context.Context, token string, raw []byte) error {
	return s.store(ctx, func(q *ent.LocationEnrollmentQuery) *ent.LocationEnrollmentQuery {
		return q.Where(locationenrollment.TokenHash(hashToken(token)))
	}, nil, raw)
}

// ReportBlob stores a pasted blob, which must carry enrollment id's own token.
func (s *Service) ReportBlob(ctx context.Context, id int64, blob string) error {
	tok, raw, err := DecodeBlob(blob)
	if err != nil {
		return err
	}
	return s.store(ctx, func(q *ent.LocationEnrollmentQuery) *ent.LocationEnrollmentQuery {
		return q.Where(locationenrollment.ID(id))
	}, hashToken(tok), raw)
}

// store locks the selected row, re-checks it and saves the report; wantHash, when set, must be the row's.
func (s *Service) store(ctx context.Context, sel func(*ent.LocationEnrollmentQuery) *ent.LocationEnrollmentQuery, wantHash, raw []byte) error {
	if s.boxErr != nil {
		return s.boxErr
	}
	return s.withTx(ctx, func(tx *ent.Client) error {
		r, err := sel(tx.LocationEnrollment.Query()).ForUpdate().Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				if wantHash != nil {
					return errNotFound
				}
				return errInvalid
			}
			return err
		}
		if wantHash != nil && !bytes.Equal(r.TokenHash, wantHash) {
			return errInvalid
		}
		if err := s.usable(r); err != nil {
			return err
		}
		rep, err := ParseReport(raw)
		if err != nil {
			return err
		}
		if rep.TokenID != "nebla@pve!"+r.TokenName {
			return bad("token_id does not match this enrollment")
		}
		body, err := json.Marshal(rep)
		if err != nil {
			return err
		}
		return tx.LocationEnrollment.UpdateOne(r).SetState(locationenrollment.StateReported).SetTokenID(rep.TokenID).
			SetTokenSecret(s.box.Seal([]byte(rep.TokenSecret))).SetReport(body).SetExpiresAt(s.now().Add(reportedTTL)).Exec(ctx)
	})
}

// Delete cancels an enrollment and drops its sealed secret.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return notFound(s.db.LocationEnrollment.DeleteOneID(id).Exec(ctx))
}

// Sweep deletes rows more than a day past expires_at.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	return s.db.LocationEnrollment.Delete().Where(locationenrollment.ExpiresAtLT(s.now().Add(-sweepAfter))).Exec(ctx)
}

// ScriptBody is the online setup script served at /v1/enroll.sh.
func (s *Service) ScriptBody() ([]byte, error) {
	if s.urlErr != nil {
		return nil, s.urlErr
	}
	return Script(s.o.BaseURL), nil
}
