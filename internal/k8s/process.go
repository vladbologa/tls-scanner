package k8s

func (c *Client) cacheProcListenInfo(pod PodInfo, entries map[int]ProcListenEntry, inodeComm map[uint64]string) {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	for _, ip := range pod.IPs {
		if _, ok := c.procListenAddrMap[ip]; !ok {
			c.procListenAddrMap[ip] = make(map[int]string)
		}
		if _, ok := c.listenInfoMap[ip]; !ok {
			c.listenInfoMap[ip] = make(map[int]ListenInfo)
		}
		if _, ok := c.processNameMap[ip]; !ok {
			c.processNameMap[ip] = make(map[int]string)
		}
		for port, entry := range entries {
			if _, exists := c.procListenAddrMap[ip][port]; !exists {
				c.procListenAddrMap[ip][port] = entry.Addr
			}
			comm := inodeComm[entry.Inode]
			if comm == "" {
				continue
			}
			c.processNameMap[ip][port] = comm
			c.listenInfoMap[ip][port] = ListenInfo{
				Port:          port,
				ListenAddress: entry.Addr,
				ProcessName:   comm,
			}
		}
	}
}

// GetCachedProcessMap returns per-IP port→process maps populated by DiscoverPortsFromProc.
func (c *Client) GetCachedProcessMap(ips []string) map[string]map[int]string {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	result := make(map[string]map[int]string)
	for _, ip := range ips {
		portMap, ok := c.processNameMap[ip]
		if !ok || len(portMap) == 0 {
			continue
		}
		copied := make(map[int]string, len(portMap))
		for port, name := range portMap {
			copied[port] = name
		}
		result[ip] = copied
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c *Client) IsLocalhostOnly(ip string, port int) (bool, string) {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	if addrMap, ok := c.procListenAddrMap[ip]; ok {
		if addr, ok := addrMap[port]; ok {
			if isLocalhostAddr(addr) {
				return true, addr
			}
		}
	}

	return false, ""
}

// isLocalhostAddr reports whether addr is a loopback address.
func isLocalhostAddr(addr string) bool {
	return addr == "127.0.0.1" || addr == "::1" || addr == "localhost"
}

func (c *Client) GetListenInfo(ip string, port int) (ListenInfo, bool) {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	if portMap, ok := c.listenInfoMap[ip]; ok {
		if info, ok := portMap[port]; ok {
			return info, true
		}
	}
	return ListenInfo{}, false
}

// TODO(refactor): remove — redundant with GetListenInfo().ProcessName
func (c *Client) GetProcessName(ip string, port int) (string, bool) {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	if portMap, ok := c.processNameMap[ip]; ok {
		if name, ok := portMap[port]; ok {
			return name, true
		}
	}
	return "", false
}
