package clients

import (
	"fmt"
	"sync"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
)

type IdTakenError struct {
}

func (e IdTakenError) Error() string {
	return "IdTakenError"
}

type TooManyClientsError struct {
	Count uint
}

func (e TooManyClientsError) Error() string {
	return fmt.Sprintf("TooManyClientsError{ Count: %d }", e.Count)
}

type InvalidClientError struct {
}

func (e InvalidClientError) Error() string {
	return "InvalidClientError"
}

// ClientManager is responsible for handling all connected clients.
type ClientManager struct {
	MaxClients uint               // Maximum number of clients
	clients    map[string]*Client // A map of active clients
	mu         sync.RWMutex       // Mutex for concurrency control
}

func NewClientManager(maxClient uint) *ClientManager {
	return &ClientManager{
		MaxClients: maxClient,
		clients:    make(map[string]*Client),
		mu:         sync.RWMutex{},
	}
}

func (cm *ClientManager) AddClient(c *Client) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if the client already exists
	existingClient, exists := cm.clients[c.id]
	if exists {
		// If the client exists and token is invalid, return an ID-TAKEN error.
		if existingClient.token != c.token {
			return &IdTakenError{}
		}
		// If valid, replace the client with the new one
		close(existingClient.closeSig)
		c.lastActive = time.Now()
		c.closeSig = make(chan struct{})
		c.msgChan = existingClient.msgChan
		cm.clients[c.id] = c
	} else {
		// If it doesn't exist, add the new client
		if cm.MaxClients > 0 && uint(len(cm.clients)) >= cm.MaxClients {
			return &TooManyClientsError{Count: cm.MaxClients}
		}
		c.lastActive = time.Now()
		c.closeSig = make(chan struct{})
		c.msgChan = make(chan OneOrMoreMsg)
		cm.clients[c.id] = c
	}
	return nil
}

func (cm *ClientManager) RemoveClient(c *Client) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Remove the client from the map
	if _, exists := cm.clients[c.id]; exists {
		delete(cm.clients, c.id)
		close(c.closeSig)
		close(c.msgChan)
	}
}

func (cm *ClientManager) SendToClient(id string, msg *protocol.Message) (bool, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.clients[id]
	if !exists {
		return false, nil
	}
	client.msgChan <- OneOrMoreMsg{One: msg, More: nil}
	return true, nil
}

func (cm *ClientManager) SendToClientBatch(id string, msg utils.Iterable[*protocol.Message]) (bool, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.clients[id]
	if !exists {
		return false, nil
	}
	client.msgChan <- OneOrMoreMsg{One: nil, More: msg}
	return true, nil
}

func (cm *ClientManager) SendToMultiClientBatch(v map[string]utils.Iterable[*protocol.Message]) (map[string]bool, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	results := make(map[string]bool)
	for id, msgs := range v {
		client, exists := cm.clients[id]
		if !exists {
			results[id] = false
			continue
		}
		client.msgChan <- OneOrMoreMsg{One: nil, More: msgs}
		results[id] = true
	}
	return results, nil
}

func (cm *ClientManager) ListClients() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clients := make([]string, 0, len(cm.clients))
	for k := range cm.clients {
		clients = append(clients, k)
	}
	return clients
}

func (cm *ClientManager) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Close all clients
	for _, c := range cm.clients {
		close(c.closeSig)
		close(c.msgChan)
	}
	cm.clients = make(map[string]*Client)
}

var (
	_ error = (*IdTakenError)(nil)
	_ error = (*TooManyClientsError)(nil)
	_ error = (*InvalidClientError)(nil)
)
