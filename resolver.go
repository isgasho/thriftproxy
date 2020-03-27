package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type IPResolvedCallback = func(hostname string, newIPs []string, removedIPs []string)

type addressWithCallback struct {
	addrs []string
	//times of resolved
	failed   int
	callback IPResolvedCallback
}

// Resolver dynamically resolve the host name to IP addresses
type Resolver struct {
	sync.Mutex
	//resolve interval
	interval time.Duration

	// 0: no stop, 1: stop the resolve
	stop int32

	hostIPs map[string]*addressWithCallback
}

func inStrArray(s string, a []string) bool {
	for _, t := range a {
		if t == s {
			return true
		}
	}
	return false
}
func strArraySub(a1 []string, a2 []string) []string {
	r := make([]string, 0)
	for _, s := range a1 {
		if !inStrArray(s, a2) {
			r = append(r, s)
		}
	}
	return r
}

func isIPAddress(addr string) bool {
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		addr = addr[1 : len(addr)-1]
	}
	return net.ParseIP(addr) != nil
}

func NewResolver(interval int) *Resolver {
	r := &Resolver{interval: time.Duration(interval) * time.Second,
		stop:    0,
		hostIPs: make(map[string]*addressWithCallback)}
	go r.periodicalResolve()
	return r
}

func (r *Resolver) ResolveHost(addr string, callback IPResolvedCallback) {
	r.Lock()
	defer r.Unlock()
	if _, ok := r.hostIPs[addr]; !ok {
		ips, err := r.doResolve(addr)
		if err == nil {
			r.hostIPs[addr] = &addressWithCallback{addrs: ips, failed: 0, callback: callback}
			callback(addr, ips, make([]string, 0))
		} else {
			r.hostIPs[addr] = &addressWithCallback{addrs: make([]string, 0), failed: 0, callback: callback}
		}
	}
}

func (r *Resolver) StopResolve(hostname string) {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.hostIPs[hostname]; ok {
		delete(r.hostIPs, hostname)

	}
}

func (r *Resolver) getHostnames() []string {
	r.Lock()
	defer r.Unlock()

	hostnames := make([]string, 0)

	for hostname, _ := range r.hostIPs {
		hostnames = append(hostnames, hostname)
	}
	return hostnames
}

// Stop stop the hostname resolve
func (r *Resolver) Stop() {
	if atomic.CompareAndSwapInt32(&r.stop, 0, 1) {
		log.Info("stop the hostname to IP resolve")
	}
}

func (r *Resolver) isStopped() bool {
	return atomic.LoadInt32(&r.stop) != 0
}

func (r *Resolver) GetAddrsOfHost(hostname string) []string {
	result := make([]string, 0)
	r.Lock()
	defer r.Unlock()

	if v, ok := r.hostIPs[hostname]; ok {
		result = append(result, v.addrs...)
	}
	return result
}
func (r *Resolver) periodicalResolve() {
	for !r.isStopped() {
		hostnames := r.getHostnames()

		for _, hostname := range hostnames {
			addrs, err := r.doResolve(hostname)
			if err == nil {
				r.addressResolved(hostname, addrs, err)
			} else {
				log.WithFields(log.Fields{"hostname": hostname}).Error("Fail to resolve host to IP")
			}
		}
		time.Sleep(r.interval)
	}
}

func (r *Resolver) addressResolved(hostname string, addrs []string, err error) {
	r.Lock()
	defer r.Unlock()
	if entry, ok := r.hostIPs[hostname]; ok {
		if !ok {
			entry.failed += 1
			if entry.failed > 3 && len(entry.addrs) > 0 {
				newAddrs := make([]string, 0)
				removedAddrs := entry.addrs
				entry.failed = 0
				entry.addrs = newAddrs
				log.WithFields(log.Fields{"hostname": hostname, "failed": entry.failed}).Error("the failed times for resolving hostname exceeds 3")
				go entry.callback(hostname, newAddrs, removedAddrs)
			}
		} else {
			newAddrs := strArraySub(addrs, entry.addrs)
			removedAddrs := strArraySub(entry.addrs, addrs)
			entry.failed = 0
			entry.addrs = addrs
			log.WithFields(log.Fields{"hostname": hostname, "newAddrs": newAddrs, "removedAddrs": removedAddrs}).Info("the ip address of host is changed")
			go entry.callback(hostname, newAddrs, removedAddrs)
		}
	}
}

func (r *Resolver) doResolve(addr string) ([]string, error) {
	hostname, port, err := splitAddr(addr)

	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(hostname)

	if err != nil {
		return nil, err
	}

	result := make([]string, 0)
	for _, ip := range ips {
		result = append(result, fmt.Sprintf("%s:%s", ip.String(), port))
	}
	return result, nil
}
