//go:build !windows

package shell

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
)

// EgressBridgeCommand and egressBridgeServe are argv[1] values main hands to
// RunEgressBridge; the sandbox has no other way in.
const (
	EgressBridgeCommand = "__egress-bridge"
	egressBridgeServe   = "__egress-bridge-serve"
)

// IsEgressBridge says whether this process was started as the sandbox's bridge.
func IsEgressBridge(args []string) bool {
	return len(args) > 1 && (args[1] == EgressBridgeCommand || args[1] == egressBridgeServe)
}

// RunEgressBridge never returns. The listener is bound before the command
// starts, so its first request cannot lose the race, and the command replaces
// this process, keeping its pid, session and terminal. A second process serves
// the listener and dies with the sandbox.
func RunEgressBridge(args []string) {
	if args[1] == egressBridgeServe {
		serveEgressBridge(args[2])
	}
	if len(args) < 5 {
		egressBridgeFailed(fmt.Errorf("bridge needs a socket, certificates and a command"))
	}
	socket, certificates, command := args[2], args[3], args[4]
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		egressBridgeFailed(err)
	}
	inherited, err := listener.File()
	if err != nil {
		egressBridgeFailed(err)
	}
	server := exec.Command(args[0], egressBridgeServe, socket)
	server.ExtraFiles = []*os.File{inherited}
	server.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := server.Start(); err != nil {
		egressBridgeFailed(err)
	}
	binary, err := exec.LookPath(command)
	if err != nil {
		egressBridgeFailed(err)
	}
	proxy := fmt.Sprintf("http://%s", listener.Addr())
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		_ = os.Setenv(name, proxy)
	}
	for _, name := range []string{"NO_PROXY", "no_proxy"} {
		_ = os.Setenv(name, "localhost,127.0.0.1,::1")
	}
	if certificates != "" {
		for _, name := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "PIP_CERT", "CARGO_HTTP_CAINFO", "GIT_SSL_CAINFO", "CURL_CA_BUNDLE"} {
			if _, set := os.LookupEnv(name); !set {
				_ = os.Setenv(name, certificates)
			}
		}
	}
	egressBridgeFailed(syscall.Exec(binary, append([]string{command}, args[5:]...), os.Environ()))
}

func serveEgressBridge(socket string) {
	listener, err := net.FileListener(os.NewFile(3, "egress"))
	if err != nil {
		os.Exit(1)
	}
	for {
		client, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}
		go func() {
			defer client.Close()
			proxy, err := net.Dial("unix", socket)
			if err != nil {
				return
			}
			defer proxy.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(proxy, client); done <- struct{}{} }()
			go func() { _, _ = io.Copy(client, proxy); done <- struct{}{} }()
			<-done
		}()
	}
}

func egressBridgeFailed(err error) {
	fmt.Fprintln(os.Stderr, "opcode sandbox:", err)
	os.Exit(126)
}
