package hostsnapshot

import (
	"net"
	"sort"
	"strings"
)

// LocalHostIPs 返回本机非 loopback / 非 link-local 地址，多分号分隔、无端口。
// IPv4 排在 IPv6 前；同族内按字符串排序以保证稳定。
func LocalHostIPs() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	return FormatHostIPs(addrs)
}

// FormatHostIPs 从 net.Addr 列表提取可上报的主机 IP（供测试注入）。
func FormatHostIPs(addrs []net.Addr) string {
	var v4, v6 []string
	seen := map[string]struct{}{}
	for _, addr := range addrs {
		ip := addrIP(addr)
		if ip == nil || !isReportableIP(ip) {
			continue
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		if ip.To4() != nil {
			v4 = append(v4, s)
		} else {
			v6 = append(v6, s)
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	out := append(v4, v6...)
	return strings.Join(out, ";")
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			host = addr.String()
		}
		return net.ParseIP(strings.Trim(host, "[]"))
	}
}

func isReportableIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	return true
}
