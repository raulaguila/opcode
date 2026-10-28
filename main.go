package main

import (
	"github.com/raulaguila/opcode/cmd"
	"github.com/raulaguila/opcode/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
