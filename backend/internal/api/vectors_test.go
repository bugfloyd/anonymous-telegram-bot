package api

import (
	"encoding/hex"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

// Signed messages computed independently (Node) and checked by the client
// too (web/src/account/account.test.ts), so the two sides can't drift apart.
const (
	vectorKey      = "043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1"
	vectorInitData = "auth_date=1800000000&start_param=reg&user=%7B%7D"
	vectorRegister = "0019696e636f676e69746f636861742f76322f726567697374657200000020ed6cc20d3628da6bf65e3033d5628dafcd44b7194eaac0d218b37621ab1b6c7300000041043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1000000164141414141414141414141414141414141414141414100000041043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1"
	vectorRequest  = "0018696e636f676e69746f636861742f76322f726571756573740000001642424242424242424242424242424242424242424242000000164141414141414141414141414141414141414141414100000004504f5354000000062f6c696e6b730000000a31383030303030303030000000126e6f6e63652d30313233343536373839616200000020015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"
	vectorLogin    = "0018696e636f676e69746f636861742f76322f73657373696f6e000000164242424242424242424242424242424242424242424200000016414141414141414141414141414141414141414141410000000a31383030303030303030000000126e6f6e63652d303132333435363738396162"
)

func TestSignedMessageVectors(t *testing.T) {
	key, err := hex.DecodeString(vectorKey)
	if err != nil {
		t.Fatal(err)
	}
	register := devicesig.Message(devicesig.LabelRegister, RegisterParts(vectorInitData, key, "AAAAAAAAAAAAAAAAAAAAAA", key)...)
	if got := hex.EncodeToString(register); got != vectorRegister {
		t.Errorf("register message =\n%s\nwant\n%s", got, vectorRegister)
	}
	login := devicesig.Message(devicesig.LabelSession, session.LoginParts(session.Login{
		AccountID: "BBBBBBBBBBBBBBBBBBBBBB",
		DeviceID:  "AAAAAAAAAAAAAAAAAAAAAA",
		Timestamp: 1_800_000_000,
		Nonce:     "nonce-0123456789ab",
	})...)
	if got := hex.EncodeToString(login); got != vectorLogin {
		t.Errorf("login message =\n%s\nwant\n%s", got, vectorLogin)
	}
	request := devicesig.Message(devicesig.LabelRequest, session.RequestParts(session.SignedRequest{
		AccountID: "BBBBBBBBBBBBBBBBBBBBBB",
		DeviceID:  "AAAAAAAAAAAAAAAAAAAAAA",
		Method:    "POST",
		Path:      "/links",
		Body:      []byte(`{"a":1}`),
		Timestamp: 1_800_000_000,
		Nonce:     "nonce-0123456789ab",
	})...)
	if got := hex.EncodeToString(request); got != vectorRequest {
		t.Errorf("request message =\n%s\nwant\n%s", got, vectorRequest)
	}
}
