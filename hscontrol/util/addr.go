package util

import (
	"errors"
	"fmt"
	"iter"
	"net/netip"
	"strings"

	"go4.org/netipx"
)

// ParseIPSet errors.
var (
	ErrNonNetworkBitsSet = errors.New("prefix contains non-network bits set")
	ErrInvalidIPRange    = errors.New("invalid IP range")
	ErrInvalidIPAddress  = errors.New("invalid IP address")
	ErrInvalidCIDRSize   = errors.New("invalid CIDR size")
)

// This is borrowed from, and updated to use [netipx.IPSet]
//
//nolint:lll // URL
// https://github.com/tailscale/tailscale/blob/71029cea2ddf82007b80f465b256d027eab0f02d/wgengine/filter/tailcfg.go#L97-L162
// TODO(kradalby): contribute upstream and make public.

// ParseIPSet parses arg as one:
//
//   - an IP address (IPv4 or IPv6)
//   - the string "*" to match everything (both IPv4 & IPv6)
//   - a CIDR (e.g. "192.168.0.0/16")
//   - a range of two IPs, inclusive, separated by hyphen ("2eff::1-2eff::0800")
//
// bits, if non-nil, is the legacy [tailcfg.FilterRule.SrcBits] CIDR length to make a IP
// address (without a slash) treated as a CIDR of *bits length.
func ParseIPSet(arg string, bits *int) (*netipx.IPSet, error) {
	var ipSet netipx.IPSetBuilder
	if arg == "*" {
		ipSet.AddPrefix(netip.PrefixFrom(netip.IPv4Unspecified(), 0))
		ipSet.AddPrefix(netip.PrefixFrom(netip.IPv6Unspecified(), 0))

		s, err := ipSet.IPSet()
		if err != nil {
			return nil, fmt.Errorf("building wildcard IP set: %w", err)
		}

		return s, nil
	}

	if strings.Contains(arg, "/") {
		pfx, err := netip.ParsePrefix(arg)
		if err != nil {
			return nil, fmt.Errorf("parsing prefix %q: %w", arg, err)
		}

		if pfx != pfx.Masked() {
			return nil, fmt.Errorf("%w: %v", ErrNonNetworkBitsSet, pfx)
		}

		ipSet.AddPrefix(pfx)

		s, err := ipSet.IPSet()
		if err != nil {
			return nil, fmt.Errorf("building CIDR IP set: %w", err)
		}

		return s, nil
	}

	if strings.Count(arg, "-") == 1 {
		ip1s, ip2s, _ := strings.Cut(arg, "-")

		ip1, err := netip.ParseAddr(ip1s)
		if err != nil {
			return nil, fmt.Errorf("parsing range start IP %q: %w", ip1s, err)
		}

		ip2, err := netip.ParseAddr(ip2s)
		if err != nil {
			return nil, fmt.Errorf("parsing range end IP %q: %w", ip2s, err)
		}

		r := netipx.IPRangeFrom(ip1, ip2)
		if !r.IsValid() {
			return nil, fmt.Errorf("%w: %q", ErrInvalidIPRange, arg)
		}

		for _, prefix := range r.Prefixes() {
			ipSet.AddPrefix(prefix)
		}

		s, err := ipSet.IPSet()
		if err != nil {
			return nil, fmt.Errorf("building range IP set: %w", err)
		}

		return s, nil
	}

	ip, err := netip.ParseAddr(arg)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidIPAddress, arg)
	}

	bits8 := uint8(ip.BitLen()) //nolint:gosec // BitLen is 32 or 128, always fits in uint8
	if bits != nil {
		if *bits < 0 || *bits > int(bits8) {
			return nil, fmt.Errorf("%w: %d for IP %q", ErrInvalidCIDRSize, *bits, arg)
		}

		bits8 = uint8(*bits) //nolint:gosec // bounds-checked against bits8 above
	}

	ipSet.AddPrefix(netip.PrefixFrom(ip, int(bits8)))

	s, err := ipSet.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building address IP set: %w", err)
	}

	return s, nil
}

func GetIPPrefixEndpoints(na netip.Prefix) (netip.Addr, netip.Addr) {
	ipRange := netipx.RangeOfPrefix(na)

	return ipRange.From(), ipRange.To()
}

func StringToIPPrefix(prefixes []string) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, len(prefixes))

	for index, prefixStr := range prefixes {
		prefix, err := netip.ParsePrefix(prefixStr)
		if err != nil {
			return nil, fmt.Errorf("parsing prefix %q: %w", prefixStr, err)
		}

		result[index] = prefix
	}

	return result, nil
}

// IPSetAddrIter returns a function that iterates over all the IPs in the [netipx.IPSet].
func IPSetAddrIter(ipSet *netipx.IPSet) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for _, rng := range ipSet.Ranges() {
			for ip := rng.From(); ip.Compare(rng.To()) <= 0; ip = ip.Next() {
				if !yield(ip) {
					return
				}
			}
		}
	}
}
