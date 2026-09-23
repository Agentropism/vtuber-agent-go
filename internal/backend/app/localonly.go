package app

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// localOnly 只放行「本机」请求：回环地址，或 WSL 虚拟网络里的宿主机地址。
//
// 为什么不止回环：本项目的服务跑在 WSL 里，Windows 侧浏览器要走 WSL IP 访问
// （实测 localhost 转发不生效），此时服务看到的对端是宿主机在 vEthernet(WSL) 上的
// 地址，也就是 WSL 的默认路由网关。它同样代表「跑着网关的这台电脑」——
// 局域网里的其他机器在 WSL2 NAT 模式下根本进不来，不可能是这个地址。
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalRequest(r.RemoteAddr) {
			logger.Warnf("拒绝非本机访问: %s %s 来自 %s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, "该页面只允许本机访问", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLocalRequest 判断对端地址是否代表本机。
func isLocalRequest(remoteAddr string) bool {
	if isLoopback(remoteAddr) {
		return true
	}

	gateway := hostGateway()
	if gateway == nil {
		return false
	}

	return remoteIP(remoteAddr).Equal(gateway)
}

// isLoopback 判断对端地址是否回环地址。
func isLoopback(remoteAddr string) bool {
	ip := remoteIP(remoteAddr)

	return ip != nil && ip.IsLoopback()
}

// remoteIP 从 RemoteAddr 里取出 IP；解析不出来返回 nil。
func remoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	return net.ParseIP(strings.Trim(host, "[]"))
}

var (
	gatewayOnce sync.Once
	gatewayAddr net.IP
)

// hostGateway 返回 WSL 虚拟网络里宿主机的地址（默认路由网关），读一次后缓存。
func hostGateway() net.IP {
	gatewayOnce.Do(func() { gatewayAddr = readDefaultGateway() })

	return gatewayAddr
}

// readDefaultGateway 从 /proc/net/route 解析默认路由的网关地址。
//
// 文件里的地址是 32 位小端十六进制（例如 01401AAC 是 172.26.64.1），
// 读不到时返回 nil——此时本机判定退化为纯回环，功能不受影响。
func readDefaultGateway() net.IP {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Scan() // 跳过表头
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}

		raw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || raw == 0 {
			continue
		}

		return net.IPv4(byte(raw), byte(raw>>8), byte(raw>>16), byte(raw>>24))
	}

	return nil
}
