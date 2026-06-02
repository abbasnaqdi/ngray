// Package cdnscan finds CDN edge IPs reachable from the current network,
// used to pick IPs that survive an operator's IP-based blocking (e.g. MCI).
package cdnscan

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/cdn"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/config"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/network"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/output"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/scanner"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/tui"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/verifier"
)

// Run launches the interactive CDN edge scanner.
func Run() error {
	cfg := config.Load()
	ifaces, _ := network.Interfaces()
	proxy := network.ProxyAddr()

	ctx, cancel := context.WithCancel(context.Background())
	model := tui.New(ifaces, proxy)

	var p *tea.Program
	model.OnStart = func(useProxy bool, iface network.Interface, enabled []*tui.CDNState) {
		var dialer *net.Dialer
		if useProxy {
			dialer = &net.Dialer{}
		} else {
			dialer = network.MakeDialer(iface)
		}
		runAll(ctx, cfg, p, dialer, enabled)
	}
	p = tea.NewProgram(model, tea.WithAltScreen())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
		p.Quit()
	}()

	finalM, err := p.Run()
	cancel()
	if err != nil {
		return fmt.Errorf("scanner TUI: %w", err)
	}
	if m, ok := finalM.(tui.Model); ok && len(m.VerClean) > 0 {
		fmt.Println()
		for _, provider := range cdn.Registry {
			if n, exists := m.VerClean[provider.Name]; exists {
				fmt.Printf("  %-12s  %d clean IPs  →  %s\n", provider.Name, n, provider.OutputFile)
			}
		}
	}
	return nil
}

func runAll(ctx context.Context, cfg *config.Config, p *tea.Program, dialer *net.Dialer, enabled []*tui.CDNState) {
	if len(enabled) == 0 {
		p.Send(tui.MsgAllDone{})
		return
	}
	writers := make(map[string]*output.Writer)
	for _, s := range enabled {
		w, err := output.New(s.Provider.OutputFile)
		if err != nil {
			continue
		}
		writers[s.Provider.Name] = w
	}

	var wg sync.WaitGroup
	for _, s := range enabled {
		s := s
		w, ok := writers[s.Provider.Name]
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCDN(ctx, s.Provider, cfg, p, cloneDialer(dialer), w)
			w.Close()
		}()
	}
	wg.Wait()
	p.Send(tui.MsgAllDone{})
}

func runCDN(ctx context.Context, provider *cdn.Provider, cfg *config.Config, p *tea.Program, dialer *net.Dialer, out *output.Writer) {
	name := provider.Name
	p.Send(tui.MsgPhase{CDN: name, Phase: tui.PhaseDiscovery})

	discCh, discStats, err := scanner.DiscoverWithDialer(ctx, provider.Ranges, cfg.DiscoverWorkers, cfg.DiscoverTimeoutDur, cfg.MasscanRate, dialer)
	if err != nil {
		p.Send(tui.MsgDone{CDN: name})
		return
	}

	statCtx, statCancel := context.WithCancel(ctx)
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-statCtx.Done():
				return
			case <-tick.C:
				p.Send(tui.MsgDiscoverStats{CDN: name, Scanned: discStats.Scanned.Load(), Found: discStats.Found.Load(), Total: discStats.TotalIPs})
			}
		}
	}()

	var discovered []string
	for ip := range discCh {
		discovered = append(discovered, ip)
	}
	statCancel()
	p.Send(tui.MsgDiscoverStats{CDN: name, Scanned: discStats.Scanned.Load(), Found: discStats.Found.Load(), Total: discStats.TotalIPs})

	if ctx.Err() != nil || len(discovered) == 0 {
		p.Send(tui.MsgDone{CDN: name})
		return
	}

	p.Send(tui.MsgPhase{CDN: name, Phase: tui.PhaseVerify})
	verCh, verStats := verifier.Verify(ctx, discovered, provider, cfg.VerifyWorkers, cfg.VerifyTimeoutDur, cloneDialer(dialer))
	for res := range verCh {
		if res.Clean {
			out.Write(res.IP)
		}
		p.Send(tui.MsgVerifyResult{CDN: name, IP: res.IP, IsClean: res.Clean, Latency: res.Latency, Status: res.Status, Verified: verStats.Verified.Load(), CleanN: verStats.Clean.Load(), Total: int64(len(discovered))})
	}
	p.Send(tui.MsgDone{CDN: name})
}

func cloneDialer(d *net.Dialer) *net.Dialer {
	if d == nil {
		return &net.Dialer{}
	}
	c := *d
	return &c
}
