package media

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// newBucket uses the real presigner (signing needs no network).
func newBucket() *Bucket {
	client := s3.New(s3.Options{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", "token"),
		// The SDK would otherwise add a checksum of an empty body to the
		// signature, and the browser's upload would be refused.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
	return &Bucket{Presign: s3.NewPresignClient(client), S3: client, Name: "incognitochat-test-media-abc"}
}

func TestUploadURLSignsTheExactLengthAndType(t *testing.T) {
	raw, err := newBucket().UploadURL(context.Background(), "IDIDIDIDIDIDIDIDIDIDID", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || !strings.HasPrefix(u.Host, "incognitochat-test-media-abc.s3.us-east-1.") || u.Path != "/m/IDIDIDIDIDIDIDIDIDIDID" {
		t.Fatalf("url = %s", raw)
	}
	q := u.Query()
	signed := strings.Split(q.Get("X-Amz-SignedHeaders"), ";")
	for _, h := range []string{"content-length", "content-type", "host"} {
		if !contains(signed, h) {
			t.Errorf("%s not signed: %v", h, signed)
		}
	}
	if q.Get("X-Amz-Expires") != "900" {
		t.Errorf("expires = %s", q.Get("X-Amz-Expires"))
	}
	for k := range q {
		if strings.Contains(strings.ToLower(k), "checksum") {
			t.Errorf("checksum in the URL: %s", k)
		}
	}
}

func TestDownloadURL(t *testing.T) {
	raw, err := newBucket().DownloadURL(context.Background(), "IDIDIDIDIDIDIDIDIDIDID")
	if err != nil || !strings.Contains(raw, "/m/IDIDIDIDIDIDIDIDIDIDID?") || !strings.Contains(raw, "X-Amz-Expires=600") {
		t.Fatalf("url = %s, %v", raw, err)
	}
}

func TestTokensFor(t *testing.T) {
	for size, want := range map[int64]int{1: 1, TokenBytes: 1, TokenBytes + 1: 2, MaxSize: 6} {
		if got := TokensFor(size); got != want {
			t.Errorf("TokensFor(%d) = %d, want %d", size, got, want)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
