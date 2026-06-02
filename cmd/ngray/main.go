package main

import (
	"fmt"
	"os"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/version"
)

const usage = `ng-ray — unified anti-DPI proxy toolkit

Usage:
  ngray                  launch the interactive TUI
  ngray server setup     provision/repair the server from config.yml
  ngray server status    show core versions, services and listening ports
  ngray client links     print share links, QR codes and the subscribe URL
  ngray sni test [host…] test SNIs for REALITY fitness and latency
  ngray scan             scan CDN edges for reachable IPs
  ngray firewall apply   apply the safe, idempotent firewall ruleset
  ngray update [--self]  update cores (and optionally ngray itself)
  ngray version          print build version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		// TUI lands here in a later phase; for now show usage.
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println(version.String())
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "config":
		return cmdConfig(args[1:])
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// cmdConfig validates and prints the resolved configuration.
func cmdConfig(args []string) error {
	c, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "init" {
		path := config.FileName
		if err := config.Save(path, c); err != nil {
			return err
		}
		fmt.Println("wrote", path)
		return nil
	}
	fmt.Printf("config valid: nginx:%d  subscription:%v  hysteria2:%v  fptn:%v\n",
		c.Nginx.Port, c.Subscription.Enabled, c.Cores.Hysteria2.Enabled, c.Cores.Fptn.Enabled)
	return nil
}
