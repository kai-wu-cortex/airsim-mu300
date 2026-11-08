package main

import (
	"errors"
	"net"
	"net/http"
	"strconv"
)

func tailnetIPv4(ip net.IP) bool {
	v := ip.To4()
	return v != nil && v[0] == 100 && v[1] >= 64 && v[1] <= 127
}
func privateIPv4(ip net.IP) bool {
	return ip.To4() != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || tailnetIPv4(ip))
}
func directAddresses(s managementSettings) ([]string, error) {
	lan, err := lanAddress(s.Interface)
	if err != nil {
		return nil, err
	}
	hosts := []string{lan}
	if s.TunnelInterface == "" {
		return hosts, nil
	}
	i, err := net.InterfaceByName(s.TunnelInterface)
	if err != nil || i.Flags&net.FlagUp == 0 {
		return nil, errors.New("Tailscale 接口未就绪，请先连接隧道")
	}
	addrs, err := i.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		ip, _, _ := net.ParseCIDR(addr.String())
		if tailnetIPv4(ip) {
			return append(hosts, ip.String()), nil
		}
	}
	return nil, errors.New("Tailscale 接口缺少 100.64.0.0/10 IPv4 地址")
}

type directEndpoint struct{ pair, control, pcm net.Listener }
type directListeners struct {
	endpoints []directEndpoint
	all       []net.Listener
}

func (d *directListeners) close() {
	for _, l := range d.all {
		l.Close()
	}
}

// Bind every socket before accepting claims. A partial failure closes all sockets,
// so the signed response never advertises a guessed or occupied gateway port.
func bindDirectListeners(hosts []string, s managementSettings) (*directListeners, error) {
	d := &directListeners{}
	for _, host := range hosts {
		var ls []net.Listener
		for _, port := range []int{s.PairPort, s.ControlPort, s.PCMPort} {
			l, err := net.Listen("tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
			if err != nil {
				d.close()
				return nil, err
			}
			d.all = append(d.all, l)
			ls = append(ls, l)
		}
		d.endpoints = append(d.endpoints, directEndpoint{ls[0], ls[1], ls[2]})
	}
	return d, nil
}
func (d *daemon) hasDirectRoute(r *http.Request) bool {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	return err == nil && d.directHosts[host]
}
func privatePairing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !privateIPv4(net.ParseIP(host)) {
			write(w, http.StatusForbidden, map[string]string{"error": "private_network_required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
