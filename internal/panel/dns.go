package panel

import (
	"context"
	"net"
	"sort"
	"time"
)

type DNSCheck struct {
	Records  []string
	LocalIPs []string
	Matches  bool
	Error    string
}

func checkDomainDNS(ctx context.Context, domain string) DNSCheck {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result := DNSCheck{LocalIPs: localIPStrings()}
	records, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	local := map[string]bool{}
	for _, ip := range result.LocalIPs {
		local[ip] = true
	}
	seen := map[string]bool{}
	for _, record := range records {
		ip := record.IP.String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		result.Records = append(result.Records, ip)
		if local[ip] {
			result.Matches = true
		}
	}
	sort.Strings(result.Records)
	return result
}

func localIPStrings() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			text := ip.String()
			if seen[text] {
				continue
			}
			seen[text] = true
			ips = append(ips, text)
		}
	}
	sort.Strings(ips)
	return ips
}
