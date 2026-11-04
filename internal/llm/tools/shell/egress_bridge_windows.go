package shell

// EgressBridgeCommand exists for the shared sandbox code; Windows has no sandbox.
const EgressBridgeCommand = ""

func IsEgressBridge([]string) bool { return false }

func RunEgressBridge([]string) {}
