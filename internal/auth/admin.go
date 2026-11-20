package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/user"
	"github.com/nebla-cloud/nebla/internal/ent/webauthnchallenge"
	"github.com/nebla-cloud/nebla/internal/ent/webauthncredential"
)

var (
	ErrSelfAdminAction = apierr.New(http.StatusUnprocessableEntity, "self_admin_action", "Use your account page to change this for yourself.")
	ErrLastAdmin       = apierr.New(http.StatusConflict, "last_admin", "The panel needs at least one admin.")
	errUserNotFound    = apierr.New(http.StatusNotFound, "not_found", "not found")
)

// AdminUser is a user with the counts the admin list shows.
type AdminUser struct {
	User
	PasskeyCount int
}

// ListAdminUsers returns one page of users with their passkey counts, plus the total count.
func (s *Service) ListAdminUsers(ctx context.Context, page, perPage int) ([]AdminUser, int, error) {
	us, total, err := s.ListUsers(ctx, page, perPage)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(us))
	for i, u := range us {
		ids[i] = u.ID
	}
	counts, err := passkeyCounts(ctx, s.db, ids...)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminUser, len(us))
	for i, u := range us {
		out[i] = AdminUser{User: u, PasskeyCount: counts[u.ID]}
	}
	return out, total, nil
}

func passkeyCounts(ctx context.Context, db *ent.Client, ids ...int64) (map[int64]int, error) {
	var rows []struct {
		UserID int64 `json:"user_id"`
		Count  int   `json:"count"`
	}
	err := db.WebauthnCredential.Query().Where(webauthncredential.UserIDIn(ids...)).
		GroupBy(webauthncredential.FieldUserID).Aggregate(ent.Count()).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.UserID] = r.Count
	}
	return out, nil
}

// LockAdmins locks every admin row in id order and returns their ids.
func LockAdmins(ctx context.Context, tx *ent.Client) ([]int64, error) {
	return tx.User.Query().Where(user.IsAdmin(true)).Order(user.ByID()).ForUpdate().IDs(ctx)
}

// LockUser locks a user row, or returns not_found.
func LockUser(ctx context.Context, tx *ent.Client, userID int64) (*ent.User, error) {
	u, err := tx.User.Query().Where(user.ID(userID)).ForUpdate().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, errUserNotFound
	}
	return u, err
}

// CheckLastAdmin refuses when userID is the only id in admins.
func CheckLastAdmin(admins []int64, userID int64) error {
	if len(admins) <= 1 && slices.Contains(admins, userID) {
		return ErrLastAdmin
	}
	return nil
}

// AdminUpdateUser sets a user's name, email and admin flag; an email change revokes their sessions except keepToken.
func (s *Service) AdminUpdateUser(ctx context.Context, actorID, userID int64, name, email string, isAdmin bool, keepToken string) (AdminUser, error) {
	var out AdminUser
	err := s.inTx(ctx, func(tx *ent.Client) error {
		var admins []int64
		if !isAdmin {
			// Admins are locked before the target so concurrent demotions take locks in one order.
			var err error
			if admins, err = LockAdmins(ctx, tx); err != nil {
				return err
			}
		}
		u, err := LockUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if actorID == userID && isAdmin != u.IsAdmin {
			return ErrSelfAdminAction
		}
		email, err := validEmail(email)
		if err != nil {
			return err
		}
		if err := validName(name); err != nil {
			return err
		}
		if u.IsAdmin && !isAdmin {
			if err := CheckLastAdmin(admins, userID); err != nil {
				return err
			}
		}
		row, err := tx.User.UpdateOneID(userID).SetName(strings.TrimSpace(name)).SetEmail(email).SetIsAdmin(isAdmin).Save(ctx)
		if err != nil {
			return mapCreateErr(err)
		}
		if email != u.Email {
			keep := ""
			if actorID == userID {
				keep = keepToken
			}
			if err := revokeOtherSessions(ctx, tx, userID, keep); err != nil {
				return err
			}
		}
		counts, err := passkeyCounts(ctx, tx, userID)
		if err != nil {
			return err
		}
		out = AdminUser{User: toUser(row), PasskeyCount: counts[userID]}
		return nil
	})
	return out, err
}

// adminSignInChange locks another user, applies fn and revokes all their sessions.
func (s *Service) adminSignInChange(ctx context.Context, actorID, userID int64, fn func(tx *ent.Client) error) error {
	if actorID == userID {
		return ErrSelfAdminAction
	}
	return s.inTx(ctx, func(tx *ent.Client) error {
		if _, err := LockUser(ctx, tx, userID); err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			return err
		}
		return RevokeSessions(ctx, tx, userID)
	})
}

// AdminSetPassword sets another user's password, optionally removing their passkeys, and signs them out.
func (s *Service) AdminSetPassword(ctx context.Context, actorID, userID int64, password string, removePasskeys bool) error {
	if actorID == userID {
		return ErrSelfAdminAction
	}
	if err := validPassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return err
	}
	return s.adminSignInChange(ctx, actorID, userID, func(tx *ent.Client) error {
		if err := tx.User.UpdateOneID(userID).SetPasswordHash(hash).Exec(ctx); err != nil {
			return err
		}
		if removePasskeys {
			return deletePasskeys(ctx, tx, userID)
		}
		return nil
	})
}

// AdminResetTOTP turns off another user's two-factor and signs them out.
func (s *Service) AdminResetTOTP(ctx context.Context, actorID, userID int64) error {
	return s.adminSignInChange(ctx, actorID, userID, func(tx *ent.Client) error {
		return tx.User.UpdateOneID(userID).ClearTotpSecret().ClearTotpPendingSecret().SetTotpEnabled(false).Exec(ctx)
	})
}

// AdminRemovePasskeys deletes another user's passkeys and pending challenges and signs them out.
func (s *Service) AdminRemovePasskeys(ctx context.Context, actorID, userID int64) error {
	return s.adminSignInChange(ctx, actorID, userID, func(tx *ent.Client) error {
		return deletePasskeys(ctx, tx, userID)
	})
}

func deletePasskeys(ctx context.Context, tx *ent.Client, userID int64) error {
	if _, err := tx.WebauthnCredential.Delete().Where(webauthncredential.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	_, err := tx.WebauthnChallenge.Delete().Where(webauthnchallenge.UserID(userID)).Exec(ctx)
	return err
}
