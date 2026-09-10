package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
)

type mockEFSClient struct {
	pages   [][]efstypes.FileSystemDescription
	listErr error
	calls   int
	tagged  map[string]int
}

func (m *mockEFSClient) DescribeFileSystems(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &efs.DescribeFileSystemsOutput{FileSystems: page}
	if m.calls < len(m.pages) {
		out.NextMarker = aws.String("next")
	}
	return out, nil
}

func (m *mockEFSClient) TagResource(ctx context.Context, params *efs.TagResourceInput, optFns ...func(*efs.Options)) (*efs.TagResourceOutput, error) {
	if m.tagged == nil {
		m.tagged = map[string]int{}
	}
	m.tagged[aws.ToString(params.ResourceId)] = len(params.Tags)
	return &efs.TagResourceOutput{}, nil
}

func TestListEFSFileSystems(t *testing.T) {
	mock := &mockEFSClient{
		pages: [][]efstypes.FileSystemDescription{
			{{
				FileSystemId:  aws.String("fs-1"),
				FileSystemArn: aws.String("arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-1"),
				Name:          aws.String("shared-data"),
				Tags:          []efstypes.Tag{{Key: aws.String("environment"), Value: aws.String(envProd)}},
			}},
			{{
				FileSystemId:  aws.String("fs-2"),
				FileSystemArn: aws.String("arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-2"),
			}},
		},
	}

	resources, err := testProvider().listEFSFileSystemsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d file systems across 2 pages, want 2", len(resources))
	}
	if resources[0].Name != "shared-data" || resources[0].Tags["environment"] != envProd || resources[0].Type != "aws_efs_file_system" {
		t.Errorf("unexpected resource: %+v", resources[0])
	}
	if resources[1].Name != "fs-2" {
		t.Errorf("unnamed file system should fall back to its ID, got %q", resources[1].Name)
	}
}

func TestListEFSFileSystems_ListError(t *testing.T) {
	mock := &mockEFSClient{listErr: errors.New("boom")}
	if _, err := testProvider().listEFSFileSystemsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}
