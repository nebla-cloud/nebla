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

package osimages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrUpstream = errors.New("osimages: upstream unavailable")

const (
	maxBody = 1 << 20
	timeout = 30 * time.Second
)

type Resolved struct{ URL, File, Checksum, Algorithm string }

var (
	gnuLine = regexp.MustCompile(`^([0-9a-fA-F]{32,})\s+\*?(\S+)$`)
	bsdLine = regexp.MustCompile(`^[A-Za-z0-9-]+ \((.+)\) = ([0-9a-fA-F]{32,})$`)
	hrefRe  = regexp.MustCompile(`href="([^"]+)"`)
)

var errNotFound = errors.New("not found")

// parseChecksums maps file name to lowercase hex for GNU and BSD style lines.
func parseChecksums(body []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if m := gnuLine.FindStringSubmatch(line); m != nil {
			out[m[2]] = strings.ToLower(m[1])
		} else if m := bsdLine.FindStringSubmatch(line); m != nil {
			out[m[1]] = strings.ToLower(m[2])
		}
	}
	return out
}

func fetch(ctx context.Context, hc *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read failed")
	}
	if len(b) > maxBody {
		return nil, fmt.Errorf("body too large")
	}
	return b, nil
}

func checksumName(ctx context.Context, hc *http.Client, e Entry, dir string) (string, error) {
	if e.ChecksumName != "" {
		return e.ChecksumName, nil
	}
	idx, err := fetch(ctx, hc, dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, m := range hrefRe.FindAllStringSubmatch(string(idx), -1) {
		if n := path.Base(m[1]); e.ChecksumFile.MatchString(n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no checksum file in index")
	}
	sort.Strings(names)
	return names[len(names)-1], nil
}

func pick(e Entry, sums map[string]string) (name, sum string, ok bool) {
	want := map[string]int{"sha256": 64, "sha512": 128}[e.Algorithm]
	var names []string
	for n, h := range sums {
		if e.File.MatchString(n) && len(h) == want {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", "", false
	}
	sort.Strings(names)
	latest := ""
	for _, n := range names {
		if strings.Contains(n, "latest") {
			latest = n
		}
	}
	if latest == "" {
		n := names[len(names)-1]
		return n, sums[n], true
	}
	// Pin the newest dated name that shares the latest alias's hash.
	pinned := latest
	for _, n := range names {
		if !strings.Contains(n, "latest") && sums[n] == sums[latest] {
			pinned = n
		}
	}
	return pinned, sums[pinned], true
}

// Resolve picks the concrete image file and hash from the first dir that serves it.
func Resolve(ctx context.Context, hc *http.Client, e Entry) (Resolved, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, dir := range e.Dirs {
		name, err := checksumName(ctx, hc, e, dir)
		var body []byte
		if err == nil {
			body, err = fetch(ctx, hc, dir+name)
		}
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return Resolved{}, fmt.Errorf("%w: %s: %v", ErrUpstream, e.Key, err)
		}
		file, sum, ok := pick(e, parseChecksums(body))
		if !ok {
			return Resolved{}, fmt.Errorf("%w: %s: no matching image in checksum file", ErrUpstream, e.Key)
		}
		return Resolved{URL: dir + file, File: file, Checksum: sum, Algorithm: e.Algorithm}, nil
	}
	return Resolved{}, fmt.Errorf("%w: %s: not found", ErrUpstream, e.Key)
}
