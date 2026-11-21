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

// Package mail sends outbound email over SMTP.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Noop drops every message; it is used when PANEL_SMTP_URL is empty.
type Noop struct{}

func (Noop) Send(context.Context, string, string, string) error { return nil }

// New returns an SMTP sender for smtp:// or smtps:// URLs, or Noop when smtpURL is empty.
func New(smtpURL, baseURL string) (Sender, error) {
	if smtpURL == "" {
		return Noop{}, nil
	}
	u, err := url.Parse(smtpURL)
	if err != nil {
		return nil, fmt.Errorf("PANEL_SMTP_URL: %w", err)
	}
	if u.Scheme != "smtp" && u.Scheme != "smtps" {
		return nil, errors.New("PANEL_SMTP_URL: scheme must be smtp or smtps")
	}
	if u.Hostname() == "" {
		return nil, errors.New("PANEL_SMTP_URL: host is required")
	}
	s := &SMTP{host: u.Hostname(), port: u.Port(), implicitTLS: u.Scheme == "smtps", from: u.Query().Get("from")}
	if s.port == "" {
		s.port = "587"
		if s.implicitTLS {
			s.port = "465"
		}
	}
	if u.User != nil {
		s.user = u.User.Username()
		s.pass, _ = u.User.Password()
	}
	if s.from == "" {
		host := "localhost"
		if b, err := url.Parse(baseURL); err == nil && b.Hostname() != "" {
			host = b.Hostname()
		}
		s.from = "nebla@" + host
	}
	if strings.ContainsAny(s.from, "\r\n") {
		return nil, errors.New("PANEL_SMTP_URL: invalid from address")
	}
	return s, nil
}

type SMTP struct {
	host, port  string
	user, pass  string
	from        string
	implicitTLS bool
}

func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	if strings.ContainsAny(to+subject, "\r\n") {
		return errors.New("mail: header contains a newline")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return fmt.Errorf("mail: dial: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	if s.implicitTLS {
		conn = tls.Client(conn, tlsCfg)
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	defer c.Close()
	if !s.implicitTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("mail: starttls: %w", err)
			}
		}
	}
	if s.user != "" {
		if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := c.Mail(s.from); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	msg := "From: " + s.from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	return c.Quit()
}
