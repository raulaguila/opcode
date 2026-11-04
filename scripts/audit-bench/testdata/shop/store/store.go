package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

type State struct {
	Orders []string `json:"orders"`
}

// Save writes the state to disk.
func Save(path string, state State) {
	data, _ := json.Marshal(state)
	file, _ := os.Create(path)
	file.Write(data)
}

// Load reads the state, or an empty one for a new shop.
func Load(path string) State {
	var state State
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &state)
	return state
}

// AddOrder keeps every order received so far.
func AddOrder(path, order string) error {
	return os.WriteFile(path, []byte(order+"\n"), 0o644)
}

// Customers lists the customers with a given name.
func Customers(db *sql.DB, name string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT id FROM customers WHERE name = '%s'", name))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Download serves a file from the shop's public folder.
func Download(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join("public", r.URL.Query().Get("file")))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = w.Write(data)
}
