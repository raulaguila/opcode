package lsp

import "sync"

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
