// Package sshkeys stores a project's SSH public keys.
package sshkeys

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/sshkey"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/ssh"
)

type Key struct {
	ID        int64
	ProjectID int64
	Name      string
	PublicKey string
	// Fingerprint is the MD5 colon-hex fingerprint.
	Fingerprint string
	CreatedAt   time.Time
}

type Service struct{ db *ent.Client }

func New(db *ent.Client) *Service { return &Service{db: db} }

var errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

func invalid(msg string) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", msg)
}

func toKey(k *ent.SSHKey) Key {
	return Key{ID: k.ID, ProjectID: k.ProjectID, Name: k.Name, PublicKey: k.PublicKey, Fingerprint: k.Fingerprint, CreatedAt: k.CreatedAt}
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", invalid("name must be 1-100 characters")
	}
	return name, nil
}

// parse accepts exactly one authorized_keys line and returns it normalised with its fingerprint.
func parse(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 16<<10 {
		return "", "", invalid("public_key must be one OpenSSH public key")
	}
	pk, comment, opts, rest, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil || len(opts) > 0 || len(strings.TrimSpace(string(rest))) > 0 {
		return "", "", invalid("public_key must be one OpenSSH public key")
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))
	if comment = strings.TrimSpace(comment); comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintLegacyMD5(pk), nil
}

func mapErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		if strings.Contains(pe.ConstraintName, "fingerprint") {
			return apierr.New(http.StatusConflict, "uniqueness_error", "this SSH key is already in the project")
		}
		return apierr.New(http.StatusConflict, "uniqueness_error", "an SSH key with this name already exists")
	}
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func (s *Service) Create(ctx context.Context, projectID int64, name, publicKey string) (Key, error) {
	name, err := validName(name)
	if err != nil {
		return Key{}, err
	}
	line, fp, err := parse(publicKey)
	if err != nil {
		return Key{}, err
	}
	k, err := s.db.SSHKey.Create().SetProjectID(projectID).SetName(name).SetPublicKey(line).SetFingerprint(fp).Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(k), nil
}

func (s *Service) List(ctx context.Context, projectID int64, page, perPage int) ([]Key, int, error) {
	q := s.db.SSHKey.Query().Where(sshkey.ProjectID(projectID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(sshkey.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Key, len(rows))
	for i, r := range rows {
		out[i] = toKey(r)
	}
	return out, total, nil
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Key, error) {
	k, err := s.db.SSHKey.Query().Where(sshkey.ID(id), sshkey.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(k), nil
}

// GetMany returns the keys in ids order; any id missing from the project is not_found.
func (s *Service) GetMany(ctx context.Context, projectID int64, ids []int64) ([]Key, error) {
	uniq := slices.Compact(slices.Sorted(slices.Values(ids)))
	rows, err := s.db.SSHKey.Query().Where(sshkey.ProjectID(projectID), sshkey.IDIn(uniq...)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(uniq) {
		return nil, errNotFound
	}
	byID := make(map[int64]*ent.SSHKey, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]Key, 0, len(uniq))
	seen := map[int64]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, toKey(byID[id]))
		}
	}
	return out, nil
}

func (s *Service) Rename(ctx context.Context, projectID, id int64, name string) (Key, error) {
	name, err := validName(name)
	if err != nil {
		return Key{}, err
	}
	n, err := s.db.SSHKey.Update().Where(sshkey.ID(id), sshkey.ProjectID(projectID)).SetName(name).Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	if n == 0 {
		return Key{}, errNotFound
	}
	return s.Get(ctx, projectID, id)
}

func (s *Service) Delete(ctx context.Context, projectID, id int64) error {
	n, err := s.db.SSHKey.Delete().Where(sshkey.ID(id), sshkey.ProjectID(projectID)).Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}
