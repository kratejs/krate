package build

import (
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/qr"
)

// startedOnTerminal records whether stdout was a terminal when the process
// started. The dev log tee later swaps os.Stdout for a pipe, so the answer has
// to be taken before that.
var startedOnTerminal = isTTY(os.Stdout)

// lanAddresses lists this machine's non-loopback IPv4 addresses, private
// ranges first, skipping link-local ones.
func lanAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		ips = append(ips, ip)
	}
	sort.SliceStable(ips, func(i, j int) bool { return ips[i].IsPrivate() && !ips[j].IsPrivate() })
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

// devNetworkURLs returns the URLs other devices can reach the dev server at:
// one per LAN address when it listens on every interface, the address itself
// when it is bound to one non-loopback address, and none when it is loopback
// only.
func devNetworkURLs(bindHost string, port int, lan []string) []string {
	host := strings.Trim(strings.TrimSpace(bindHost), "[]")
	p := strconv.Itoa(port)
	switch {
	case isWildcardHost(host):
		urls := make([]string, 0, len(lan))
		for _, ip := range lan {
			urls = append(urls, "http://"+net.JoinHostPort(ip, p))
		}
		return urls
	case isLoopbackHostname(host):
		return nil
	}
	return []string{"http://" + net.JoinHostPort(host, p)}
}

// printDevNetwork prints the LAN URL, with a QR code on a terminal so a phone
// can open it, or a hint when the server is not reachable from the network.
func printDevNetwork(bindHost string, port int) {
	urls := devNetworkURLs(bindHost, port, lanAddresses())
	if len(urls) == 0 {
		if isWildcardHost(bindHost) {
			return
		}
		printf("  %snetwork%s    %snot exposed; set devServer.host to \"0.0.0.0\" (or run with --host 0.0.0.0) to test on a phone%s\n", cGray, cReset, cGray, cReset)
		return
	}
	for _, u := range urls {
		printf("  %snetwork%s    -> %s%s%s\n", cGreen, cReset, cCyan, u, cReset)
	}
	if !startedOnTerminal {
		return
	}
	m, err := qr.Encode(urls[0])
	if err != nil {
		return
	}
	printf("\n")
	for _, line := range m.ANSILines() {
		printf("    %s\n", line)
	}
	printf("\n")
}
