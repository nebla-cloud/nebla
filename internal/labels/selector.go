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

// Package labels parses and matches label selectors.
package labels

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
)

var (
	KeyRE   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._/-]{0,61}[a-zA-Z0-9])?$`)
	ValueRE = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?)?$`)
)

type Op int

const (
	Eq Op = iota
	Ne
	Has
	Lacks
)

type Term struct {
	Key, Value string
	Op         Op
}

type Selector []Term

func invalid(msg string) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", msg)
}

// Parse reads comma-separated k=v, k!=v, k and !k terms.
func Parse(sel string) (Selector, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, invalid("label_selector must not be empty")
	}
	var out Selector
	for raw := range strings.SplitSeq(sel, ",") {
		raw = strings.TrimSpace(raw)
		key, val, op := raw, "", Has
		switch {
		case strings.HasPrefix(raw, "!"):
			key, op = raw[1:], Lacks
		case strings.Contains(raw, "!="):
			key, val, _ = strings.Cut(raw, "!=")
			op = Ne
		case strings.Contains(raw, "="):
			key, val, _ = strings.Cut(raw, "=")
			op = Eq
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !KeyRE.MatchString(key) {
			return nil, invalid("label_selector: bad key in \"" + raw + "\"")
		}
		if (op == Eq || op == Ne) && !ValueRE.MatchString(val) {
			return nil, invalid("label_selector: bad value in \"" + raw + "\"")
		}
		out = append(out, Term{Key: key, Value: val, Op: op})
	}
	return out, nil
}

// Matches reports whether every term holds for the label map.
func (s Selector) Matches(m map[string]string) bool {
	for _, t := range s {
		v, ok := m[t.Key]
		switch t.Op {
		case Eq:
			ok = ok && v == t.Value
		case Ne:
			ok = !ok || v != t.Value
		case Lacks:
			ok = !ok
		}
		if !ok {
			return false
		}
	}
	return true
}

// String is the canonical form that Parse accepts back.
func (s Selector) String() string {
	parts := make([]string, len(s))
	for i, t := range s {
		switch t.Op {
		case Eq:
			parts[i] = t.Key + "=" + t.Value
		case Ne:
			parts[i] = t.Key + "!=" + t.Value
		case Has:
			parts[i] = t.Key
		case Lacks:
			parts[i] = "!" + t.Key
		}
	}
	return strings.Join(parts, ",")
}
