package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	ectypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
)

type mockElastiCacheClient struct {
	pages      [][]ectypes.CacheCluster
	tags       map[string][]ectypes.Tag
	tagsErrFor map[string]bool
	listErr    error
	calls      int
}

func (m *mockElastiCacheClient) DescribeCacheClusters(ctx context.Context, params *elasticache.DescribeCacheClustersInput, optFns ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &elasticache.DescribeCacheClustersOutput{CacheClusters: page}
	if m.calls < len(m.pages) {
		out.Marker = aws.String("next")
	}
	return out, nil
}

func (m *mockElastiCacheClient) ListTagsForResource(ctx context.Context, params *elasticache.ListTagsForResourceInput, optFns ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error) {
	arn := aws.ToString(params.ResourceName)
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	return &elasticache.ListTagsForResourceOutput{TagList: m.tags[arn]}, nil
}

func (m *mockElastiCacheClient) AddTagsToResource(ctx context.Context, params *elasticache.AddTagsToResourceInput, optFns ...func(*elasticache.Options)) (*elasticache.AddTagsToResourceOutput, error) {
	return &elasticache.AddTagsToResourceOutput{}, nil
}

func cacheCluster(id string) ectypes.CacheCluster {
	return ectypes.CacheCluster{
		CacheClusterId: aws.String(id),
		ARN:            aws.String("arn:aws:elasticache:us-east-1:123456789012:cluster:" + id),
	}
}

func TestListElastiCacheClusters(t *testing.T) {
	sessions := "arn:aws:elasticache:us-east-1:123456789012:cluster:sessions"
	mock := &mockElastiCacheClient{
		pages: [][]ectypes.CacheCluster{{cacheCluster("sessions")}, {cacheCluster("queue")}},
		tags:  map[string][]ectypes.Tag{sessions: {{Key: aws.String("environment"), Value: aws.String(envProd)}}},
	}

	resources, err := testProvider().listElastiCacheClustersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d clusters across 2 pages, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_elasticache_cluster" || r.ARN == "" {
			t.Errorf("unexpected resource: %+v", r)
		}
		if r.ID == "sessions" && r.Tags["environment"] != envProd {
			t.Errorf("tags = %v", r.Tags)
		}
	}
}

func TestListElastiCacheClusters_UnreadableClusterIsSkipped(t *testing.T) {
	denied := "arn:aws:elasticache:us-east-1:123456789012:cluster:denied"
	mock := &mockElastiCacheClient{
		pages:      [][]ectypes.CacheCluster{{cacheCluster("denied"), cacheCluster("ok")}},
		tagsErrFor: map[string]bool{denied: true},
	}

	resources, err := testProvider().listElastiCacheClustersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable cluster", resources)
	}
}

func TestListElastiCacheClusters_ListError(t *testing.T) {
	mock := &mockElastiCacheClient{listErr: errors.New("boom")}
	if _, err := testProvider().listElastiCacheClustersFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}
