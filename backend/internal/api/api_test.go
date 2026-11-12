package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/circl/blindsign/blindrsa"

	"github.com/incognitochat/incognitochat/backend/internal/devicelist"
	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/inbox"
	"github.com/incognitochat/incognitochat/backend/internal/links"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/session"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata/initdatatest"
	"github.com/incognitochat/incognitochat/backend/internal/tokens"
)

const (
	botID  = 123456
	userID = 7357135791
)

var start = time.Unix(1_800_000_000, 0)

// ---- in-memory stores ----

type memAccount struct {
	key       []byte
	th        string
	replaced  bool
	member    bool
	level     int
	granted   int
	allocated int
	devices   map[string][]byte
	links     int
	chat      []byte // sealed chat ID for notices
	language  string
}

type memCode struct {
	owner      string
	ownerLevel int
	uses, left int
}

type memLink struct {
	owner string
	link  directory.Link
}

type memDirectory struct {
	mu       sync.Mutex
	telegram map[string]string
	accounts map[string]*memAccount
	links    map[string]*memLink
	codes    map[string]*memCode
	levels   map[int]int
	boxes    map[string]directory.Mailbox
	fail     error
}

func newMemDirectory() *memDirectory {
	return &memDirectory{
		telegram: map[string]string{},
		accounts: map[string]*memAccount{},
		links:    map[string]*memLink{},
		codes:    map[string]*memCode{},
		levels:   map[int]int{},
		boxes:    map[string]directory.Mailbox{},
	}
}

func (m *memDirectory) CreateMailboxes(_ context.Context, account string, ids []string, max int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.boxes {
		if b.Owner == account && b.Link == "" {
			n++
		}
	}
	if n+len(ids) > max {
		return directory.ErrLimit
	}
	for _, id := range ids {
		m.boxes[id] = directory.Mailbox{ID: id, Owner: account}
	}
	return nil
}

func (m *memDirectory) Mailbox(_ context.Context, id string) (directory.Mailbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.boxes[id]
	if !ok {
		return directory.Mailbox{}, directory.ErrNotFound
	}
	return b, nil
}

func (m *memDirectory) Mailboxes(_ context.Context, account string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, b := range m.boxes {
		if b.Owner == account {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memDirectory) DiscardMailbox(_ context.Context, account, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.boxes[id]
	if !ok || b.Owner != account || b.Link != "" {
		return directory.ErrNotFound
	}
	delete(m.boxes, id)
	return nil
}

func (m *memDirectory) Devices(_ context.Context, account string, _ time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[account]
	if !ok || a.replaced {
		return nil, nil
	}
	var out []string
	for d := range a.devices {
		out = append(out, d)
	}
	return out, nil
}

type memInbox struct {
	mu    sync.Mutex
	items map[string][]inbox.Item // "<account>#<device>" -> items
}

func (m *memInbox) Deliver(_ context.Context, account string, devices []string, mailbox string, env []byte, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range devices {
		m.items[account+"#"+d] = append(m.items[account+"#"+d], inbox.Item{ID: ids.New(), Mailbox: mailbox, Envelope: env})
	}
	return nil
}

func (m *memInbox) List(_ context.Context, account, device string, limit int, _ time.Time) ([]inbox.Item, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.items[account+"#"+device]
	if len(items) > limit {
		return append([]inbox.Item(nil), items[:limit]...), true, nil
	}
	return append([]inbox.Item(nil), items...), false, nil
}

func (m *memInbox) Ack(_ context.Context, account, device string, ackIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	drop := map[string]bool{}
	for _, id := range ackIDs {
		drop[id] = true
	}
	var kept []inbox.Item
	for _, it := range m.items[account+"#"+device] {
		if !drop[it.ID] {
			kept = append(kept, it)
		}
	}
	m.items[account+"#"+device] = kept
	return nil
}

func (m *memDirectory) Levels(context.Context) (map[int]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int]int{}
	for k, v := range m.levels {
		out[k] = v
	}
	return out, nil
}

func (m *memDirectory) SetLevel(_ context.Context, level, allowance int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.levels[level] = allowance
	return nil
}

func (m *memDirectory) CreateInvitation(_ context.Context, account, code string, uses, ownerLevel, maxAllocated int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.codes[code]; taken {
		return directory.ErrConflict
	}
	a := m.accounts[account]
	if maxAllocated >= 0 {
		if !a.member || a.allocated+uses > maxAllocated {
			return directory.ErrLimit
		}
		a.allocated += uses
	}
	m.codes[code] = &memCode{owner: account, ownerLevel: ownerLevel, uses: uses, left: uses}
	return nil
}

func (m *memDirectory) Invitations(_ context.Context, account string) ([]directory.Invitation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []directory.Invitation
	for code, c := range m.codes {
		if c.owner == account {
			out = append(out, directory.Invitation{Code: code, Uses: c.uses, Left: c.left})
		}
	}
	return out, nil
}

func (m *memDirectory) Redeem(_ context.Context, account, code string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codes[code]
	if ok && c.owner == account {
		return 0, directory.ErrOwnInvitation
	}
	if !ok || c.left == 0 {
		return 0, directory.ErrNotFound
	}
	a := m.accounts[account]
	if a.member {
		return 0, directory.ErrAlreadyMember
	}
	c.left--
	a.member, a.level = true, c.ownerLevel+1
	return a.level, nil
}

func (m *memDirectory) Grant(_ context.Context, linkID string, n int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[linkID]
	if !ok {
		return directory.ErrNotFound
	}
	a := m.accounts[l.owner]
	if !a.member {
		return directory.ErrNotMember
	}
	a.granted += n
	return nil
}

func (m *memDirectory) CreateLink(_ context.Context, account string, l directory.Link, max int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.links[l.ID]; taken {
		return directory.ErrConflict
	}
	a, ok := m.accounts[account]
	if !ok || a.links >= max {
		return directory.ErrLimit
	}
	a.links++
	m.links[l.ID] = &memLink{owner: account, link: l}
	m.boxes[l.Mailbox] = directory.Mailbox{ID: l.Mailbox, Owner: account, Link: l.ID}
	return nil
}

func (m *memDirectory) ReplaceLink(_ context.Context, account, oldID string, l directory.Link) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.links[oldID]
	if !ok || old.owner != account {
		return directory.ErrNotFound
	}
	if _, taken := m.links[l.ID]; taken {
		return directory.ErrConflict
	}
	delete(m.links, oldID)
	delete(m.boxes, old.link.Mailbox)
	m.links[l.ID] = &memLink{owner: account, link: l}
	m.boxes[l.Mailbox] = directory.Mailbox{ID: l.Mailbox, Owner: account, Link: l.ID}
	return nil
}

func (m *memDirectory) Link(_ context.Context, id string) (directory.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[id]
	if !ok {
		return directory.Link{}, directory.ErrNotFound
	}
	return l.link, nil
}

func (m *memDirectory) SetLinkKeys(_ context.Context, account, id string, keys []links.Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[id]
	if !ok || l.owner != account {
		return directory.ErrNotFound
	}
	l.link.Keys = keys
	return nil
}

func (m *memDirectory) DeleteLink(_ context.Context, account, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[id]
	if !ok || l.owner != account {
		return directory.ErrNotFound
	}
	delete(m.links, id)
	delete(m.boxes, l.link.Mailbox)
	m.accounts[account].links--
	return nil
}

func (m *memDirectory) ByTelegram(_ context.Context, th string) (directory.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return directory.Account{}, m.fail
	}
	id, ok := m.telegram[th]
	if !ok {
		return directory.Account{}, directory.ErrNotFound
	}
	a := m.accounts[id]
	devices := map[string][]byte{}
	for k, v := range a.devices {
		devices[k] = v
	}
	return directory.Account{ID: id, Key: a.key, Devices: devices}, nil
}

func (m *memDirectory) put(n directory.NewAccount) {
	m.telegram[n.TelegramHash] = n.ID
	m.accounts[n.ID] = &memAccount{key: n.AccountKey, th: n.TelegramHash, devices: map[string][]byte{n.DeviceID: n.DeviceKey}}
}

func (m *memDirectory) Create(_ context.Context, n directory.NewAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.telegram[n.TelegramHash]; ok {
		return directory.ErrConflict
	}
	m.put(n)
	return nil
}

func (m *memDirectory) Replace(_ context.Context, old directory.Account, n directory.NewAccount, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.telegram[n.TelegramHash] != old.ID {
		return directory.ErrConflict
	}
	m.accounts[old.ID].replaced = true
	m.accounts[old.ID].th = ""
	for id, l := range m.links {
		if l.owner == old.ID {
			delete(m.links, id)
		}
	}
	for id, b := range m.boxes {
		if b.Owner == old.ID {
			delete(m.boxes, id)
		}
	}
	m.put(n)
	return nil
}

func (m *memDirectory) Device(_ context.Context, account, device string, _ time.Time) (directory.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[account]
	if !ok || a.devices[device] == nil {
		return directory.Device{}, directory.ErrNotFound
	}
	return directory.Device{
		Key: a.devices[device], Replaced: a.replaced, TelegramHash: a.th,
		Member: a.member, Level: a.level, Granted: a.granted, Allocated: a.allocated, Links: a.links,
		Notices: a.chat != nil,
	}, nil
}

func (m *memDirectory) SetNotices(_ context.Context, account string, sealed []byte, language string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[account]
	if !ok {
		return directory.ErrNotFound
	}
	a.chat, a.language = sealed, language
	return nil
}

type memLimiter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (l *memLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return l.AllowN(ctx, key, 1, limit, window)
}

func (l *memLimiter) AllowN(_ context.Context, key string, n, limit int, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n < 1 || l.counts[key]+n > limit {
		return false, nil
	}
	l.counts[key] += n
	return true, nil
}

type memSpent struct {
	mu    sync.Mutex
	spent map[string]bool
}

func (m *memSpent) Spend(_ context.Context, nonce []byte, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spent[string(nonce)] {
		return false, nil
	}
	m.spent[string(nonce)] = true
	return true, nil
}

// testIssuer is one issuer key for the whole test binary (RSA keys are slow to make).
var testIssuer = sync.OnceValue(func() *tokens.Issuer {
	key, err := rsa.GenerateKey(rand.Reader, tokens.ModulusBits)
	if err != nil {
		panic(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	issuer, err := tokens.NewIssuer(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if err != nil {
		panic(err)
	}
	return issuer
})

// mintToken does what a client does: blind a token input for this week, have
// the issuer sign it, unblind.
func mintToken(t *testing.T, now time.Time) string {
	t.Helper()
	issuer := testIssuer()
	keyID := issuer.KeyID()
	challenge := tokens.Challenge(tokens.EpochOf(now))
	input := binary.BigEndian.AppendUint16(nil, tokens.Type)
	input = append(append(append(input, randomBytes(t, 32)...), challenge[:]...), keyID[:]...)
	client, err := blindrsa.NewClient(blindrsa.SHA384PSSDeterministic, issuer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	blinded, state, err := client.Blind(rand.Reader, input)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := issuer.Sign(blinded)
	if err != nil {
		t.Fatal(err)
	}
	final, err := client.Finalize(state, sig)
	if err != nil {
		t.Fatal(err)
	}
	return b64(append(input, final...))
}

// ---- test server and client ----

type harness struct {
	server *Server
	router *httpapi.Router
	dir    *memDirectory
	inbox  *memInbox
	logs   *bytes.Buffer
	now    time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	issuer, err := session.NewIssuer(map[byte][]byte{1: bytes.Repeat([]byte("k"), 32)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{dir: newMemDirectory(), logs: &bytes.Buffer{}, now: start, inbox: &memInbox{items: map[string][]inbox.Item{}}}
	h.server = &Server{
		Telegram:  initdatatest.Verifier(botID),
		Sessions:  issuer,
		Directory: h.dir,
		Nonces:    session.NewMemoryNonces(func() time.Time { return h.now }),
		Limits:    &memLimiter{counts: map[string]int{}},
		Pepper:    []byte("pepper-pepper-pepper-pepper-pepper"),
		Log:       logx.New(h.logs, "api"),
		Now:       func() time.Time { return h.now },
		Inbox:     h.inbox,
		Issuer:    testIssuer(),
		Spent:     &memSpent{spent: map[string]bool{}},
	}
	h.router = httpapi.NewRouter()
	h.server.Routes(h.router)
	return h
}

func (h *harness) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var req httpapi.Request
	req.RawPath = path
	req.RequestContext.HTTP.Method = http.MethodPost
	req.Headers = map[string]string{"content-type": "application/json"}
	req.Body = string(raw)
	resp, err := h.router.Serve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
		t.Fatalf("response body %q: %v", resp.Body, err)
	}
	return resp.StatusCode, out
}

// device is a client install: its keys, as the web client creates them.
type device struct {
	ak, dsk *ecdsa.PrivateKey
	id      string
	account string
	user    int64 // Telegram user ID
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func newDevice(t *testing.T) *device {
	t.Helper()
	ak, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	dsk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return &device{ak: ak, dsk: dsk, id: ids.New(), user: userID}
}

func raw(t *testing.T, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func signP1363(t *testing.T, k *ecdsa.PrivateKey, msg []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, k, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func launch(t *testing.T, user int64, startParam string, authDate time.Time) string {
	t.Helper()
	fields := map[string]string{
		"auth_date":     strconv.FormatInt(authDate.Unix(), 10),
		"chat_instance": "-1",
		"chat_type":     "sender",
		"user":          `{"id":` + strconv.FormatInt(user, 10) + `,"first_name":"A","allows_write_to_pm":true}`,
	}
	if startParam != "" {
		fields["start_param"] = startParam
	}
	return initdatatest.Sign(botID, fields)
}

func (d *device) firstList(t *testing.T, issued time.Time) devicelist.List {
	issued = issued.Truncate(time.Hour)
	return devicelist.List{
		Seq:     1,
		Issued:  issued,
		Expires: issued.Add(devicelist.MaxValidity),
		Entries: []devicelist.Entry{{DeviceID: d.id, Key: raw(t, d.dsk), Added: issued}},
	}
}

// registration builds a /register body; change can alter it before signing.
func (d *device) registration(t *testing.T, now time.Time, change func(r map[string]any, list *devicelist.List, initData *string)) map[string]any {
	t.Helper()
	initData := launch(t, d.user, RegistrationPrefix+devicesig.Fingerprint(raw(t, d.ak)), now.Add(-30*time.Second))
	list := d.firstList(t, now)
	body := map[string]any{}
	if change != nil {
		change(body, &list, &initData)
	}
	listBytes := devicelist.Encode(list)
	parts := RegisterParts(initData, raw(t, d.ak), d.id, raw(t, d.dsk))
	msg := devicesig.Message(devicesig.LabelRegister, parts...)
	defaults := map[string]any{
		"initData":      initData,
		"accountKey":    b64(raw(t, d.ak)),
		"device":        d.id,
		"deviceKey":     b64(raw(t, d.dsk)),
		"deviceList":    b64(listBytes),
		"deviceListSig": b64(signP1363(t, d.ak, listBytes)),
		"accountSig":    b64(signP1363(t, d.ak, msg)),
		"deviceSig":     b64(signP1363(t, d.dsk, msg)),
	}
	for k, v := range defaults {
		if _, set := body[k]; !set {
			body[k] = v
		}
	}
	return body
}

func (d *device) login(t *testing.T, now time.Time) map[string]any {
	t.Helper()
	login := session.Login{
		AccountID: d.account,
		DeviceID:  d.id,
		Timestamp: now.Unix(),
		Nonce:     b64(randomBytes(t, 16)),
	}
	sig := signP1363(t, d.dsk, devicesig.Message(devicesig.LabelSession, session.LoginParts(login)...))
	return map[string]any{
		"account":   login.AccountID,
		"device":    login.DeviceID,
		"timestamp": login.Timestamp,
		"nonce":     login.Nonce,
		"signature": b64(sig),
	}
}

func (h *harness) register(t *testing.T, d *device, replace bool) (int, map[string]any) {
	t.Helper()
	body := d.registration(t, h.now, nil)
	if replace {
		body["replace"] = true
	}
	status, out := h.post(t, "/register", body)
	if status == http.StatusOK {
		d.account = out["account"].(string)
	}
	return status, out
}

func wantError(t *testing.T, status int, out map[string]any, wantStatus int, code string) {
	t.Helper()
	if status != wantStatus || out["error"] != code {
		t.Fatalf("got %d %v, want %d %s", status, out, wantStatus, code)
	}
}

// ---- tests ----

func TestRegisterAndLogIn(t *testing.T) {
	h := newHarness(t)
	d := newDevice(t)

	status, out := h.register(t, d, false)
	if status != http.StatusOK {
		t.Fatalf("register: %d %v", status, out)
	}
	claims, err := h.server.Sessions.Verify(out["token"].(string), h.now)
	if err != nil || claims.AccountID != d.account || claims.DeviceID != d.id {
		t.Fatalf("token: %+v, %v", claims, err)
	}
	if int64(out["expires"].(float64)) != claims.Expires.Unix() {
		t.Fatalf("expires = %v, token says %v", out["expires"], claims.Expires.Unix())
	}

	// The directory holds a keyed hash, never the Telegram user ID.
	for th := range h.dir.telegram {
		if strings.Contains(th, strconv.Itoa(userID)) || th != directory.TelegramHash(h.server.Pepper, userID) {
			t.Fatalf("telegram hash %q", th)
		}
	}

	status, out = h.post(t, "/session", d.login(t, h.now))
	if status != http.StatusOK || out["account"] != d.account {
		t.Fatalf("login: %d %v", status, out)
	}
}

func TestRegisterRetryIsIdempotent(t *testing.T) {
	h := newHarness(t)
	d := newDevice(t)
	body := d.registration(t, h.now, nil)
	status, first := h.post(t, "/register", body)
	if status != http.StatusOK {
		t.Fatalf("first: %d %v", status, first)
	}
	status, again := h.post(t, "/register", body)
	if status != http.StatusOK || again["account"] != first["account"] {
		t.Fatalf("retry: %d %v, first account %v", status, again, first["account"])
	}
	if len(h.dir.accounts) != 1 {
		t.Fatalf("%d accounts", len(h.dir.accounts))
	}
}

func TestReplaceAccount(t *testing.T) {
	h := newHarness(t)
	old := newDevice(t)
	if status, out := h.register(t, old, false); status != http.StatusOK {
		t.Fatalf("register: %d %v", status, out)
	}

	// Setting up again with new keys needs an explicit replace.
	fresh := newDevice(t)
	status, out := h.register(t, fresh, false)
	wantError(t, status, out, http.StatusConflict, "account_exists")

	if status, out := h.register(t, fresh, true); status != http.StatusOK || fresh.account == old.account {
		t.Fatalf("replace: %d %v", status, out)
	}

	// The old device is told, loudly, once its signature checks out.
	status, out = h.post(t, "/session", old.login(t, h.now))
	wantError(t, status, out, http.StatusGone, "account_replaced")

	// Without a valid signature it learns nothing.
	forged := old.login(t, h.now)
	forged["signature"] = b64(make([]byte, 64))
	status, out = h.post(t, "/session", forged)
	wantError(t, status, out, http.StatusUnauthorized, "login_refused")

	if status, out := h.post(t, "/session", fresh.login(t, h.now)); status != http.StatusOK {
		t.Fatalf("new device login: %d %v", status, out)
	}
}

func TestRegisterRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := range RegistrationsPerDay {
		if status, out := h.register(t, newDevice(t), true); status != http.StatusOK {
			t.Fatalf("registration %d: %d %v", i+1, status, out)
		}
	}
	status, out := h.register(t, newDevice(t), true)
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")
}

func TestRegisterRejects(t *testing.T) {
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	tests := []struct {
		name   string
		change func(t *testing.T, d *device, r map[string]any, list *devicelist.List, initData *string)
		status int
		code   string
	}{
		{"launch without start_param", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			*initData = launch(t, userID, "", start)
		}, http.StatusUnauthorized, "init_data_rejected"},
		{"launch from a link", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			*initData = launch(t, userID, "aBcDeFgHiJkLmNoPqRsTuV", start)
		}, http.StatusUnauthorized, "init_data_rejected"},
		{"another account key's code", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			*initData = launch(t, userID, RegistrationPrefix+devicesig.Fingerprint(raw(t, other)), start)
		}, http.StatusUnauthorized, "init_data_rejected"},
		{"old launch", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			*initData = launch(t, userID, RegistrationPrefix+devicesig.Fingerprint(raw(t, d.ak)), start.Add(-11*time.Minute))
		}, http.StatusUnauthorized, "init_data_rejected"},
		{"tampered launch", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			*initData = strings.Replace(*initData, "first_name%22%3A%22A", "first_name%22%3A%22B", 1)
		}, http.StatusUnauthorized, "init_data_rejected"},
		{"account signature by another key", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			r["accountSig"] = b64(signP1363(t, other, devicesig.Message(devicesig.LabelRegister,
				RegisterParts(*initData, raw(t, d.ak), d.id, raw(t, d.dsk))...)))
		}, http.StatusUnauthorized, "signature_rejected"},
		{"device signature over another launch", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			r["deviceSig"] = b64(signP1363(t, d.dsk, devicesig.Message(devicesig.LabelRegister,
				RegisterParts(*initData+"x", raw(t, d.ak), d.id, raw(t, d.dsk))...)))
		}, http.StatusUnauthorized, "signature_rejected"},
		{"session signature instead", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, initData *string) {
			r["deviceSig"] = b64(signP1363(t, d.dsk, devicesig.Message(devicesig.LabelSession,
				RegisterParts(*initData, raw(t, d.ak), d.id, raw(t, d.dsk))...)))
		}, http.StatusUnauthorized, "signature_rejected"},
		{"list not first", func(t *testing.T, d *device, _ map[string]any, list *devicelist.List, _ *string) {
			list.Seq = 2
		}, http.StatusBadRequest, "invalid_device_list"},
		{"list names another device", func(t *testing.T, d *device, _ map[string]any, list *devicelist.List, _ *string) {
			list.Entries[0].DeviceID = ids.New()
		}, http.StatusBadRequest, "invalid_device_list"},
		{"list with a second device", func(t *testing.T, d *device, _ map[string]any, list *devicelist.List, _ *string) {
			list.Entries = append(list.Entries, devicelist.Entry{DeviceID: ids.New(), Key: raw(t, other), Added: list.Issued})
		}, http.StatusBadRequest, "invalid_device_list"},
		{"stale list", func(t *testing.T, d *device, _ map[string]any, list *devicelist.List, _ *string) {
			*list = d.firstList(t, start.Add(-3*time.Hour))
		}, http.StatusBadRequest, "invalid_device_list"},
		{"list signed by the device key", func(t *testing.T, d *device, r map[string]any, list *devicelist.List, _ *string) {
			r["deviceListSig"] = b64(signP1363(t, d.dsk, devicelist.Encode(*list)))
		}, http.StatusBadRequest, "invalid_device_list"},
		{"bad device ID", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, _ *string) {
			r["device"] = "short"
		}, http.StatusBadRequest, "bad_request"},
		{"bad account key", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, _ *string) {
			r["accountKey"] = b64(bytes.Repeat([]byte{4}, 65))
		}, http.StatusBadRequest, "bad_request"},
		{"padded base64", func(t *testing.T, d *device, r map[string]any, _ *devicelist.List, _ *string) {
			r["deviceKey"] = base64.URLEncoding.EncodeToString(raw(t, d.dsk))
		}, http.StatusBadRequest, "bad_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			d := newDevice(t)
			body := d.registration(t, h.now, func(r map[string]any, list *devicelist.List, initData *string) {
				tt.change(t, d, r, list, initData)
			})
			status, out := h.post(t, "/register", body)
			wantError(t, status, out, tt.status, tt.code)
			if len(h.dir.accounts) != 0 {
				t.Fatal("an account was stored")
			}
		})
	}
}

func TestSessionRejects(t *testing.T) {
	h := newHarness(t)
	d := newDevice(t)
	if status, out := h.register(t, d, false); status != http.StatusOK {
		t.Fatalf("register: %d %v", status, out)
	}

	login := d.login(t, h.now)
	if status, out := h.post(t, "/session", login); status != http.StatusOK {
		t.Fatalf("login: %d %v", status, out)
	}
	status, out := h.post(t, "/session", login)
	wantError(t, status, out, http.StatusUnauthorized, "login_refused") // replayed

	stale := d.login(t, h.now.Add(-2*time.Minute))
	status, out = h.post(t, "/session", stale)
	wantError(t, status, out, http.StatusUnauthorized, "login_refused")

	unknown := newDevice(t)
	unknown.account = d.account
	status, out = h.post(t, "/session", unknown.login(t, h.now))
	wantError(t, status, out, http.StatusUnauthorized, "login_refused")

	otherAccount := d.login(t, h.now)
	otherAccount["account"] = ids.New()
	status, out = h.post(t, "/session", otherAccount)
	wantError(t, status, out, http.StatusUnauthorized, "login_refused")

	bad := d.login(t, h.now)
	bad["account"] = "x"
	status, out = h.post(t, "/session", bad)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
}

func TestRequiresJSON(t *testing.T) {
	h := newHarness(t)
	var req httpapi.Request
	req.RawPath = "/session"
	req.RequestContext.HTTP.Method = http.MethodPost
	req.Headers = map[string]string{"content-type": "text/plain"}
	req.Body = "{}"
	resp, _ := h.router.Serve(context.Background(), req)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestInternalErrorsLogNothingAboutTheRequest(t *testing.T) {
	h := newHarness(t)
	h.dir.fail = errors.New("table " + strconv.Itoa(userID) + " unavailable")
	d := newDevice(t)
	status, out := h.register(t, d, false)
	wantError(t, status, out, http.StatusInternalServerError, "internal")

	logs := h.logs.String()
	if !strings.Contains(logs, `"route":"POST /register"`) || !strings.Contains(logs, `"status":500`) {
		t.Fatalf("logs = %s", logs)
	}
	for _, secret := range []string{strconv.Itoa(userID), d.id, "unavailable", b64(raw(t, d.ak))} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs contain %q: %s", secret, logs)
		}
	}
}
