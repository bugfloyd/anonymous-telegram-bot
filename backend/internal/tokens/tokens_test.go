package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/circl/blindsign/blindrsa"
)

var now = time.Unix(1_800_000_000, 0)

// The RFC 9474 vector the web client checks too (web/src/crypto/blindrsa-vectors.json).
func TestRFC9474Vector(t *testing.T) {
	raw, err := os.ReadFile("testdata/rfc9474-pss-deterministic.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vector map[string]string `json:"vector"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	v := file.Vector
	num := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
		if !ok {
			t.Fatalf("bad number %q", s)
		}
		return n
	}
	hx := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: num(v["n"]), E: int(num(v["e"]).Int64())},
		D:         num(v["d"]),
		Primes:    []*big.Int{num(v["p"]), num(v["q"])},
	}
	key.Precompute()
	sig, err := blindrsa.NewSigner(key).BlindSign(hx(v["blinded_msg"]))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(sig) != v["blind_sig"] {
		t.Fatalf("blind signature = %x\nwant %s", sig, v["blind_sig"])
	}
	verifier, _ := blindrsa.NewVerifier(blindrsa.SHA384PSSDeterministic, &key.PublicKey)
	if err := verifier.Verify(hx(v["input_msg"]), hx(v["sig"])); err != nil {
		t.Fatalf("the vector's final signature doesn't verify: %v", err)
	}
}

func newIssuer(t *testing.T) (*Issuer, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, ModulusBits)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	issuer, err := NewIssuer(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	return issuer, &key.PublicKey
}

// mint does what a client does: blind a token input, have it signed, unblind.
func mint(t *testing.T, issuer *Issuer, pub *rsa.PublicKey, epoch uint64, keyID [32]byte) []byte {
	t.Helper()
	input := binary.BigEndian.AppendUint16(nil, Type)
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	challenge := Challenge(epoch)
	input = append(append(append(input, nonce...), challenge[:]...), keyID[:]...)
	client, err := blindrsa.NewClient(blindrsa.SHA384PSSDeterministic, pub)
	if err != nil {
		t.Fatal(err)
	}
	blinded, state, err := client.Blind(rand.Reader, input)
	if err != nil {
		t.Fatal(err)
	}
	blindSig, err := issuer.Sign(blinded)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := client.Finalize(state, blindSig)
	if err != nil {
		t.Fatal(err)
	}
	return append(input, sig...)
}

func TestTokens(t *testing.T) {
	issuer, pub := newIssuer(t)
	epoch := EpochOf(now)

	token := mint(t, issuer, pub, epoch, issuer.KeyID())
	nonce, forget, err := issuer.Check(token, now)
	if err != nil || len(nonce) != 32 {
		t.Fatalf("Check = %x, %v", nonce, err)
	}
	if want := time.Unix(int64(epoch+2)*int64(Epoch/time.Second), 0); !forget.Equal(want) {
		t.Fatalf("forget after %v, want %v", forget, want)
	}
	// Last week's tokens are still good; older and future ones aren't.
	if _, _, err := issuer.Check(mint(t, issuer, pub, epoch-1, issuer.KeyID()), now); err != nil {
		t.Fatalf("last week's token: %v", err)
	}
	for name, bad := range map[string][]byte{
		"two weeks old": mint(t, issuer, pub, epoch-2, issuer.KeyID()),
		"next week":     mint(t, issuer, pub, epoch+1, issuer.KeyID()),
		"other key ID":  mint(t, issuer, pub, epoch, [32]byte{1}),
		"truncated":     token[:len(token)-1],
		"other type":    append([]byte{0, 1}, token[2:]...),
		"tampered nonce": func() []byte {
			b := append([]byte(nil), token...)
			b[5] ^= 1
			return b
		}(),
	} {
		if _, _, err := issuer.Check(bad, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A token signed by another issuer is refused.
	other, otherPub := newIssuer(t)
	if _, _, err := issuer.Check(mint(t, other, otherPub, epoch, issuer.KeyID()), now); err == nil {
		t.Error("accepted another issuer's token")
	}
}

func TestNewIssuerRejects(t *testing.T) {
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	der, _ := x509.MarshalPKCS8PrivateKey(small)
	for name, in := range map[string][]byte{
		"not PEM":    []byte("nope"),
		"1024 bits":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
		"wrong type": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}),
	} {
		if _, err := NewIssuer(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestChallengeVector(t *testing.T) {
	// Computed independently (Node) and checked by the client (web/src/protocol/token.test.ts).
	epoch := EpochOf(now)
	got := Challenge(epoch)
	if epoch != 2976 || hex.EncodeToString(got[:]) != "3ff2ca2b7e3d94fa8b80f80f0f44814f65decd7bbd029649ed7f22149d457f93" {
		t.Fatalf("epoch %d, challenge %x", epoch, got)
	}
}
