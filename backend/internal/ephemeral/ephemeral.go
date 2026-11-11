// Package ephemeral keeps short-lived records in DynamoDB: used login nonces
// and rate counters. Every item expires through the table's TTL attribute e,
// and reads treat expired items as missing because TTL deletion can lag.
//
//	N#<nonce>  N              a used login nonce
//	S#<nonce>  S              a spent send token's nonce
//	Q#<key>    Q              a quiet period (e.g. after a notice)
//	R#<key>    <window start> a rate counter (c)
//
// Neither kind holds an account or device ID.
package ephemeral

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DB is the part of the DynamoDB client this package uses.
type DB interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// Nonces records used login nonces. It implements session.NonceStore.
type Nonces struct {
	DB    DB
	Table string
	Now   func() time.Time
}

// Use records nonce until expires, rounded up to the minute. It reports false
// if the nonce was already used and hasn't expired.
func (n *Nonces) Use(ctx context.Context, nonce string, expires time.Time) (bool, error) {
	return once(ctx, n.DB, n.Table, "N#"+nonce, "N", expires, n.Now())
}

// Spent records spent send tokens. Tokens say nothing about who spent them,
// and neither does this.
type Spent struct {
	DB    DB
	Table string
	Now   func() time.Time
}

// Spend records a token's nonce until forgetAfter (when the token stops
// being accepted anyway). It reports false if the token was spent already.
func (sp *Spent) Spend(ctx context.Context, nonce []byte, forgetAfter time.Time) (bool, error) {
	return once(ctx, sp.DB, sp.Table, "S#"+base64.RawURLEncoding.EncodeToString(nonce), "S", forgetAfter, sp.Now())
}

// Quiet holds quiet periods: one action per key per period.
type Quiet struct {
	DB    DB
	Table string
	Now   func() time.Time
}

// Start begins a quiet period for key until until. It reports false if one
// is already running.
func (q *Quiet) Start(ctx context.Context, key string, until time.Time) (bool, error) {
	return once(ctx, q.DB, q.Table, "Q#"+key, "Q", until, q.Now())
}

// once writes an item unless a live one exists, and reports whether it did.
func once(ctx context.Context, db DB, table, pk, sk string, expires, now time.Time) (bool, error) {
	_, err := db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &table,
		Item: map[string]types.AttributeValue{
			"pk": s(pk),
			"sk": s(sk),
			"e":  num(expires.Add(time.Minute - time.Nanosecond).Truncate(time.Minute).Unix()),
		},
		ConditionExpression:       aws.String("attribute_not_exists(pk) OR e <= :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":now": num(now.Unix())},
	})
	if conditionFailed(err) {
		return false, nil
	}
	return err == nil, err
}

// Limiter counts uses of a key in fixed windows.
type Limiter struct {
	DB    DB
	Table string
	Now   func() time.Time
}

// Allow counts one use of key in the current window and reports whether it
// is within limit. Windows are aligned to multiples of window since the Unix
// epoch (so a day is a UTC day).
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return l.AllowN(ctx, key, 1, limit, window)
}

// AllowN counts n uses at once, allowing them only if all fit in the limit.
func (l *Limiter) AllowN(ctx context.Context, key string, n, limit int, window time.Duration) (bool, error) {
	if n < 1 || n > limit {
		return false, nil
	}
	now := l.Now()
	start := now.Truncate(window)
	// Kept an hour past the window; rounded up to the hour.
	expires := start.Add(window + 2*time.Hour - time.Nanosecond).Truncate(time.Hour)
	_, err := l.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &l.Table,
		Key: map[string]types.AttributeValue{
			"pk": s("R#" + key),
			"sk": s(strconv.FormatInt(start.Unix(), 10)),
		},
		UpdateExpression:    aws.String("ADD c :n SET e = :e"),
		ConditionExpression: aws.String("attribute_not_exists(c) OR c <= :most"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":n":    num(int64(n)),
			":e":    num(expires.Unix()),
			":most": num(int64(limit - n)),
		},
	})
	if conditionFailed(err) {
		return false, nil
	}
	return err == nil, err
}

func conditionFailed(err error) bool {
	var failed *types.ConditionalCheckFailedException
	return errors.As(err, &failed)
}

func s(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }

func num(v int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(v, 10)}
}
