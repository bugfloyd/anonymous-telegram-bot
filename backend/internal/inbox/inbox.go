// Package inbox stores undelivered envelopes in the messages table, one item
// per message per device (design §12, §14):
//
//	D#<account>#<device>  <random ID>  mailbox (m), envelope (b), expiry (e)
//
// No sender, no account of the sender, no precise time: the expiry is 30 days
// after delivery, rounded up to the hour. A device confirms what it stored,
// which deletes its items; whatever isn't confirmed expires.
package inbox

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

const (
	// TTL is how long an undelivered message is kept (design D13).
	TTL = 30 * 24 * time.Hour

	batchSize  = 25
	maxRetries = 5
)

// DB is the part of the DynamoDB client the inbox uses.
type DB interface {
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

// Store is the messages table.
type Store struct {
	DB    DB
	Table string
}

// Item is one undelivered envelope.
type Item struct {
	ID       string
	Mailbox  string
	Envelope []byte
}

func devicePK(accountID, deviceID string) string { return "D#" + accountID + "#" + deviceID }

// Deliver stores an envelope once for each of the owner's devices.
func (s *Store) Deliver(ctx context.Context, accountID string, devices []string, mailbox string, envelope []byte, now time.Time) error {
	expires := strconv.FormatInt(now.Add(TTL+time.Hour-time.Nanosecond).Truncate(time.Hour).Unix(), 10)
	requests := make([]types.WriteRequest, 0, len(devices))
	for _, d := range devices {
		requests = append(requests, types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: devicePK(accountID, d)},
			"sk": &types.AttributeValueMemberS{Value: ids.New()},
			"m":  &types.AttributeValueMemberS{Value: mailbox},
			"b":  &types.AttributeValueMemberB{Value: envelope},
			"e":  &types.AttributeValueMemberN{Value: expires},
		}}})
	}
	return s.write(ctx, requests)
}

// List returns up to limit of a device's undelivered envelopes, in no
// meaningful order (random IDs), and whether there are more.
func (s *Store) List(ctx context.Context, accountID, deviceID string, limit int, now time.Time) ([]Item, bool, error) {
	out, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:              &s.Table,
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: devicePK(accountID, deviceID)},
		},
		Limit:          aws.Int32(int32(limit)),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, false, err
	}
	items := make([]Item, 0, len(out.Items))
	for _, it := range out.Items {
		if e, ok := it["e"].(*types.AttributeValueMemberN); ok {
			if v, err := strconv.ParseInt(e.Value, 10, 64); err != nil || v <= now.Unix() {
				continue // expired, not yet removed by TTL
			}
		}
		id, ok1 := it["sk"].(*types.AttributeValueMemberS)
		mb, ok2 := it["m"].(*types.AttributeValueMemberS)
		env, ok3 := it["b"].(*types.AttributeValueMemberB)
		if ok1 && ok2 && ok3 {
			items = append(items, Item{ID: id.Value, Mailbox: mb.Value, Envelope: env.Value})
		}
	}
	return items, len(out.LastEvaluatedKey) > 0, nil
}

// Ack deletes a device's items that it has stored. Unknown IDs are ignored.
func (s *Store) Ack(ctx context.Context, accountID, deviceID string, itemIDs []string) error {
	requests := make([]types.WriteRequest, 0, len(itemIDs))
	for _, id := range itemIDs {
		requests = append(requests, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: devicePK(accountID, deviceID)},
			"sk": &types.AttributeValueMemberS{Value: id},
		}}})
	}
	return s.write(ctx, requests)
}

func (s *Store) write(ctx context.Context, requests []types.WriteRequest) error {
	for len(requests) > 0 {
		n := min(batchSize, len(requests))
		pending := map[string][]types.WriteRequest{s.Table: requests[:n]}
		requests = requests[n:]
		for attempt := 0; len(pending[s.Table]) > 0; attempt++ {
			if attempt == maxRetries {
				return errors.New("inbox: batch write left items unprocessed")
			}
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(50<<attempt) * time.Millisecond):
				}
			}
			out, err := s.DB.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: pending})
			if err != nil {
				return err
			}
			pending = out.UnprocessedItems
		}
	}
	return nil
}
