package pve

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

// LeafFingerprint returns the upper-hex SHA-256 of the leaf certificate served at baseURL's host.
func LeafFingerprint(ctx context.Context, baseURL string) (string, error) {
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
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}} // only reads the cert
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return "", fmt.Errorf("pve: read certificate: %w", err)
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("pve: no peer certificate")
	}
	return fmt.Sprintf("%X", sha256.Sum256(certs[0].Raw)), nil
}
