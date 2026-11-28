// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// SSHKey is a public key that can be put on servers.
type SSHKey struct {
	ID          int64
	Name        string
	PublicKey   string
	Fingerprint string
	Created     time.Time
}

func sshKeyFromWire(w wire.SSHKey) SSHKey {
	return SSHKey{ID: w.ID, Name: w.Name, PublicKey: w.PublicKey, Fingerprint: w.Fingerprint, Created: w.Created}
}

// SSHKeyCreateOpts creates an SSH key.
type SSHKeyCreateOpts struct {
	Name      string
	PublicKey string
}

// SSHKeyUpdateOpts renames an SSH key; nil fields are not sent.
type SSHKeyUpdateOpts struct {
	Name *string
}

type sshKeyCreateBody struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

type sshKeyUpdateBody struct {
	Name *string `json:"name,omitempty"`
}

// SSHKeysService works with SSH keys.
type SSHKeysService struct{ c *Client }

// List returns one page of SSH keys.
func (s *SSHKeysService) List(ctx context.Context, opts ListOpts) ([]SSHKey, Meta, error) {
	return listPage(ctx, s.c, "/ssh_keys", opts.values(), "ssh_keys", sshKeyFromWire)
}

// All returns every SSH key.
func (s *SSHKeysService) All(ctx context.Context) ([]SSHKey, error) {
	return listAll(ctx, s.c, "/ssh_keys", nil, "ssh_keys", sshKeyFromWire)
}

// Get returns one SSH key.
func (s *SSHKeysService) Get(ctx context.Context, id int64) (*SSHKey, error) {
	var w wire.SSHKey
	if err := s.c.do(ctx, "GET", "/ssh_keys/"+strconv.FormatInt(id, 10), nil, nil, &w); err != nil {
		return nil, err
	}
	k := sshKeyFromWire(w)
	return &k, nil
}

// GetByName returns the SSH key with exactly this name.
func (s *SSHKeysService) GetByName(ctx context.Context, name string) (*SSHKey, error) {
	return getByName(ctx, s.All, name, func(k SSHKey) string { return k.Name }, "SSH key")
}

// Create adds an SSH key.
func (s *SSHKeysService) Create(ctx context.Context, opts SSHKeyCreateOpts) (*SSHKey, error) {
	var w wire.SSHKey
	if err := s.c.do(ctx, "POST", "/ssh_keys", nil, sshKeyCreateBody{Name: opts.Name, PublicKey: opts.PublicKey}, &w); err != nil {
		return nil, err
	}
	k := sshKeyFromWire(w)
	return &k, nil
}

// Update renames an SSH key.
func (s *SSHKeysService) Update(ctx context.Context, id int64, opts SSHKeyUpdateOpts) (*SSHKey, error) {
	var w wire.SSHKey
	if err := s.c.do(ctx, "PUT", "/ssh_keys/"+strconv.FormatInt(id, 10), nil, sshKeyUpdateBody{Name: opts.Name}, &w); err != nil {
		return nil, err
	}
	k := sshKeyFromWire(w)
	return &k, nil
}

// Delete removes an SSH key.
func (s *SSHKeysService) Delete(ctx context.Context, id int64) error {
	return s.c.do(ctx, "DELETE", "/ssh_keys/"+strconv.FormatInt(id, 10), nil, nil, nil)
}
