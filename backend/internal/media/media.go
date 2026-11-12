// Package media handles encrypted attachments (design phase 5). The app
// encrypts a file, pads it, and uploads only the ciphertext, straight to S3
// with a presigned URL. The recipient downloads it the same way, decrypts it,
// keeps it on the device, and deletes it from S3. Whatever isn't fetched
// expires. The server never sees a file's key, name or type.
package media

import (
	"context"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	// MaxSize is the largest upload: a 25 MB file, padded, plus encryption overhead.
	MaxSize = 26 << 20
	// TokenBytes is how much one send token pays for: an upload costs one
	// token per started 5 MiB.
	TokenBytes = 5 << 20
	// UploadURLTTL and DownloadURLTTL are how long presigned URLs work.
	UploadURLTTL   = 15 * time.Minute
	DownloadURLTTL = 10 * time.Minute
	// Retention is how long an upload waits to be fetched: undelivered
	// messages wait 30 days.
	Retention = 31 * 24 * time.Hour
	// ContentType is the only type uploads carry: the server doesn't learn
	// what a file is.
	ContentType = "application/octet-stream"
)

// TokensFor is how many send tokens an upload of size bytes costs.
func TokensFor(size int64) int { return int((size + TokenBytes - 1) / TokenBytes) }

// Presigner signs S3 requests for the app to make (s3.PresignClient).
type Presigner interface {
	PresignPutObject(context.Context, *s3.PutObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// Deleter deletes objects (s3.Client).
type Deleter interface {
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Bucket is the media bucket.
type Bucket struct {
	Presign Presigner
	S3      Deleter
	Name    string
}

func key(id string) *string {
	k := "m/" + id
	return &k
}

// UploadURL returns a URL that accepts exactly size bytes for id, once
// signed: a PUT of any other length, or with another type, is refused.
func (b *Bucket) UploadURL(ctx context.Context, id string, size int64) (string, error) {
	contentType := ContentType
	req, err := b.Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        &b.Name,
		Key:           key(id),
		ContentLength: &size,
		ContentType:   &contentType,
	}, s3.WithPresignExpires(UploadURLTTL))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// DownloadURL returns a URL that fetches id's ciphertext.
func (b *Bucket) DownloadURL(ctx context.Context, id string) (string, error) {
	req, err := b.Presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &b.Name, Key: key(id)}, s3.WithPresignExpires(DownloadURLTTL))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// Delete deletes id's ciphertext. Deleting one that's gone is fine.
func (b *Bucket) Delete(ctx context.Context, id string) error {
	_, err := b.S3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.Name, Key: key(id)})
	return err
}
