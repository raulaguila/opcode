package main

import (
	"os"

	"github.com/raulaguila/opcode/cmd"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/logging"
)

func main() {
	if shell.IsEgressBridge(os.Args) {
		shell.RunEgressBridge(os.Args)
		return
	}
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
