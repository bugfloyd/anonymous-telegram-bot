// Package devicelist reads an account's signed device list.
//
// The list says which devices belong to the account. Devices sign it with the
// account key and check each other's copies, so a server that adds a device
// of its own is caught. The server only checks that a list is well formed and
// signed, and stores it.
//
// A list is a devicesig message labelled LabelDeviceList with the parts:
//
//	suite (1 byte), sequence number (8), hash of the previous list (32),
//	issued (8), expires (8), then for each device: ID (16), signing key (65),
//	added (8)
//
// Numbers are big-endian; times are Unix seconds on a whole hour, so the list
// doesn't record exactly when anything happened.
package devicelist

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

const (
	// MaxDevices is the most devices one account can have.
	MaxDevices = 10
	// MaxValidity is the longest a list may be valid for.
	MaxValidity = 30 * 24 * time.Hour
	// Granularity is the precision of every time in a list.
	Granularity = time.Hour

	headerParts = 5
	entryParts  = 3
	keyLen      = 65
)

// ErrInvalid is returned for a list that is malformed or badly signed.
var ErrInvalid = errors.New("devicelist: invalid")

// Entry is one device in a list.
type Entry struct {
	DeviceID string
	Key      []byte // the device signing key, a 65-byte P-256 point
	Added    time.Time
}

// List is a parsed device list.
type List struct {
	Seq     uint64
	Prev    [32]byte // SHA-256 of the previous list; zero for the first
	Issued  time.Time
	Expires time.Time
	Entries []Entry
}

// Encode returns the list's signed bytes.
func Encode(l List) []byte {
	parts := [][]byte{
		{devicesig.Suite},
		u64(l.Seq),
		l.Prev[:],
		u64(uint64(l.Issued.Unix())),
		u64(uint64(l.Expires.Unix())),
	}
	for _, e := range l.Entries {
		id, _ := ids.Decode(e.DeviceID)
		parts = append(parts, id[:], e.Key, u64(uint64(e.Added.Unix())))
	}
	return devicesig.Message(devicesig.LabelDeviceList, parts...)
}

// Parse reads a list and checks it is well formed. It doesn't check the
// signature or whether the list is current.
func Parse(raw []byte) (List, error) {
	label, parts, err := devicesig.Parse(raw)
	if err != nil || label != devicesig.LabelDeviceList {
		return List{}, ErrInvalid
	}
	if len(parts) < headerParts+entryParts || (len(parts)-headerParts)%entryParts != 0 {
		return List{}, ErrInvalid
	}
	if !bytes.Equal(parts[0], []byte{devicesig.Suite}) || len(parts[2]) != 32 {
		return List{}, ErrInvalid
	}
	var l List
	var ok bool
	if l.Seq, ok = readU64(parts[1]); !ok || l.Seq == 0 {
		return List{}, ErrInvalid
	}
	copy(l.Prev[:], parts[2])
	if l.Issued, ok = readTime(parts[3]); !ok {
		return List{}, ErrInvalid
	}
	if l.Expires, ok = readTime(parts[4]); !ok {
		return List{}, ErrInvalid
	}
	if !l.Issued.Before(l.Expires) || l.Expires.Sub(l.Issued) > MaxValidity {
		return List{}, ErrInvalid
	}

	count := (len(parts) - headerParts) / entryParts
	if count > MaxDevices {
		return List{}, ErrInvalid
	}
	seen := make(map[string]bool, count)
	for i := range count {
		p := parts[headerParts+i*entryParts:]
		if len(p[0]) != 16 || len(p[1]) != keyLen {
			return List{}, ErrInvalid
		}
		if _, err := devicesig.ParsePublicKey(p[1]); err != nil {
			return List{}, ErrInvalid
		}
		added, ok := readTime(p[2])
		if !ok || added.After(l.Issued) {
			return List{}, ErrInvalid
		}
		id := ids.Encode([16]byte(p[0]))
		if seen[id] {
			return List{}, ErrInvalid
		}
		seen[id] = true
		l.Entries = append(l.Entries, Entry{DeviceID: id, Key: bytes.Clone(p[1]), Added: added})
	}
	return l, nil
}

// Verify parses a list and checks it was signed by the account key.
func Verify(raw, signature []byte, accountKey *ecdsa.PublicKey) (List, error) {
	l, err := Parse(raw)
	if err != nil {
		return List{}, err
	}
	if devicesig.VerifyEncoded(accountKey, signature, raw) != nil {
		return List{}, ErrInvalid
	}
	return l, nil
}

// Hash is what the next list records as its previous list.
func Hash(raw []byte) [32]byte {
	return sha256.Sum256(raw)
}

// Device returns the entry for a device, if the list has one.
func (l List) Device(id string) (Entry, bool) {
	for _, e := range l.Entries {
		if e.DeviceID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func u64(v uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, v)
}

func readU64(b []byte) (uint64, bool) {
	if len(b) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(b), true
}

// readTime reads Unix seconds that must fall on a whole hour.
func readTime(b []byte) (time.Time, bool) {
	v, ok := readU64(b)
	if !ok || v == 0 || v%uint64(Granularity/time.Second) != 0 || v > 1<<40 {
		return time.Time{}, false
	}
	return time.Unix(int64(v), 0), true
}
