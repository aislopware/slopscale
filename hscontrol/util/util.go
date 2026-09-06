package util

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tailscale.com/util/cmpver"
	"tailscale.com/util/rands"
)

// URL parsing errors.
var (
	ErrMultipleURLsFound     = errors.New("multiple URLs found")
	ErrNoURLFound            = errors.New("no URL found")
	ErrEmptyTracerouteOutput = errors.New("empty traceroute output")
	ErrTracerouteHeaderParse = errors.New("parsing traceroute header")
	ErrTracerouteDidNotReach = errors.New("traceroute did not reach target")
)

func TailscaleVersionNewerOrEqual(minimum, toCheck string) bool {
	return cmpver.Compare(minimum, toCheck) <= 0 ||
		toCheck == "unstable" ||
		toCheck == "head"
}

// ParseLoginURLFromCLILogin parses the output of the tailscale up command to extract the login URL.
// It returns an error if not exactly one URL is found.
func ParseLoginURLFromCLILogin(output string) (*url.URL, error) {
	lines := strings.Split(output, "\n")

	var urlStr string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if urlStr != "" {
				return nil, fmt.Errorf("%w: %s and %s", ErrMultipleURLsFound, urlStr, line)
			}

			urlStr = line
		}
	}

	if urlStr == "" {
		return nil, ErrNoURLFound
	}

	loginURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	return loginURL, nil
}

type TraceroutePath struct {
	// Hop is the current jump in the total traceroute.
	Hop int

	// Hostname is the resolved hostname or IP address identifying the jump
	Hostname string

	// IP is the IP address of the jump
	IP netip.Addr

	// Latencies is a list of the latencies for this jump
	Latencies []time.Duration
}

type Traceroute struct {
	// Hostname is the resolved hostname or IP address identifying the target
	Hostname string

	// IP is the IP address of the target
	IP netip.Addr

	// Route is the path taken to reach the target if successful. The list is ordered by the path taken.
	Route []TraceroutePath

	// Success indicates if the traceroute was successful.
	Success bool

	// Err contains an error if  the traceroute was not successful.
	Err error
}

// parseLatency parses a traceroute latency token such as "1.5" or "<1",
// returning the duration rounded to the nearest microsecond. The second
// return value reports whether the token was a valid number.
func parseLatency(tok string) (time.Duration, bool) {
	ms, err := strconv.ParseFloat(strings.TrimPrefix(tok, "<"), 64)
	if err != nil {
		return 0, false
	}

	// Round to nearest microsecond to avoid floating point precision issues.
	return time.Duration(ms * float64(time.Millisecond)).Round(time.Microsecond), true
}

// hostIPCaptureGroups is the number of regexp capture groups a
// hostname-and-IP match produces: the full match, the hostname, and the IP.
const hostIPCaptureGroups = 3

// Regexes used to parse traceroute/tracert output. Compiled once at package
// init since ParseTraceroute can be called on a hot path (e.g. per debug
// request) and hop lines are parsed one at a time.
var (
	// tracerouteHeaderRegex handles both 'traceroute' and 'tracert' (Windows) headers.
	tracerouteHeaderRegex = regexp.MustCompile(
		`(?i)(?:traceroute|tracing route) to ([^ ]+) (?:\[([^\]]+)\]|\(([^)]+)\))`,
	)
	// tracerouteHopRegex is a flexible regex that handles various traceroute output formats:
	// "hostname (IP)", "hostname [IP]", "IP only", "* * *".
	tracerouteHopRegex           = regexp.MustCompile(`^\s*(\d+)\s+(.*)$`)
	tracerouteHostIPRegex        = regexp.MustCompile(`^([^ ]+) \(([^)]+)\)`)
	tracerouteHostIPBracketRegex = regexp.MustCompile(`^([^ ]+) \[([^\]]+)\]`)
	// tracerouteLatencyRegex matches latencies with flexible spacing and an optional '<'.
	tracerouteLatencyRegex = regexp.MustCompile(`(<?\d+(?:\.\d+)?)\s*ms\b`)
)

// parseTracerouteHeader parses the first line of traceroute/tracert output
// and returns the target hostname and IP.
func parseTracerouteHeader(headerLine string) (string, netip.Addr, error) {
	headerMatches := tracerouteHeaderRegex.FindStringSubmatch(headerLine)
	if len(headerMatches) < 2 {
		return "", netip.Addr{}, fmt.Errorf("%w: %s", ErrTracerouteHeaderParse, headerLine)
	}

	hostname := headerMatches[1]
	// IP can be in either capture group 2 or 3 depending on format
	ipStr := headerMatches[2]
	if ipStr == "" {
		ipStr = headerMatches[3]
	}

	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return "", netip.Addr{}, fmt.Errorf("parsing IP address %s: %w", ipStr, err)
	}

	return hostname, ip, nil
}

// isWindowsLatencyFirst reports whether remainder starts with a latency
// token, which indicates tracert's "latencies before hostname" format, e.g.
// "  1    <1 ms    <1 ms    <1 ms  router.local [192.168.1.1]".
func isWindowsLatencyFirst(remainder string) bool {
	if !strings.Contains(remainder, " ms ") || strings.HasPrefix(remainder, "*") {
		return false
	}

	firstSpace := strings.Index(remainder, " ")
	if firstSpace <= 0 {
		return false
	}

	firstPart := remainder[:firstSpace]

	_, err := strconv.ParseFloat(strings.TrimPrefix(firstPart, "<"), 64)

	return err == nil
}

// extractWindowsLeadingLatencies extracts the latencies at the start of a
// Windows tracert hop line and returns them along with the remainder of the
// line with those latencies removed.
func extractWindowsLeadingLatencies(remainder string) ([]time.Duration, string) {
	var latencies []time.Duration

	for {
		latMatch := tracerouteLatencyRegex.FindStringSubmatchIndex(remainder)
		if len(latMatch) == 0 || latMatch[0] > 0 {
			break
		}
		// Extract and remove the latency from the beginning
		if d, ok := parseLatency(remainder[latMatch[2]:latMatch[3]]); ok {
			latencies = append(latencies, d)
		}

		remainder = strings.TrimSpace(remainder[latMatch[1]:])
	}

	return latencies, remainder
}

// parseHopHostnameAndIP extracts the hostname and IP address (if any) from
// the start of remainder, returning them along with the unconsumed rest of
// the line.
func parseHopHostnameAndIP(remainder string) (string, netip.Addr, string) {
	if strings.HasPrefix(remainder, "*") {
		// Timeout hop; skip any remaining asterisks.
		return "*", netip.Addr{}, strings.TrimLeft(remainder, "* ")
	}

	if hostMatch := tracerouteHostIPRegex.FindStringSubmatch(remainder); len(hostMatch) >= hostIPCaptureGroups {
		// Format: hostname (IP)
		hopIP, _ := netip.ParseAddr(hostMatch[2])

		return hostMatch[1], hopIP, strings.TrimSpace(remainder[len(hostMatch[0]):])
	}

	if hostMatch := tracerouteHostIPBracketRegex.FindStringSubmatch(remainder); len(hostMatch) >= hostIPCaptureGroups {
		// Windows format: hostname followed by the IP address in square brackets.
		hopIP, _ := netip.ParseAddr(hostMatch[2])

		return hostMatch[1], hopIP, strings.TrimSpace(remainder[len(hostMatch[0]):])
	}

	// Try to parse as IP only or hostname only.
	parts := strings.Fields(remainder)
	if len(parts) == 0 {
		return "", netip.Addr{}, remainder
	}

	var hopIP netip.Addr

	parsedIP, err := netip.ParseAddr(parts[0])
	if err == nil {
		hopIP = parsedIP
	}

	return parts[0], hopIP, strings.TrimSpace(strings.Join(parts[1:], " "))
}

// extractLatencies parses all latency tokens out of remainder.
func extractLatencies(remainder string) []time.Duration {
	var latencies []time.Duration

	for _, match := range tracerouteLatencyRegex.FindAllStringSubmatch(remainder, -1) {
		if len(match) > 1 {
			if d, ok := parseLatency(match[1]); ok {
				latencies = append(latencies, d)
			}
		}
	}

	return latencies
}

// parseHopLine parses a single traceroute hop line into a [TraceroutePath].
// reachedTarget reports whether this hop's IP matches the traceroute's target.
func parseHopLine(hop int, remainder string, targetIP netip.Addr) (TraceroutePath, bool) {
	var latencies []time.Duration

	// Check for Windows tracert format which has latencies before hostname.
	latencyFirst := isWindowsLatencyFirst(remainder)
	if latencyFirst {
		latencies, remainder = extractWindowsLeadingLatencies(remainder)
	}

	hopHostname, hopIP, remainder := parseHopHostnameAndIP(remainder)

	// Extract latencies from the remaining part (if not already done).
	if !latencyFirst {
		latencies = extractLatencies(remainder)
	}

	path := TraceroutePath{
		Hop:       hop,
		Hostname:  hopHostname,
		IP:        hopIP,
		Latencies: latencies,
	}

	return path, hopIP == targetIP
}

// ParseTraceroute parses the output of the traceroute command and returns a [Traceroute] struct.
func ParseTraceroute(output string) (Traceroute, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 {
		return Traceroute{}, ErrEmptyTracerouteOutput
	}

	hostname, ip, err := parseTracerouteHeader(lines[0])
	if err != nil {
		return Traceroute{}, err
	}

	result := Traceroute{
		Hostname: hostname,
		IP:       ip,
		Route:    []TraceroutePath{},
		Success:  false,
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		matches := tracerouteHopRegex.FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}

		hop, err := strconv.Atoi(matches[1])
		if err != nil {
			// Skip lines that don't start with a hop number
			continue
		}

		path, reachedTarget := parseHopLine(hop, strings.TrimSpace(matches[2]), ip)

		result.Route = append(result.Route, path)

		if reachedTarget {
			result.Success = true
		}
	}

	// If we didn't reach the target, it's unsuccessful
	if !result.Success {
		result.Err = ErrTracerouteDidNotReach
	}

	return result, nil
}

func IsCI() bool {
	_, ci := os.LookupEnv("CI")
	_, gh := os.LookupEnv("GITHUB_RUN_ID")

	return ci || gh
}

// GenerateRegistrationKey generates a vanity key for tracking web authentication
// registration flows in logs. This key is NOT stored in the database and does NOT use bcrypt -
// it's purely for observability and correlating log entries during the registration process.
func GenerateRegistrationKey() (string, error) {
	const (
		registerKeyPrefix = "hskey-reg-"
		registerKeyLength = 64
	)

	return registerKeyPrefix + rands.HexString(registerKeyLength), nil
}
