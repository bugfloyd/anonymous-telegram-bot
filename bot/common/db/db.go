// Package db provides the DynamoDB client shared by all repositories.
package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// OpTimeout bounds every DynamoDB call so a slow request can't hang a handler.
const OpTimeout = 5 * time.Second

// Client returns a DynamoDB client, created once per process (Lambda
// execution environment). DYNAMODB_ENDPOINT points it at a local DynamoDB for
// development and tests.
var Client = sync.OnceValues(func() (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint := os.Getenv("DYNAMODB_ENDPOINT"); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
})

// OpContext returns a context with OpTimeout.
func OpContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), OpTimeout)
}

// IsConditionFailed reports whether err is a failed condition on a single-item write.
func IsConditionFailed(err error) bool {
	var failed *types.ConditionalCheckFailedException
	return errors.As(err, &failed)
}

// Cancellation reason codes for TransactWriteItems.
const (
	ConditionFailed     = "ConditionalCheckFailed"
	TransactionConflict = "TransactionConflict"
)

// CancellationReasons returns, for a canceled TransactWriteItems call, each
// item's cancellation code by position (e.g. ConditionFailed,
// TransactionConflict, "None"). It returns nil for other errors.
func CancellationReasons(err error) []string {
	var canceled *types.TransactionCanceledException
	if !errors.As(err, &canceled) {
		return nil
	}
	reasons := make([]string, len(canceled.CancellationReasons))
	for i, reason := range canceled.CancellationReasons {
		reasons[i] = aws.ToString(reason.Code)
	}
	return reasons
}

// RetryPause waits a little longer on each attempt, with jitter, before
// retrying a transaction that conflicted with a concurrent one.
func RetryPause(attempt int) {
	time.Sleep(time.Duration(attempt+1)*40*time.Millisecond + time.Duration(rand.IntN(40))*time.Millisecond)
}
