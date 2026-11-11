// Package ephemeral keeps short-lived records in DynamoDB: used login nonces
// and rate counters. Every item expires through the table's TTL attribute e,
// and reads treat expired items as missing because TTL deletion can lag.
//
//	N#<nonce>  N              a used login nonce
//	R#<key>    <window start> a rate counter (c)
//
// Neither kind holds an account or device ID.
package ephemeral

import (
	"context"
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
	_, err := n.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &n.Table,
		Item: map[string]types.AttributeValue{
			"pk": s("N#" + nonce),
			"sk": s("N"),
			"e":  num(expires.Add(time.Minute - time.Nanosecond).Truncate(time.Minute).Unix()),
		},
		ConditionExpression:       aws.String("attribute_not_exists(pk) OR e <= :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":now": num(n.Now().Unix())},
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
		UpdateExpression:    aws.String("ADD c :one SET e = :e"),
		ConditionExpression: aws.String("attribute_not_exists(c) OR c < :limit"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one":   num(1),
			":e":     num(expires.Unix()),
			":limit": num(int64(limit)),
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
