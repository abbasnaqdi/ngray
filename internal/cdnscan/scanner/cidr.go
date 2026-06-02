package scanner

import (
	"fmt"
	"iter"
	"net/netip"
)

// CountHosts returns the number of usable host addresses in a CIDR block.
func CountHosts(cidr string) (int64, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	prefix = prefix.Masked()
	bits := prefix.Bits()
	addrBits := prefix.Addr().BitLen()
	hostBits := addrBits - bits

	switch {
	case hostBits == 0:
		return 1, nil
	case hostBits == 1:
		return 2, nil
	case hostBits >= 62:
		return 1<<62 - 1, nil
	default:
		return (int64(1) << hostBits) - 2, nil
	}
}

// IterHosts yields each usable host IP in a CIDR without loading all IPs into memory.
// Network and broadcast addresses are excluded for subnets smaller than /31.
func IterHosts(cidr string) (iter.Seq[string], error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	prefix = prefix.Masked()

	bits := prefix.Bits()
	addrBits := prefix.Addr().BitLen()
	hostBits := addrBits - bits

	return func(yield func(string) bool) {
		if hostBits <= 1 {
			// /31 or /32: all addresses are host addresses
			addr := prefix.Addr()
			for prefix.Contains(addr) {
				if !yield(addr.String()) {
					return
				}
				addr = addr.Next()
			}
			return
		}

		// Regular subnet: skip network address, stop before broadcast.
		addr := prefix.Addr().Next() // first host = network+1
		for prefix.Contains(addr) {
			next := addr.Next()
			if !prefix.Contains(next) {
				break // addr is the broadcast address
			}
			if !yield(addr.String()) {
				return
			}
			addr = next
		}
	}, nil
}
