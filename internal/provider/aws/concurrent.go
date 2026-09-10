package aws

import (
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/unicrons/tagctl/internal/types"
)

// forEachConcurrently runs fn over items with at most maxConcurrentAPICalls in
// flight and returns everything fn produced. Order is not preserved.
func forEachConcurrently[T any](items []T, fn func(T) []types.Resource) []types.Resource {
	if len(items) == 0 {
		return nil
	}

	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan []types.Resource, len(items))
	var wg sync.WaitGroup

	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results <- fn(item)
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	resources := make([]types.Resource, 0, len(items))
	for batch := range results {
		resources = append(resources, batch...)
	}
	return resources
}

// one wraps a single resource for forEachConcurrently.
func one(r types.Resource) []types.Resource { return []types.Resource{r} }

// cachedClient returns the client for region from cache, creating it on first use.
func cachedClient[T, O any](p *Provider, cache map[string]*T, region string, newClient func(aws.Config, ...func(*O)) *T) *T {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := cache[region]; ok {
		return client
	}

	regionalCfg := p.cfg.Copy()
	regionalCfg.Region = region
	client := newClient(regionalCfg)
	cache[region] = client
	return client
}

// regionalClient returns the client of type T for region, creating and caching
// it in p.clients on first use. Services added after the typed cache maps use
// this instead of a dedicated map and getter.
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
