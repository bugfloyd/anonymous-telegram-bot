package ephemeral

import (
	"context"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Uploads records media uploads until they're fetched and deleted, or
// expire:
//
//	U#<id>  U  the SHA-256 of its delete secret (d), its size (s)
//
// Nothing says who uploaded it or who it's for.
type Uploads struct {
	DB    SocketDB
	Table string
	Now   func() time.Time
}

// Upload is a recorded upload.
type Upload struct {
	DeleteHash []byte
	Size       int64
}

// Put records an upload until expires. It fails if the ID is taken.
func (u *Uploads) Put(ctx context.Context, id string, up Upload, expires time.Time) error {
	_, err := u.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &u.Table,
		Item: map[string]types.AttributeValue{
			"pk": sv("U#" + id), "sk": sv("U"),
			"d": &types.AttributeValueMemberB{Value: up.DeleteHash},
			"s": num(up.Size),
			"e": num(expires.Unix()),
		},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	return err
}

// Get returns an upload, or false if there is none (or it expired).
func (u *Uploads) Get(ctx context.Context, id string) (Upload, bool, error) {
	out, err := u.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &u.Table,
		Key:            map[string]types.AttributeValue{"pk": sv("U#" + id), "sk": sv("U")},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil || out.Item == nil {
		return Upload{}, false, err
	}
	// Expired items count as gone: TTL deletion can lag by days.
	expires, ok1 := intAttr(out.Item, "e")
	size, ok2 := intAttr(out.Item, "s")
	d, ok3 := out.Item["d"].(*types.AttributeValueMemberB)
	if !ok1 || !ok2 || !ok3 || expires <= u.Now().Unix() {
		return Upload{}, false, nil
	}
	return Upload{DeleteHash: d.Value, Size: size}, true, nil
}

func intAttr(item map[string]types.AttributeValue, name string) (int64, bool) {
	n, ok := item[name].(*types.AttributeValueMemberN)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	return v, err == nil
}

// Delete forgets an upload.
func (u *Uploads) Delete(ctx context.Context, id string) error {
	_, err := u.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &u.Table,
		Key:       map[string]types.AttributeValue{"pk": sv("U#" + id), "sk": sv("U")},
	})
	return err
}
