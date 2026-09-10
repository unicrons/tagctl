package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

type mockKMSClient struct {
	keys       map[string]*kmstypes.KeyMetadata
	tags       map[string][]kmstypes.Tag
	tagsErrFor map[string]bool
	listErr    error
}

func (m *mockKMSClient) ListKeys(ctx context.Context, params *kms.ListKeysInput, optFns ...func(*kms.Options)) (*kms.ListKeysOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := &kms.ListKeysOutput{}
	for id := range m.keys {
		out.Keys = append(out.Keys, kmstypes.KeyListEntry{KeyId: aws.String(id)})
	}
	return out, nil
}

func (m *mockKMSClient) DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	return &kms.DescribeKeyOutput{KeyMetadata: m.keys[aws.ToString(params.KeyId)]}, nil
}

func (m *mockKMSClient) ListResourceTags(ctx context.Context, params *kms.ListResourceTagsInput, optFns ...func(*kms.Options)) (*kms.ListResourceTagsOutput, error) {
	id := aws.ToString(params.KeyId)
	if m.tagsErrFor[id] {
		return nil, errors.New("access denied")
	}
	return &kms.ListResourceTagsOutput{Tags: m.tags[id]}, nil
}

func (m *mockKMSClient) TagResource(ctx context.Context, params *kms.TagResourceInput, optFns ...func(*kms.Options)) (*kms.TagResourceOutput, error) {
	return &kms.TagResourceOutput{}, nil
}

func keyMeta(id string, manager kmstypes.KeyManagerType, description string) *kmstypes.KeyMetadata {
	return &kmstypes.KeyMetadata{
		KeyId:       aws.String(id),
		Arn:         aws.String("arn:aws:kms:us-east-1:123456789012:key/" + id),
		KeyManager:  manager,
		Description: aws.String(description),
	}
}

func TestListKMSKeys_OnlyCustomerManaged(t *testing.T) {
	mock := &mockKMSClient{
		keys: map[string]*kmstypes.KeyMetadata{
			"cmk-1": keyMeta("cmk-1", kmstypes.KeyManagerTypeCustomer, "app secrets"),
			"cmk-2": keyMeta("cmk-2", kmstypes.KeyManagerTypeCustomer, ""),
			"aws-1": keyMeta("aws-1", kmstypes.KeyManagerTypeAws, "Default key for EBS"),
		},
		tags: map[string][]kmstypes.Tag{
			"cmk-1": {{TagKey: aws.String("environment"), TagValue: aws.String(envProd)}},
		},
	}

	resources, err := testProvider().listKMSKeysFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d keys, want 2 (AWS-managed key excluded)", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_kms_key" || r.ARN == "" {
			t.Errorf("unexpected resource: %+v", r)
		}
		switch r.ID {
		case "cmk-1":
			if r.Name != "app secrets" || r.Tags["environment"] != envProd {
				t.Errorf("cmk-1 = %+v", r)
			}
		case "cmk-2":
			if r.Name != "cmk-2" {
				t.Errorf("key without description should use its ID as name, got %q", r.Name)
			}
		default:
			t.Errorf("unexpected key %q", r.ID)
		}
	}
}

func TestListKMSKeys_UnreadableKeyIsSkipped(t *testing.T) {
	mock := &mockKMSClient{
		keys: map[string]*kmstypes.KeyMetadata{
			"ok":     keyMeta("ok", kmstypes.KeyManagerTypeCustomer, ""),
			"denied": keyMeta("denied", kmstypes.KeyManagerTypeCustomer, ""),
		},
		tagsErrFor: map[string]bool{"denied": true},
	}

	resources, err := testProvider().listKMSKeysFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable key", resources)
	}
}

func TestListKMSKeys_ListError(t *testing.T) {
	mock := &mockKMSClient{listErr: errors.New("boom")}
	if _, err := testProvider().listKMSKeysFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}
