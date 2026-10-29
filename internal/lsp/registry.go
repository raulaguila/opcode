package lsp

import (
	"context"
	"fmt"
	"sync"
)

var clientsMu sync.RWMutex

func SnapshotClients(clients map[string]*Client) map[string]*Client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	result := make(map[string]*Client, len(clients))
	for key, client := range clients {
		result[key] = client
	}
	return result
}
func SetClient(clients map[string]*Client, key string, client *Client) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients[key] = client
}
func DeleteClient(clients map[string]*Client, key string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	delete(clients, key)
}

var lazyStarters = map[string]func(context.Context, string) error{}

func RegisterLazyClients(clients map[string]*Client, start func(context.Context, string) error) func() {
	key := fmt.Sprintf("%p", clients)
	clientsMu.Lock()
	lazyStarters[key] = start
	clientsMu.Unlock()
	return func() { clientsMu.Lock(); delete(lazyStarters, key); clientsMu.Unlock() }
}
func EnsureClients(ctx context.Context, clients map[string]*Client, path string) error {
	if path == "" {
		return nil
	}
	key := fmt.Sprintf("%p", clients)
	clientsMu.RLock()
	start := lazyStarters[key]
	clientsMu.RUnlock()
	if start == nil {
		return nil
	}
	return start(ctx, path)
}
