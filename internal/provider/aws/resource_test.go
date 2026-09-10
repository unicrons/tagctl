package aws

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestResource_NilTagsBecomeEmpty(t *testing.T) {
	r := testProvider().resource(defaultRegion, "aws_x", "id", "name", "arn:x", nil, nil)
	if r.Tags == nil || len(r.Tags) != 0 || r.Account != "123456789012" || r.Provider != "aws" {
		t.Errorf("got %+v", r)
	}
}

func TestTagsToMap(t *testing.T) {
	type kv struct{ K, V *string }
	got := tagsToMap([]kv{{aws.String("a"), aws.String("1")}, {nil, aws.String("2")}, {aws.String("c"), nil}},
		func(t kv) *string { return t.K }, func(t kv) *string { return t.V })
	if len(got) != 1 || got["a"] != "1" {
		t.Errorf("got %v", got)
	}
}

func TestNotSubscribed(t *testing.T) {
	if !notSubscribed(apiError{"UninitializedAccountException"}) || !notSubscribed(apiError{"ResourceNotFoundException"}) {
		t.Error("subscription errors must be recognised")
	}
	if notSubscribed(apiError{"AccessDeniedException"}) || notSubscribed(errors.New("dial tcp: timeout")) {
		t.Error("real failures must not be swallowed")
	}
}

func TestDBClusterType(t *testing.T) {
	for engine, want := range map[string]string{"docdb": "aws_docdb_cluster", "neptune": "aws_neptune_cluster", "aurora-postgresql": "aws_rds_cluster", "": "aws_rds_cluster"} {
		if got := dbClusterType(engine); got != want {
			t.Errorf("%q: got %q, want %q", engine, got, want)
		}
	}
}
