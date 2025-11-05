package hub

import (
	"net"
	"strings"
)

func LocalIPs() []string {
	ifaces, _ := net.Interfaces()

	ips := []string{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, _ := ifc.Addrs()

		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip = ip.To4()
			if ip == nil {
				continue
			}

			ips = append(ips, ip.String())
		}
	}

	// putting 192.168* ips first, because that's my home LAN initials
	order := func(prefix string) {
		for i := range ips {
			if strings.HasPrefix(ips[i], prefix) {
				ips[0], ips[i] = ips[i], ips[0]
				return
			}
		}
	}
	order("192.168.")
	return ips
}
