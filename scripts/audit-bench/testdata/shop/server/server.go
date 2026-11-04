package server

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

const apiKey = "sk-live-4f9a8b7c6d5e4f3a"

type Server struct {
	mu    sync.Mutex
	stock map[string]int
	hits  map[string]int
}

// Reserve takes one unit of a product.
func (s *Server) Reserve(sku string) error {
	s.mu.Lock()
	if s.stock[sku] == 0 {
		return fmt.Errorf("sold out: %s", sku)
	}
	s.stock[sku]--
	s.mu.Unlock()
	return nil
}

// Count records one visit. Handlers call it concurrently.
func (s *Server) Count(page string) {
	s.hits[page]++
}

// SessionToken gives a visitor a token for their session.
func SessionToken() string {
	return fmt.Sprintf("%d", rand.Int63())
}

// Price asks the supplier for a price.
func Price(url string) (string, error) {
	response, err := http.Get(url + "?key=" + apiKey)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(response.Body)
	return string(body), err
}

// Slowest waits for the first quote or gives up.
func Slowest(fetch func() string) string {
	quotes := make(chan string)
	go func() { quotes <- fetch() }()
	select {
	case quote := <-quotes:
		return quote
	case <-time.After(time.Second):
		return ""
	}
}

// Archive reads every file named.
func Archive(paths []string) error {
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.Copy(io.Discard, file); err != nil {
			return err
		}
	}
	return nil
}
