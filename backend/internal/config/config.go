// Package config reads a function's settings at startup: names from its
// environment, secrets from SSM Parameter Store. Errors name what is missing,
// never a value.
package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// maxPerCall is SSM's limit for GetParameters.
const maxPerCall = 10

// Parameters is the part of the SSM client Load uses.
type Parameters interface {
	GetParameters(context.Context, *ssm.GetParametersInput, ...func(*ssm.Options)) (*ssm.GetParametersOutput, error)
}

// Env returns the named environment variables. Every one must be set.
func Env(getenv func(string) string, names ...string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	var missing []string
	for _, name := range names {
		v := getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		values[name] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: environment variables not set: %s", strings.Join(missing, ", "))
	}
	return values, nil
}

// Load reads parameters by name, decrypted. Every one must exist and be
// non-empty.
func Load(ctx context.Context, client Parameters, names ...string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	for len(names) > 0 {
		batch := names[:min(maxPerCall, len(names))]
		names = names[len(batch):]
		out, err := client.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          batch,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return nil, err
		}
		if len(out.InvalidParameters) > 0 {
			return nil, fmt.Errorf("config: parameters not found: %s", strings.Join(out.InvalidParameters, ", "))
		}
		for _, p := range out.Parameters {
			values[aws.ToString(p.Name)] = aws.ToString(p.Value)
		}
		for _, name := range batch {
			if values[name] == "" {
				return nil, fmt.Errorf("config: parameter %s is empty", name)
			}
		}
	}
	return values, nil
}
