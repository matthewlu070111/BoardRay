package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/matthewlu070111/BoardRay/internal/agent"
)

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if err := run(os.Args[1:]); err != nil {
		log.Printf("boardray-agent: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Printf("boardray-agent %s\n", version)
		return nil
	}
	if len(args) == 1 && args[0] == "keygen" {
		value, err := agent.NewRealitySecrets()
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%s\t%s\n", value.PrivateKey, value.PublicKey, value.ShortID)
		return nil
	}
	flags := flag.NewFlagSet("boardray-agent", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/boardray/config.json", "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: boardray-agent [-config PATH] | version | keygen")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agent.Run(ctx, *configPath, version)
}
