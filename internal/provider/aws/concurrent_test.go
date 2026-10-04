package aws

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/unicrons/tagctl/internal/types"
)

func clientCacheProvider() *Provider {
	return &Provider{
		cfg:     aws.Config{Region: "us-east-1"},
		clients: map[string]any{},
	}
}

func TestForEachConcurrently_NoItemsReturnsNilWithoutCallingFn(t *testing.T) {
	got := forEachConcurrently(context.Background(), nil, func(int) []types.Resource {
		t.Error("fn called with no items")
		return nil
	})

	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestForEachConcurrently_ProcessesEveryItemOnce(t *testing.T) {
	items := make([]int, 10*maxConcurrentAPICalls)
	for i := range items {
		items[i] = i
	}

	got := forEachConcurrently(context.Background(), items, func(i int) []types.Resource {
		return one(types.Resource{ID: fmt.Sprintf("r-%d", i)})
	})

	if len(got) != len(items) {
		t.Fatalf("got %d resources, want %d", len(got), len(items))
	}
	seen := make(map[string]int, len(got))
	for _, r := range got {
		seen[r.ID]++
	}
	for i := range items {
		if id := fmt.Sprintf("r-%d", i); seen[id] != 1 {
			t.Errorf("%s produced %d times, want once", id, seen[id])
		}
	}
}

func TestForEachConcurrently_FlattensBatchesAndDropsEmptyOnes(t *testing.T) {
	got := forEachConcurrently(context.Background(), []int{0, 1, 2, 3}, func(n int) []types.Resource {
		return make([]types.Resource, n)
	})

	if len(got) != 6 {
		t.Errorf("got %d resources, want 6 (0+1+2+3)", len(got))
	}
}

func TestForEachConcurrently_NeverExceedsMaxConcurrentAPICalls(t *testing.T) {
	items := make([]int, 8*maxConcurrentAPICalls)

	var inFlight, peak atomic.Int64
	var fillOnce sync.Once
	filled := make(chan struct{})
	var timedOut atomic.Bool

	got := forEachConcurrently(context.Background(), items, func(int) []types.Resource {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		raiseTo(&peak, n)
		if n == maxConcurrentAPICalls {
			fillOnce.Do(func() { close(filled) })
		}
		// Holding every call until the pool is full proves the bound is
		// reached, not just respected.
		select {
		case <-filled:
		case <-time.After(5 * time.Second):
			timedOut.Store(true)
		}
		return one(types.Resource{})
	})

	if timedOut.Load() {
		t.Fatalf("the pool never held %d calls at once", maxConcurrentAPICalls)
	}
	if len(got) != len(items) {
		t.Errorf("got %d resources, want %d", len(got), len(items))
	}
	if p := peak.Load(); p != maxConcurrentAPICalls {
		t.Errorf("peak of %d calls in flight, want exactly %d", p, maxConcurrentAPICalls)
	}
}

func TestOne_WrapsTheResource(t *testing.T) {
	got := one(types.Resource{ID: "i-1"})

	if len(got) != 1 || got[0].ID != "i-1" {
		t.Errorf("one() = %v, want a single i-1", got)
	}
}

func TestRegionalClient_ReturnsTheCachedClientForTheSameRegion(t *testing.T) {
	p := clientCacheProvider()

	first := regionalClient(p, "eu-west-1", sqs.NewFromConfig)
	second := regionalClient(p, "eu-west-1", sqs.NewFromConfig)

	if first != second {
		t.Error("second call built a new client, want the cached one")
	}
	if got := first.Options().Region; got != "eu-west-1" {
		t.Errorf("client region = %q, want eu-west-1", got)
	}
}

func TestRegionalClient_BuildsOneClientPerRegion(t *testing.T) {
	p := clientCacheProvider()

	ireland := regionalClient(p, "eu-west-1", sqs.NewFromConfig)
	virginia := regionalClient(p, "us-east-1", sqs.NewFromConfig)

	if ireland == virginia {
		t.Fatal("both regions share one client")
	}
	if got := virginia.Options().Region; got != "us-east-1" {
		t.Errorf("client region = %q, want us-east-1", got)
	}
	if p.cfg.Region != "us-east-1" {
		t.Errorf("provider config region = %q, want it untouched", p.cfg.Region)
	}
}

func TestRegionalClient_KeepsServicesApartInTheSameRegion(t *testing.T) {
	p := clientCacheProvider()

	queues := regionalClient(p, "eu-west-1", sqs.NewFromConfig)
	topics := regionalClient(p, "eu-west-1", sns.NewFromConfig)

	if queues == nil || topics == nil {
		t.Fatalf("clients = %v, %v, want both built", queues, topics)
	}
	if len(p.clients) != 2 {
		t.Errorf("cache holds %d clients, want one per service", len(p.clients))
	}
	if again := regionalClient(p, "eu-west-1", sqs.NewFromConfig); again != queues {
		t.Error("SQS client replaced after the SNS one was cached")
	}
}

func TestRegionalClient_BuildsOnceUnderConcurrentCalls(t *testing.T) {
	p := clientCacheProvider()
	var built atomic.Int64
	newClient := func(cfg aws.Config, optFns ...func(*sqs.Options)) *sqs.Client {
		built.Add(1)
		return sqs.NewFromConfig(cfg, optFns...)
	}

	const callers = 32
	clients := make([]*sqs.Client, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			clients[i] = regionalClient(p, "eu-west-1", newClient)
		}()
	}
	close(start)
	wg.Wait()

	if n := built.Load(); n != 1 {
		t.Errorf("client built %d times, want 1", n)
	}
	for i, c := range clients {
		if c != clients[0] {
			t.Fatalf("caller %d got a different client", i)
		}
	}
}

func TestNameFromARN(t *testing.T) {
	tests := []struct {
		arn  string
		want string
	}{
		{"arn:aws:ecs:us-east-1:123456789012:cluster/prod", "prod"},
		{"arn:aws:sns:us-east-1:123456789012:alerts", "alerts"},
		{"arn:aws:ecs:us-east-1:123456789012:service/prod/api", "api"},
		{"plain-name", "plain-name"},
		{"arn:aws:s3:::", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := nameFromARN(tt.arn); got != tt.want {
			t.Errorf("nameFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
		}
	}
}
