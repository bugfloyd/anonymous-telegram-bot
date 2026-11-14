package ephemeral

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// SocketTTL is how long a connection record lives: API Gateway closes every
// socket after 2 hours, plus margin.
const SocketTTL = 2*time.Hour + 10*time.Minute

// MaxSockets is how many connections one account keeps on record. An
// account has one device, with a socket for each visible copy of the app,
// so a few are plenty. It bounds what a delivery costs: each one is pushed
// to every recorded connection of the recipient.
const MaxSockets = 5

// SocketDB is the part of the DynamoDB client Sockets uses.
type SocketDB interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// Sockets records which accounts have an app open, by WebSocket connection:
//
//	C#<account>     <connection>   an open socket
//	K#<connection>  K              its account (a), for $disconnect
//
// Nothing else is kept: no device, IP or time beyond the expiry.
type Sockets struct {
	DB    SocketDB
	Table string
	Now   func() time.Time
}

// Open records a connection for an account. If the account then has more
// than MaxSockets on record, the oldest are forgotten: they stay open but
// get no more pushes. Forgetting, rather than refusing the new one, means
// records of sockets that ended without a $disconnect never lock anyone out.
func (s *Sockets) Open(ctx context.Context, account, connection string) error {
	e := num(s.Now().Add(SocketTTL).Unix())
	if _, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.Table,
		Item:      map[string]types.AttributeValue{"pk": sv("K#" + connection), "sk": sv("K"), "a": sv(account), "e": e},
	}); err != nil {
		return err
	}
	if _, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.Table,
		Item:      map[string]types.AttributeValue{"pk": sv("C#" + account), "sk": sv(connection), "e": e},
	}); err != nil {
		return err
	}
	return s.trim(ctx, account, connection)
}

// trim forgets an account's oldest connections beyond MaxSockets, never the
// one just opened.
func (s *Sockets) trim(ctx context.Context, account, opened string) error {
	out, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.Table,
		KeyConditionExpression:    aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": sv("C#" + account)},
		ProjectionExpression:      aws.String("sk, e"),
		ConsistentRead:            aws.Bool(true),
	})
	if err != nil {
		return err
	}
	type record struct {
		connection string
		expires    int64
	}
	records := make([]record, 0, len(out.Items))
	for _, it := range out.Items {
		c, ok := it["sk"].(*types.AttributeValueMemberS)
		if !ok || c.Value == opened {
			continue
		}
		var expires int64
		if e, ok := it["e"].(*types.AttributeValueMemberN); ok {
			expires, _ = strconv.ParseInt(e.Value, 10, 64)
		}
		records = append(records, record{c.Value, expires})
	}
	// The opened connection takes one place.
	extra := len(records) - (MaxSockets - 1)
	if extra <= 0 {
		return nil
	}
	slices.SortFunc(records, func(a, b record) int { return int(a.expires - b.expires) })
	for _, r := range records[:extra] {
		if err := s.Forget(ctx, account, r.connection); err != nil {
			return err
		}
	}
	return nil
}

// Close forgets a connection that ended.
func (s *Sockets) Close(ctx context.Context, connection string) error {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.Table,
		Key:       map[string]types.AttributeValue{"pk": sv("K#" + connection), "sk": sv("K")},
	})
	if err != nil {
		return err
	}
	account, ok := out.Item["a"].(*types.AttributeValueMemberS)
	if !ok {
		return nil
	}
	return s.Forget(ctx, account.Value, connection)
}

// Forget deletes a connection's records (it closed, or is gone).
func (s *Sockets) Forget(ctx context.Context, account, connection string) error {
	if _, err := s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.Table,
		Key:       map[string]types.AttributeValue{"pk": sv("C#" + account), "sk": sv(connection)},
	}); err != nil {
		return err
	}
	_, err := s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.Table,
		Key:       map[string]types.AttributeValue{"pk": sv("K#" + connection), "sk": sv("K")},
	})
	return err
}

// Connections lists an account's open connections. An empty account has
// none, at the same cost (a send to an unknown mailbox asks for it).
func (s *Sockets) Connections(ctx context.Context, account string) ([]string, error) {
	out, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &s.Table,
		KeyConditionExpression:    aws.String("pk = :pk"),
		FilterExpression:          aws.String("e > :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": sv("C#" + account), ":now": num(s.Now().Unix())},
		ProjectionExpression:      aws.String("sk"),
	})
	if err != nil {
		return nil, err
	}
	conns := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		if c, ok := it["sk"].(*types.AttributeValueMemberS); ok {
			conns = append(conns, c.Value)
		}
	}
	return conns, nil
}

func sv(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
