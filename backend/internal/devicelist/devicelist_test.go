package devicelist

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

const (
	vectorKey = "043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1"
	// The same list encoded independently (Node), and by the client in
	// web/src/protocol/device-list.test.ts.
	vectorList = "001c696e636f676e69746f636861742f76322f6465766963652d6c697374000000010100000008000000000000000100000020000000000000000000000000000000000000000000000000000000000000000000000008000000006b49d20000000008000000006b715f00000000100000000000000000000000000000000000000041043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb100000008000000006b49d200"
)

var issued = time.Unix(1_800_000_000, 0)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func vector(t *testing.T) List {
	return List{
		Seq:     1,
		Issued:  issued,
		Expires: issued.Add(MaxValidity),
		Entries: []Entry{{DeviceID: "AAAAAAAAAAAAAAAAAAAAAA", Key: mustHex(t, vectorKey), Added: issued}},
	}
}

func TestEncodeMatchesClient(t *testing.T) {
	got := Encode(vector(t))
	if want := mustHex(t, vectorList); !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x\nwant     %x", got, want)
	}
	l, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if l.Seq != 1 || !l.Issued.Equal(issued) || len(l.Entries) != 1 || l.Entries[0].DeviceID != "AAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("Parse = %+v", l)
	}
	if _, ok := l.Device("AAAAAAAAAAAAAAAAAAAAAA"); !ok {
		t.Fatal("Device didn't find the entry")
	}
}

func sign(t *testing.T, key *ecdsa.PrivateKey, raw []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(raw)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

func TestVerify(t *testing.T) {
	ak, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	raw := Encode(vector(t))
	sig := sign(t, ak, raw)
	if _, err := Verify(raw, sig, &ak.PublicKey); err != nil {
		t.Fatalf("valid list refused: %v", err)
	}
	if _, err := Verify(raw, sig, &other.PublicKey); err == nil {
		t.Fatal("accepted a list signed by another key")
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-20] ^= 1
	if _, err := Verify(tampered, sign(t, other, tampered), &ak.PublicKey); err == nil {
		t.Fatal("accepted a tampered list")
	}
}

func TestParseRejects(t *testing.T) {
	devKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rawKey, _ := devKey.PublicKey.Bytes()
	entry := Entry{DeviceID: ids.New(), Key: rawKey, Added: issued}

	cases := map[string]func(l *List){
		"sequence zero":       func(l *List) { l.Seq = 0 },
		"no devices":          func(l *List) { l.Entries = nil },
		"expires before":      func(l *List) { l.Expires = l.Issued },
		"valid too long":      func(l *List) { l.Expires = l.Issued.Add(MaxValidity + Granularity) },
		"issued off the hour": func(l *List) { l.Issued = l.Issued.Add(time.Second) },
		"added after issued":  func(l *List) { l.Entries[0].Added = l.Issued.Add(Granularity) },
		"duplicate device":    func(l *List) { l.Entries = append(l.Entries, l.Entries[0]) },
		"bad key":             func(l *List) { l.Entries[0].Key = bytes.Repeat([]byte{4}, 65) },
		"too many devices": func(l *List) {
			for range MaxDevices {
				e := entry
				e.DeviceID = ids.New()
				l.Entries = append(l.Entries, e)
			}
		},
	}
	for name, change := range cases {
		l := vector(t)
		l.Entries = append([]Entry(nil), l.Entries...)
		change(&l)
		if _, err := Parse(Encode(l)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	ok := vector(t)
	ok.Entries = append(ok.Entries, entry)
	if _, err := Parse(Encode(ok)); err != nil {
		t.Errorf("two devices refused: %v", err)
	}

	good := Encode(vector(t))
	if _, err := Parse(devicesig.Message(devicesig.LabelSession, []byte{1})); err == nil {
		t.Error("accepted another label")
	}
	if _, err := Parse(append(bytes.Clone(good), 0, 0, 0, 0)); err == nil {
		t.Error("accepted an extra empty part")
	}
	wrongSuite := bytes.Clone(good)
	wrongSuite[2+len(devicesig.LabelDeviceList)+4] = 2
	if _, err := Parse(wrongSuite); err == nil {
		t.Error("accepted another suite")
	}
}
