package config

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakeSSM struct {
	values map[string]string
	calls  int
}

func (f *fakeSSM) GetParameters(_ context.Context, in *ssm.GetParametersInput, _ ...func(*ssm.Options)) (*ssm.GetParametersOutput, error) {
	f.calls++
	if len(in.Names) > maxPerCall || !aws.ToBool(in.WithDecryption) {
		return nil, fmt.Errorf("bad request")
	}
	out := &ssm.GetParametersOutput{}
	for _, name := range in.Names {
		if v, ok := f.values[name]; ok {
			out.Parameters = append(out.Parameters, types.Parameter{Name: aws.String(name), Value: aws.String(v)})
		} else {
			out.InvalidParameters = append(out.InvalidParameters, name)
		}
	}
	return out, nil
}

func TestEnv(t *testing.T) {
	env := map[string]string{"A": "1", "B": "2"}
	got, err := Env(func(k string) string { return env[k] }, "A", "B")
	if err != nil || got["A"] != "1" || got["B"] != "2" {
		t.Fatalf("Env = %v, %v", got, err)
	}
	if _, err := Env(func(k string) string { return env[k] }, "A", "C", "D"); err == nil || !strings.Contains(err.Error(), "C, D") {
		t.Fatalf("missing: %v", err)
	}
}

func TestLoad(t *testing.T) {
	f := &fakeSSM{values: map[string]string{}}
	var names []string
	for i := range 12 {
		name := fmt.Sprintf("/p/%d", i)
		f.values[name] = "secret-" + name
		names = append(names, name)
	}
	got, err := Load(context.Background(), f, names...)
	if err != nil || len(got) != 12 || got["/p/11"] != "secret-/p/11" || f.calls != 2 {
		t.Fatalf("Load = %d values, %v, %d calls", len(got), err, f.calls)
	}

	_, err = Load(context.Background(), f, "/p/1", "/missing")
	if err == nil || !strings.Contains(err.Error(), "/missing") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("missing parameter: %v", err)
	}

	f.values["/empty"] = ""
	if _, err := Load(context.Background(), f, "/empty"); err == nil {
		t.Fatal("accepted an empty parameter")
	}
}

func TestInviteOnly(t *testing.T) {
	for value, want := range map[string]bool{"": true, "true": true, "yes": true, "false": false, " FALSE ": false} {
		if InviteOnly(value) != want {
			t.Errorf("InviteOnly(%q) = %v", value, !want)
		}
	}
}

func TestTelegramIDs(t *testing.T) {
	got, err := TelegramIDs(" 12, 34 ,,56")
	if err != nil || len(got) != 3 || got[0] != 12 || got[2] != 56 {
		t.Fatalf("TelegramIDs = %v, %v", got, err)
	}
	if got, err := TelegramIDs(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"12,abc", "-5", "0", "1.5"} {
		if _, err := TelegramIDs(bad); err == nil {
			t.Errorf("TelegramIDs(%q) accepted", bad)
		}
	}
}
