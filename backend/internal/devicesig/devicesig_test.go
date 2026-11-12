package devicesig

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Produced by WebCrypto (Node 26, the same implementation browsers expose):
// generateKey ECDSA P-256, exportKey "raw", sign with SHA-256 over
// Message(LabelSession, "AAAAAAAAAAAAAAAAAAAAAA", "1700000000", "nonce-123").
const (
	webCryptoPublicKey = "043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1"
	webCryptoMessage   = "0018696e636f676e69746f636861742f76322f73657373696f6e00000016414141414141414141414141414141414141414141410000000a31373030303030303030000000096e6f6e63652d313233"
	webCryptoSignature = "de28e9d54c5b32c8e19a5ee4d4e03056c92964367e96e17a4f750c51fd802cf70051f0f1c3fb9693cd8564282df3c88dbd8f9328f11ae2f489703c58235082d3"
)

var webCryptoParts = [][]byte{[]byte("AAAAAAAAAAAAAAAAAAAAAA"), []byte("1700000000"), []byte("nonce-123")}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMessageMatchesClientEncoding(t *testing.T) {
	got := Message(LabelSession, webCryptoParts...)
	if want := mustHex(t, webCryptoMessage); !bytes.Equal(got, want) {
		t.Fatalf("Message = %x\nwant      %x", got, want)
	}
}

func TestVerifyWebCryptoVector(t *testing.T) {
	pub, err := ParsePublicKey(mustHex(t, webCryptoPublicKey))
	if err != nil {
		t.Fatal(err)
	}
	sig := mustHex(t, webCryptoSignature)
	if err := Verify(pub, sig, LabelSession, webCryptoParts...); err != nil {
		t.Fatalf("WebCrypto signature rejected: %v", err)
	}

	t.Run("other label", func(t *testing.T) {
		if Verify(pub, sig, LabelRegister, webCryptoParts...) == nil {
			t.Fatal("accepted for another purpose")
		}
	})
	t.Run("bytes moved between parts", func(t *testing.T) {
		moved := [][]byte{[]byte("AAAAAAAAAAAAAAAAAAAAAA1"), []byte("700000000"), []byte("nonce-123")}
		if Verify(pub, sig, LabelSession, moved...) == nil {
			t.Fatal("accepted with shifted parts")
		}
	})
	t.Run("tampered signature", func(t *testing.T) {
		bad := bytes.Clone(sig)
		bad[10] ^= 1
		if Verify(pub, bad, LabelSession, webCryptoParts...) == nil {
			t.Fatal("accepted a tampered signature")
		}
	})
	t.Run("DER instead of P1363", func(t *testing.T) {
		if Verify(pub, append([]byte{0x30, 0x44}, sig...), LabelSession, webCryptoParts...) == nil {
			t.Fatal("accepted a wrong-length signature")
		}
	})
}

func TestVerifyGoSignature(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ParsePublicKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(Message(LabelRegister, []byte("x")))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	if err := Verify(pub, sig, LabelRegister, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if Verify(nil, sig, LabelRegister, []byte("x")) == nil {
		t.Fatal("accepted a nil key")
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	valid := mustHex(t, webCryptoPublicKey)
	offCurve := bytes.Clone(valid)
	offCurve[64] ^= 1
	compressed := append([]byte{0x02}, valid[1:33]...)
	for name, raw := range map[string][]byte{
		"empty":      nil,
		"compressed": compressed,
		"off curve":  offCurve,
		"too long":   append(bytes.Clone(valid), 0),
		"bad prefix": append([]byte{0x05}, valid[1:]...),
	} {
		if _, err := ParsePublicKey(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFingerprintMatchesClient(t *testing.T) {
	// Computed with WebCrypto, as web/src/crypto/keys.ts does.
	const want = "IVVfe8yQuoQPEcsZWUQBwjazebJkrBLkm1Q7ec-It2M"
	if got := Fingerprint(mustHex(t, webCryptoPublicKey)); got != want {
		t.Fatalf("Fingerprint = %s, want %s", got, want)
	}
}

func TestParse(t *testing.T) {
	label, parts, err := Parse(mustHex(t, webCryptoMessage))
	if err != nil {
		t.Fatal(err)
	}
	if label != LabelSession || len(parts) != len(webCryptoParts) {
		t.Fatalf("Parse = %q, %d parts", label, len(parts))
	}
	for i := range parts {
		if !bytes.Equal(parts[i], webCryptoParts[i]) {
			t.Fatalf("part %d = %q, want %q", i, parts[i], webCryptoParts[i])
		}
	}

	empty := Message("x", []byte{}, nil)
	if _, parts, err := Parse(empty); err != nil || len(parts) != 2 || len(parts[0]) != 0 {
		t.Fatalf("empty parts: %v, %d", err, len(parts))
	}

	full := mustHex(t, webCryptoMessage)
	for _, bad := range [][]byte{
		nil,
		{0x00},
		{0x00, 0x05, 'a'},                     // label past the end
		append(bytes.Clone(full), 0x00),       // trailing byte
		append(bytes.Clone(full), 0, 0, 0, 9), // part past the end
		full[:len(full)-1],                    // truncated part
	} {
		if _, _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%x) accepted", bad)
		}
	}
}
