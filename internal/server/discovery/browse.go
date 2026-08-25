package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const defaultBrowseTimeout = 2 * time.Second

// Browse looks up `_speechkit._tcp` on the LAN. It never returns
// credentials. An empty result is not an error: nothing announced, or
// discovery is off on the server.
func Browse(ctx context.Context) []Record {
	timeout := defaultBrowseTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	entries := make(chan *mdns.ServiceEntry, 16)
	var (
		mu      sync.Mutex
		found   []Record
		seen    = map[string]struct{}{}
		collect sync.WaitGroup
	)
	collect.Add(1)
	go func() {
		defer collect.Done()
		for e := range entries {
			rec, ok := recordFromEntry(e)
			if !ok {
				continue
			}
			mu.Lock()
			if _, dup := seen[rec.URL]; !dup {
				seen[rec.URL] = struct{}{}
				found = append(found, rec)
			}
			mu.Unlock()
		}
	}()
	_ = mdns.Query(&mdns.QueryParam{
		Service:     ServiceType,
		Domain:      "local",
		Timeout:     timeout,
		Entries:     entries,
		DisableIPv6: true,
	})
	close(entries)
	collect.Wait()
	if found == nil {
		return []Record{}
	}
	return found
}
