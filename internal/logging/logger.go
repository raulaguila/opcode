package logging

import (
	"fmt"
	"log/slog"
	"os"

	// "path/filepath"

	"runtime"
	"runtime/debug"
	"time"
)

func getCaller() string {
	var caller string
	if _, file, line, ok := runtime.Caller(2); ok {
		// caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		caller = fmt.Sprintf("%s:%d", file, line)
	} else {
		caller = "unknown"
	}
	return caller
}
func Info(msg string, args ...any) {
	source := getCaller()
	slog.Info(msg, append([]any{"source", source}, args...)...)
}

func Debug(msg string, args ...any) {
	// slog.Debug(msg, args...)
	source := getCaller()
	slog.Debug(msg, append([]any{"source", source}, args...)...)
}

func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

func InfoPersist(msg string, args ...any) {
	args = append(args, persistKeyArg, true)
	slog.Info(msg, args...)
}

func ErrorPersist(msg string, args ...any) {
	args = append(args, persistKeyArg, true)
	slog.Error(msg, args...)
}

// RecoverPanic is a common function to handle panics gracefully.
// It logs the error, creates a panic log file with stack trace,
// and executes an optional cleanup function before returning.
func RecoverPanic(name string, cleanup func()) {
	if r := recover(); r != nil {
		// Log the panic
		ErrorPersist(fmt.Sprintf("Panic in %s: %v", name, r))

		// Create a timestamped panic log file
		timestamp := time.Now().Format("20060102-150405")
		filename := fmt.Sprintf("opcode-panic-%s-%s.log", name, timestamp)

		file, err := os.Create(filename)
		if err != nil {
			ErrorPersist(fmt.Sprintf("Failed to create panic log: %v", err))
		} else {
			defer file.Close()

			// Write panic information and stack trace
			fmt.Fprintf(file, "Panic in %s: %v\n\n", name, r)
			fmt.Fprintf(file, "Time: %s\n\n", time.Now().Format(time.RFC3339))
			fmt.Fprintf(file, "Stack Trace:\n%s\n", debug.Stack())

			InfoPersist(fmt.Sprintf("Panic details written to %s", filename))
		}

		// Execute cleanup function if provided
		if cleanup != nil {
			cleanup()
		}
	}
}
