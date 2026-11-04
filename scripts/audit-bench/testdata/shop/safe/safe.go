package safe

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

type Cache struct {
	mu     sync.Mutex
	values map[string]string
}

// Get reads a key; reading a missing key or a nil map gives the zero value.
func (c *Cache) Get(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[key]
}

// Name uses a checked type assertion, which does not panic.
func Name(value any) string {
	name, ok := value.(string)
	if !ok {
		return ""
	}
	return name
}

// Mean guards the empty slice.
func Mean(values []int) int {
	if len(values) == 0 {
		return 0
	}
	total := 0
	for _, value := range values {
		total += value
	}
	return total / len(values)
}

// Tail guards the empty slice.
func Tail(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// Parts relies on functions of a recent Go.
func Parts(path string) []string {
	var parts []string
	for part := range strings.SplitSeq(path, "/") {
		parts = append(parts, part)
	}
	return parts[:min(len(parts), 8)]
}

// Routes uses path patterns, supported since Go 1.22.
func Routes(mux *http.ServeMux, handler http.HandlerFunc) {
	mux.HandleFunc("GET /orders/{id}", handler)
}

// Write closes the file and reports every error.
func Write(path string, data []byte) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	_, err = file.Write(data)
	return err
}

// Greet copies the loop variable by language rule since Go 1.22.
func Greet(names []string, out chan<- string) {
	var group sync.WaitGroup
	for _, name := range names {
		group.Add(1)
		go func() {
			defer group.Done()
			out <- fmt.Sprintf("hello %s", name)
		}()
	}
	group.Wait()
}
