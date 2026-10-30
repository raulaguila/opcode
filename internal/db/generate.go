package db

// Keep sqlc's query implementations, then normalize only its repeated statement
// lifecycle. The normalizer fails if the generated input shape is unsupported.
//go:generate sqlc generate -f ../../sqlc.yaml
//go:generate go run ../../scripts/db-prepared -file db.go
