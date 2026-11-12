package ephemeral

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
)

func TestUploads(t *testing.T) {
	client := dynamotest.Client(t)
	clock := time.Unix(1_800_000_000, 0)
	u := &Uploads{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()
	hash := bytes.Repeat([]byte{7}, 32)

	if _, ok, err := u.Get(ctx, "up1"); ok || err != nil {
		t.Fatalf("before: %v %v", ok, err)
	}
	if err := u.Put(ctx, "up1", Upload{DeleteHash: hash, Size: 1234}, clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := u.Put(ctx, "up1", Upload{DeleteHash: hash, Size: 1}, clock.Add(time.Hour)); err == nil {
		t.Fatal("a taken ID was accepted")
	}
	got, ok, err := u.Get(ctx, "up1")
	if err != nil || !ok || !bytes.Equal(got.DeleteHash, hash) || got.Size != 1234 {
		t.Fatalf("Get = %+v %v %v", got, ok, err)
	}
	clock = clock.Add(time.Hour)
	if _, ok, _ := u.Get(ctx, "up1"); ok {
		t.Fatal("expired upload still there")
	}
	if err := u.Delete(ctx, "up1"); err != nil {
		t.Fatal(err)
	}
}
