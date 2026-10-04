package aws

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/unicrons/tagctl/internal/types"
)

// forEachConcurrently runs fn over items with at most maxConcurrentAPICalls in
// flight and returns everything fn produced. Order is not preserved. Once ctx
// is cancelled no further item is dispatched; the calls in flight finish.
func forEachConcurrently[T any](ctx context.Context, items []T, fn func(T) []types.Resource) []types.Resource {
	if len(items) == 0 {
		return nil
	}

	slots := make(chan struct{}, maxConcurrentAPICalls)
	resources := make([]types.Resource, 0, len(items))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, item := range items {
		if !acquireSlot(ctx, slots) {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			batch := fn(item)
			mu.Lock()
			resources = append(resources, batch...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return resources
}

// one wraps a single resource for forEachConcurrently.
func one(r types.Resource) []types.Resource { return []types.Resource{r} }

// regionalClient returns the client of type T for region, creating and caching
// it in p.clients on first use.
func regionalClient[T, O any](p *Provider, region string, newClient func(aws.Config, ...func(*O)) *T) *T {
	key := fmt.Sprintf("%T/%s", (*T)(nil), region)
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.clients[key]; ok {
		return client.(*T)
	}
	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := newClient(regionalCfg)
	p.clients[key] = client
	return client
}

// nameFromARN returns the last path or colon segment of an ARN.
func nameFromARN(arn string) string {
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == '/' || arn[i] == ':' {
			return arn[i+1:]
		}
	}
	return arn
}
