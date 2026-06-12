package morpheus

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

const (
	// discoveryListPageSize is items per list request during discovery.
	discoveryListPageSize = 100
	// discoveryListPageParallelism is concurrent list-page fetches within one category.
	discoveryListPageParallelism = 4
	// discoveryEnrichParallelism is concurrent per-item enrich calls within one category.
	discoveryEnrichParallelism = 10
	// discoveryGlobalEnrichParallelism caps enrich calls across all categories.
	discoveryGlobalEnrichParallelism = 32
)

// discoveryConcurrency limits in-flight discovery HTTP calls across categories.
type discoveryConcurrency struct {
	enrichSem chan struct{}
}

func newDiscoveryConcurrency() *discoveryConcurrency {
	return &discoveryConcurrency{
		enrichSem: make(chan struct{}, discoveryGlobalEnrichParallelism),
	}
}

func parseListPage(body []byte, dataKey string) (items []json.RawMessage, total int, err error) {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, 0, err
	}
	total = extractMetaTotal(wrapper)
	raw, ok := wrapper[dataKey]
	if !ok {
		return nil, total, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, total, err
	}
	return items, total, nil
}

func extractMetaTotal(wrapper map[string]json.RawMessage) int {
	raw, ok := wrapper["meta"]
	if !ok {
		return 0
	}
	var meta struct {
		Total int64 `json:"total"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return 0
	}
	if meta.Total <= 0 {
		return 0
	}
	return int(meta.Total)
}

// paginateDiscoveryList lists all items using parallel page fetches when beneficial.
func (c *Client) paginateDiscoveryList(basePath, dataKey string, pageSize int) ([]json.RawMessage, error) {
	if pageSize <= 0 {
		pageSize = discoveryListPageSize
	}
	firstPath := fmt.Sprintf("%s?max=%d&offset=0", basePath, pageSize)
	body, err := c.get(firstPath)
	if err != nil {
		return nil, err
	}
	firstItems, total, err := parseListPage(body, dataKey)
	if err != nil {
		return nil, err
	}
	if len(firstItems) == 0 {
		return nil, nil
	}
	if total > len(firstItems) {
		rest, err := c.fetchDiscoveryListPagesParallel(basePath, dataKey, pageSize, total, 1)
		if err != nil {
			return nil, err
		}
		out := make([]json.RawMessage, 0, total)
		out = append(out, firstItems...)
		out = append(out, rest...)
		return out, nil
	}
	if len(firstItems) < pageSize {
		return firstItems, nil
	}
	rest, err := c.fetchDiscoveryListPagesParallelUnknownTotal(basePath, dataKey, pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(firstItems)+len(rest))
	out = append(out, firstItems...)
	out = append(out, rest...)
	return out, nil
}

func (c *Client) fetchDiscoveryListPagesParallel(basePath, dataKey string, pageSize, total, startPage int) ([]json.RawMessage, error) {
	numPages := (total + pageSize - 1) / pageSize
	if startPage >= numPages {
		return nil, nil
	}
	results := make([]pageResult, 0, numPages-startPage)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, discoveryListPageParallelism)

	for page := startPage; page < numPages; page++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			offset := page * pageSize
			path := fmt.Sprintf("%s?max=%d&offset=%d", basePath, pageSize, offset)
			body, err := c.get(path)
			if err != nil {
				mu.Lock()
				results = append(results, pageResult{page: page, err: err})
				mu.Unlock()
				return
			}
			items, _, err := parseListPage(body, dataKey)
			mu.Lock()
			results = append(results, pageResult{page: page, items: items, err: err})
			mu.Unlock()
		}(page)
	}
	wg.Wait()

	if err := firstPageResultError(results); err != nil {
		return nil, err
	}
	sortPageResults(results)
	var all []json.RawMessage
	for _, pr := range results {
		all = append(all, pr.items...)
	}
	return all, nil
}

func (c *Client) fetchDiscoveryListPagesParallelUnknownTotal(basePath, dataKey string, pageSize, startOffset int) ([]json.RawMessage, error) {
	var all []json.RawMessage
	offset := startOffset
	for {
		type pageResult struct {
			idx   int
			items []json.RawMessage
			err   error
		}
		batch := discoveryListPageParallelism
		results := make([]pageResult, batch)
		var wg sync.WaitGroup
		for i := 0; i < batch; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				off := offset + i*pageSize
				path := fmt.Sprintf("%s?max=%d&offset=%d", basePath, pageSize, off)
				body, err := c.get(path)
				if err != nil {
					results[i] = pageResult{idx: i, err: err}
					return
				}
				items, _, err := parseListPage(body, dataKey)
				results[i] = pageResult{idx: i, items: items, err: err}
			}(i)
		}
		wg.Wait()

		done := false
		for i := 0; i < batch; i++ {
			if results[i].err != nil {
				return nil, results[i].err
			}
			if len(results[i].items) == 0 {
				done = true
				break
			}
			all = append(all, results[i].items...)
			if len(results[i].items) < pageSize {
				done = true
				break
			}
		}
		if done {
			break
		}
		offset += batch * pageSize
	}
	return all, nil
}

type pageResult struct {
	page  int
	items []json.RawMessage
	err   error
}

func firstPageResultError(results []pageResult) error {
	for _, pr := range results {
		if pr.err != nil {
			return pr.err
		}
	}
	return nil
}

func sortPageResults(results []pageResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].page < results[j].page
	})
}
