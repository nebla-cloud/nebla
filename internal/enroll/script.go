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
	_ "embed"
	"regexp"
	"strings"
	"text/template"
)

//go:embed enroll.sh
var scriptSrc string

var (
	scriptTmpl  = template.Must(template.New("enroll.sh").Option("missingkey=error").Parse(scriptSrc))
	panelURLRe  = regexp.MustCompile(`^https?://[A-Za-z0-9.:\[\]-]+(/[A-Za-z0-9._~/-]*)?$`)
	enrollTokRe = regexp.MustCompile(`^nbe_[A-Za-z0-9_-]{43}$`)
	tokenNameRe = regexp.MustCompile(`^nebla-[a-z0-9-]{1,32}-[a-z0-9]{6}$`)
)

func mustMatch(re *regexp.Regexp, what, v string) {
	if !re.MatchString(v) {
		panic("enroll: invalid " + what)
	}
}

// Script renders the setup script for panelURL; it panics on a URL outside the safe charset.
func Script(panelURL string) []byte {
	mustMatch(panelURLRe, "panel URL", panelURL)
	var b bytes.Buffer
	if err := scriptTmpl.Execute(&b, struct{ PanelURL string }{panelURL}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// Command is the one-line online setup command.
func Command(panelURL, token string) string {
	mustMatch(panelURLRe, "panel URL", panelURL)
	mustMatch(enrollTokRe, "enrollment token", token)
	return "curl -fsSL '" + strings.TrimRight(panelURL, "/") + "/v1/enroll.sh' | NEBLA_TOKEN=" + token + " bash"
}

// OfflineScript is the full script in a quoted heredoc, with the token and token name set.
func OfflineScript(panelURL, token, tokenName string) string {
	mustMatch(enrollTokRe, "enrollment token", token)
	mustMatch(tokenNameRe, "token name", tokenName)
	return "NEBLA_TOKEN=" + token + " NEBLA_TOKEN_NAME=" + tokenName + " NEBLA_OFFLINE=1 bash <<'NEBLA_EOF'\n" +
		string(Script(panelURL)) + "NEBLA_EOF\n"
}
