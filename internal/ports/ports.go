// Package ports allocates loopback backend ports per core (each core owns a
// distinct range) and verifies a port is free before it is assigned.
package ports

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/state"
)

type Range struct{ Lo, Hi int }

// CoreRanges keeps each core's backend ports in its own band. These bind to
// 127.0.0.1 only and are never exposed, so the choice is for clean bookkeeping.
var CoreRanges = map[string]Range{
	"xray":    {11000, 11999},
	"shoes":   {12000, 12999},
	"singbox": {13000, 13999},
}

// HTTPSink is the loopback fallback that nginx routes unknown SNIs to.
const HTTPSink = 8080

type Allocator struct {
	st   *state.State
	used map[int]bool
}

func NewAllocator(st *state.State) *Allocator {
	used := map[int]bool{}
	for _, p := range st.Ports {
		used[p] = true
	}
	return &Allocator{st: st, used: used}
}

// Backend returns a stable loopback port for id within core's range, reusing a
// prior assignment when it is still free, otherwise picking the next free port.
func (a *Allocator) Backend(core, id string) (int, error) {
	r, ok := CoreRanges[core]
	if !ok {
		return 0, fmt.Errorf("unknown core %q", core)
	}
	if p, ok := a.st.Ports[id]; ok && p >= r.Lo && p <= r.Hi && FreeTCP(p) {
		a.used[p] = true
		return p, nil
	}
	for p := r.Lo; p <= r.Hi; p++ {
		if a.used[p] || !FreeTCP(p) {
			continue
		}
		a.used[p] = true
		a.st.Ports[id] = p
		return p, nil
	}
	return 0, fmt.Errorf("no free port in range %d-%d for %s", r.Lo, r.Hi, core)
}

// FreeTCP reports whether a loopback TCP port can be bound right now.
func FreeTCP(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// FreePublicTCP reports whether a public TCP port is bindable.
func FreePublicTCP(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// FreeUDP reports whether a public UDP port is bindable.
func FreeUDP(port int) bool {
	c, err := net.ListenPacket("udp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	c.SetDeadline(time.Now())
	c.Close()
	return true
}
