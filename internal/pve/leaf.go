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

package pve

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"time"
)

// VerifyLeafFingerprint verifies the peer against the reported upper-hex SHA-256 fingerprints.
// On ErrFingerprint it returns the observed fingerprint for diagnostics.
func VerifyLeafFingerprint(ctx context.Context, baseURL string, fingerprints []string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("pve: invalid url")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var seen string
	d := tls.Dialer{Config: &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // VerifyPeerCertificate checks the enrollment pins.
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("pve: no peer certificate")
			}
			seen = fmt.Sprintf("%X", sha256.Sum256(raw[0]))
			if !slices.Contains(fingerprints, seen) {
				return ErrFingerprint
			}
			return nil
		},
	}}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return seen, fmt.Errorf("pve: read certificate: %w", err)
	}
	defer conn.Close()
	return seen, nil
}
