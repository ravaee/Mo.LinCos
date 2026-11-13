// Command lincos is both halves of LinCos:
//
//	lincos agent          on the Linux server: samples it and serves the UI on 127.0.0.1
//	lincos open <host>    on your Mac: opens an SSH tunnel to <host> and the UI in your browser
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/ravaee/Mo.LinCos/internal/agent"
	"github.com/ravaee/Mo.LinCos/internal/client"
	"github.com/ravaee/Mo.LinCos/ui"
)

// version is set at build time with -ldflags "-X main.version=..."
var version = "dev"

const defaultPort = 7070

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "agent":
		err = runAgent(ctx, os.Args[2:])
	case "open":
		err = runOpen(ctx, os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("lincos", version)
	case "help", "--help", "-h":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("lincos: %v", err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `LinCos %s - a small monitor for Linux servers

  lincos agent [--listen 127.0.0.1:%d] [--interval 2s]
      Run on the server (normally as the lincos systemd service).

  lincos open <ssh-host> [--port %d]
      Run on your Mac: tunnels to the agent over SSH and opens it in your browser.
      <ssh-host> is anything ssh accepts, such as an alias from ~/.ssh/config.

  lincos version
`, version, defaultPort, defaultPort)
}

func runAgent(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("agent", flag.ExitOnError)
	listen := fl.String("listen", fmt.Sprintf("127.0.0.1:%d", defaultPort), "address to serve on; must be a loopback address")
	interval := fl.Duration("interval", 2*time.Second, "time between snapshots while the page is open")
	fl.Parse(args)

	// The agent can see every process and container. Serving that to the network
	// would need a login of its own; SSH already is one
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	if !agent.IsLocalHost(host) {
		return fmt.Errorf("--listen %s is not a loopback address; reach the agent through SSH instead", *listen)
	}
	if *interval < 500*time.Millisecond {
		return errors.New("--interval must be at least 500ms")
	}
	// A soft cap: the Go runtime collects harder before going past it
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(32 << 20)
	}

	a := agent.New(agent.NewSampler(agent.DefaultPaths(), version), *interval, ui.Files)
	go a.Run(ctx)

	srv := &http.Server{Addr: *listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("lincos %s serving on http://%s (open it with: lincos open <this server>)", version, *listen)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runOpen(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("open", flag.ExitOnError)
	port := fl.Int("port", defaultPort, "the agent's port on the server")
	noBrowser := fl.Bool("no-browser", false, "print the address instead of opening the browser")
	// Allow the host before or after the flags
	var host string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		host, args = args[0], args[1:]
	}
	fl.Parse(args)
	if host == "" && fl.NArg() > 0 {
		host = fl.Arg(0)
	}
	if host == "" {
		return errors.New("which server? usage: lincos open <ssh-host>")
	}
	return client.Open(ctx, host, *port, !*noBrowser)
}
