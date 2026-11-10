// Package initdata checks the launch data (initData) Telegram gives a Mini App,
// using Telegram's Ed25519 signature for third parties
// (https://core.telegram.org/bots/webapps#validating-data-for-third-party-use).
//
// The older "hash" field is ignored on purpose: it is an HMAC keyed with the bot
// token, so anyone holding the token could forge it. The Ed25519 signature can
// only come from Telegram.
//
// Only the Telegram user ID, the start parameter, the write-access flag and the
// signing time are returned. Names, usernames, photos and chat details are
// never read, so they can't be stored by mistake.
package initdata

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Telegram's public keys for initData signatures.
var (
	ProductionKey = mustKey("e7bf03a2fa4602af4580703d88dda5bb59f32ed8b02a56c187fe7d34caed242d")
	TestKey       = mustKey("40055058a4ee38156a06562e52eece92a771bcd8346a8c4615cb7376eddf72ec")
)

var (
	// ErrMalformed is returned for launch data that can't be parsed.
	ErrMalformed = errors.New("initdata: malformed")
	// ErrSignature is returned when the Ed25519 signature doesn't verify.
	ErrSignature = errors.New("initdata: invalid signature")
	// ErrExpired is returned when the data was signed too long ago, or in the future.
	ErrExpired = errors.New("initdata: outside the accepted time window")
)

// Data is what the backend may use from verified launch data.
type Data struct {
	UserID          int64
	StartParam      string
	AllowsWriteToPM bool
	AuthDate        time.Time
}

// Verifier checks launch data for one bot.
type Verifier struct {
	BotID int64
	Key   ed25519.PublicKey
	// MaxAge is how old the data may be; MaxSkew how far in the future.
	MaxAge, MaxSkew time.Duration
}

// NewVerifier returns a verifier for botID with Telegram's production key and
// a window of 10 minutes in the past and 1 minute in the future.
func NewVerifier(botID int64) Verifier {
	return Verifier{BotID: botID, Key: ProductionKey, MaxAge: 10 * time.Minute, MaxSkew: time.Minute}
}

var startParamPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,512}$`)

// Verify parses raw launch data once, checks its signature and age, and
// returns the fields the backend may use.
func (v Verifier) Verify(raw string, now time.Time) (Data, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return Data{}, ErrMalformed
	}
	fields := make(map[string]string, len(values))
	for key, vals := range values {
		// A repeated key could be read one way here and another way elsewhere.
		if key == "" || len(vals) != 1 {
			return Data{}, ErrMalformed
		}
		fields[key] = vals[0]
	}

	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(fields["signature"], "="))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Data{}, ErrMalformed
	}
	if len(v.Key) != ed25519.PublicKeySize || v.BotID <= 0 {
		return Data{}, ErrSignature
	}
	if !ed25519.Verify(v.Key, []byte(checkString(v.BotID, fields)), sig) {
		return Data{}, ErrSignature
	}

	// Everything below was signed by Telegram.
	authUnix, err := strconv.ParseInt(fields["auth_date"], 10, 64)
	if err != nil {
		return Data{}, ErrMalformed
	}
	authDate := time.Unix(authUnix, 0)
	if authDate.Before(now.Add(-v.MaxAge)) || authDate.After(now.Add(v.MaxSkew)) {
		return Data{}, ErrExpired
	}

	var user struct {
		ID              json.Number `json:"id"`
		AllowsWriteToPM bool        `json:"allows_write_to_pm"`
	}
	dec := json.NewDecoder(strings.NewReader(fields["user"]))
	dec.UseNumber()
	if err := dec.Decode(&user); err != nil {
		return Data{}, ErrMalformed
	}
	userID, err := user.ID.Int64()
	if err != nil || userID <= 0 {
		return Data{}, ErrMalformed
	}

	startParam := fields["start_param"]
	if !startParamPattern.MatchString(startParam) {
		return Data{}, ErrMalformed
	}

	return Data{UserID: userID, StartParam: startParam, AllowsWriteToPM: user.AllowsWriteToPM, AuthDate: authDate}, nil
}

// checkString builds Telegram's data-check-string: "<bot_id>:WebAppData", then
// every field except hash and signature as key=value, sorted by key, one per line.
func checkString(botID int64, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key != "hash" && key != "signature" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(strconv.FormatInt(botID, 10))
	b.WriteString(":WebAppData")
	for _, key := range keys {
		b.WriteString("\n")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(fields[key])
	}
	return b.String()
}

func mustKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("initdata: bad built-in key")
	}
	return ed25519.PublicKey(b)
}
