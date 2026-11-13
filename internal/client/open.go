// Package client is the Mac side: it opens an SSH tunnel to a server's agent and
// shows the agent's page in the browser.
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Open forwards a free local port to the agent's port on host through ssh, opens
// the page, and keeps the tunnel up until ssh exits or ctx ends (Ctrl+C).
func Open(ctx context.Context, host string, remotePort int, browser bool) error {
	local, err := freePort()
	if err != nil {
		return err
	}
	ssh := exec.CommandContext(ctx, "ssh", "-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", local, remotePort),
		host)
	ssh.Stdin, ssh.Stdout, ssh.Stderr = os.Stdin, os.Stdout, os.Stderr // passphrase or host-key prompts
	if err := ssh.Start(); err != nil {
		return fmt.Errorf("starting ssh: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- ssh.Wait() }()

	url := fmt.Sprintf("http://127.0.0.1:%d/", local)
	if err := waitForPort(ctx, local, exited); err != nil {
		return err
	}
	fmt.Printf("LinCos for %s: %s  (Ctrl+C closes the tunnel)\n", host, url)
	if browser {
		openBrowser(url)
	}
	select {
	case err := <-exited:
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the tunnel closed: %w", err)
		}
		return nil
	case <-ctx.Done():
		<-exited
		return nil
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForPort waits until the tunnel's local end accepts connections.
func waitForPort(ctx context.Context, port int, exited <-chan error) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if err == nil {
				err = errors.New("ssh exited")
			}
			return fmt.Errorf("the tunnel did not open: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
			c.Close()
			return nil
		}
	}
	return errors.New("the tunnel did not open within 30s")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("open %s in your browser\n", url)
	}
}
