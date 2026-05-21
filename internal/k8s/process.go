package k8s

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func (c *Client) GetProcessMapForPod(pod PodInfo) (map[string]map[int]string, map[string]map[int]ListenInfo, error) {
	processMap := make(map[string]map[int]string)
	listenInfoMap := make(map[string]map[int]ListenInfo)
	if len(pod.Containers) == 0 {
		return processMap, listenInfoMap, nil
	}

	command := []string{"/bin/sh", "-c", "lsof -i -sTCP:LISTEN -P -n -F cn"}
	containerName := pod.Containers[0]
	slog.Debug("executing lsof command", "namespace", pod.Namespace, "pod", pod.Name, "container", containerName, "command", command)

	req := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.restCfg, "POST", req.URL())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create executor for pod %s: %v", pod.Name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	slog.Debug("lsof command finished", "namespace", pod.Namespace, "pod", pod.Name)
	slog.Debug("lsof stdout", "output", stdout.String())
	slog.Debug("lsof stderr", "output", stderr.String())

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, fmt.Errorf("lsof exec TIMED OUT in pod %s/%s (30s)", pod.Namespace, pod.Name)
		}
		return nil, nil, fmt.Errorf("exec failed on pod %s: %v, stdout: %s, stderr: %s", pod.Name, err, stdout.String(), stderr.String())
	}

	if stdout.Len() == 0 {
		slog.Debug("lsof returned empty stdout", "namespace", pod.Namespace, "pod", pod.Name)
	}

	processMap, listenInfoMap = ParseLsofOutput(stdout.String(), pod.IPs, pod.Namespace, pod.Name)
	return processMap, listenInfoMap, nil
}

// ParseLsofOutput parses `lsof -i -sTCP:LISTEN -P -n -F cn` output and returns
// per-IP maps of port→process name and port→ListenInfo. Uses net.SplitHostPort
// to correctly handle both IPv4 ("*:9099") and IPv6 ("[::]:8443") addresses.
//
// TODO(refactor): collapse to single ListenInfo map return; move to internal/netdiscovery
func ParseLsofOutput(output string, ips []string, namespace, podName string) (map[string]map[int]string, map[string]map[int]ListenInfo) {
	processMap := make(map[string]map[int]string)
	listenInfoMap := make(map[string]map[int]ListenInfo)

	sc := bufio.NewScanner(strings.NewReader(output))
	var currentProcess string
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'c':
			currentProcess = line[1:]
		case 'n':
			host, portStr, err := net.SplitHostPort(line[1:])
			if err != nil {
				slog.Warn("lsof: cannot parse address", "namespace", namespace, "pod", podName, "address", line[1:], "error", err)
				continue
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				slog.Error("lsof: invalid port number", "namespace", namespace, "pod", podName, "port", portStr)
				continue
			}
			for _, ip := range ips {
				if processMap[ip] == nil {
					processMap[ip] = make(map[int]string)
				}
				if listenInfoMap[ip] == nil {
					listenInfoMap[ip] = make(map[int]ListenInfo)
				}
				processMap[ip][port] = currentProcess
				listenInfoMap[ip][port] = ListenInfo{
					Port:          port,
					ListenAddress: host,
					ProcessName:   currentProcess,
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		slog.Warn("lsof: scanner error, output may be truncated", "namespace", namespace, "pod", podName, "error", err)
	}

	return processMap, listenInfoMap
}

// TODO(refactor): return map[int]bool port set; stop caching into processNameMap
func (c *Client) GetAndCachePodProcesses(pod PodInfo) map[string]map[int]string {
	c.processCacheMutex.Lock()
	if c.processDiscoveryAttempted[pod.Name] {
		c.processCacheMutex.Unlock()
		return nil
	}
	c.processDiscoveryAttempted[pod.Name] = true
	c.processCacheMutex.Unlock()

	processMap, listenInfoMap, err := c.GetProcessMapForPod(pod)
	if err != nil {
		slog.Warn("could not get process map for pod", "namespace", pod.Namespace, "pod", pod.Name, "error", err)
		return nil
	}

	if len(processMap) > 0 || len(listenInfoMap) > 0 {
		c.processCacheMutex.Lock()
		defer c.processCacheMutex.Unlock()
		for ip, portMap := range processMap {
			if _, ok := c.processNameMap[ip]; !ok {
				c.processNameMap[ip] = make(map[int]string)
			}
			for port, process := range portMap {
				c.processNameMap[ip][port] = process
			}
		}
		for ip, portMap := range listenInfoMap {
			if _, ok := c.listenInfoMap[ip]; !ok {
				c.listenInfoMap[ip] = make(map[int]ListenInfo)
			}
			for port, info := range portMap {
				c.listenInfoMap[ip][port] = info
			}
		}
	}

	return processMap
}

func (c *Client) IsLocalhostOnly(ip string, port int) (bool, string) {
	c.processCacheMutex.Lock()
	defer c.processCacheMutex.Unlock()

	// Primary source: lsof data (covers ports visible from Containers[0]).
	// When lsof has an entry for this port, treat it as authoritative and do
	// not fall through to proc data — lsof provides richer process context and
	// its bind address should be trusted over the kernel's /proc view.
	if portMap, ok := c.listenInfoMap[ip]; ok {
		if info, ok := portMap[port]; ok {
			if isLocalhostAddr(info.ListenAddress) {
				return true, info.ListenAddress
			}
			return false, ""
		}
	}

	// Fallback: /proc/net/tcp data (covers all containers via shared network
	// namespace, including ports owned by secondary containers that are
	// invisible to lsof due to PID namespace isolation).
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
