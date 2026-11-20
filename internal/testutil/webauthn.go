package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/url"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// Authenticator is a software passkey (ES256, "none" attestation) for tests.
type Authenticator struct {
	key        *ecdsa.PrivateKey
	credID     []byte
	aaguid     [16]byte
	userHandle []byte
	signCount  uint32
	origin     string
	rpID       string
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

var b64 = base64.RawURLEncoding

// NewAuthenticator makes an authenticator that signs for origin, with the RP ID taken from its hostname.
func NewAuthenticator(origin string, aaguid [16]byte) *Authenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	u, err := url.Parse(origin)
	if err != nil {
		panic(err)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return &Authenticator{key: key, credID: id, aaguid: aaguid, origin: origin, rpID: u.Hostname()}
}

// Register answers creation options (the publicKey object) with a RegistrationResponseJSON, ignoring excludeCredentials.
func (a *Authenticator) Register(options json.RawMessage) json.RawMessage {
	var o struct {
		Challenge string `json:"challenge"`
		User      struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	must(json.Unmarshal(options, &o))
	handle, err := b64.DecodeString(o.User.ID)
	must(err)
	a.userHandle = handle
	a.signCount = 1

	x, y := a.key.PublicKey.X.FillBytes(make([]byte, 32)), a.key.PublicKey.Y.FillBytes(make([]byte, 32))
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1, XCoord: x, YCoord: y,
	})
	must(err)
	auth := a.authData(flagUP | flagUV | flagAT)
	auth = append(auth, a.aaguid[:]...)
	auth = binary.BigEndian.AppendUint16(auth, uint16(len(a.credID)))
	auth = append(auth, a.credID...)
	auth = append(auth, cose...)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
	must(err)

	return a.credential(map[string]any{
		"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", o.Challenge)),
		"attestationObject": b64.EncodeToString(att),
		"transports":        []string{"internal"},
	})
}

// Assert answers request options (the publicKey object) with an AuthenticationResponseJSON that carries the user handle.
func (a *Authenticator) Assert(options json.RawMessage) json.RawMessage {
	var o struct {
		Challenge string `json:"challenge"`
	}
	must(json.Unmarshal(options, &o))
	a.signCount++
	auth := a.authData(flagUP | flagUV)
	cd := a.clientData("webauthn.get", o.Challenge)
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, auth...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	must(err)
	return a.credential(map[string]any{
		"clientDataJSON":    b64.EncodeToString(cd),
		"authenticatorData": b64.EncodeToString(auth),
		"signature":         b64.EncodeToString(sig),
		"userHandle":        b64.EncodeToString(a.userHandle),
	})
}

func (a *Authenticator) authData(flags byte) []byte {
	rp := sha256.Sum256([]byte(a.rpID))
	out := append(rp[:], flags)
	return binary.BigEndian.AppendUint32(out, a.signCount)
}

func (a *Authenticator) clientData(typ, challenge string) []byte {
	cd, err := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	must(err)
	return cd
}

func (a *Authenticator) credential(response map[string]any) json.RawMessage {
	out, err := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{}, "response": response,
	})
	must(err)
	return out
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
