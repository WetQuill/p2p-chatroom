package network

import (
	"context"
	"net"
	"sync"
	"time"
)

// DualStackConnector 双栈连接器实现RFC 8305 Happy Eyeballs算法
type DualStackConnector struct {
	preferIPv6  bool
	v4Delay     time.Duration // IPv4延迟（默认300ms）
	fastFallback bool
	timeout     time.Duration // 连接超时

	mu       sync.Mutex
	results  chan connectResult
	cancelFn context.CancelFunc
}

// connectResult 连接结果
type connectResult struct {
	conn    net.Conn
	err     error
	isIPv6  bool
	latency time.Duration
}

// NewDualStackConnector 创建双栈连接器
func NewDualStackConnector(preferIPv6 bool) *DualStackConnector {
	return &DualStackConnector{
		preferIPv6:  preferIPv6,
		v4Delay:     300 * time.Millisecond,
		fastFallback: true,
		timeout:     10 * time.Second,
	}
}

// Connect 连接到指定主机和端口
func (dsc *DualStackConnector) Connect(host string, port int) (net.Conn, error) {
	return dsc.ConnectContext(context.Background(), host, port)
}

// ConnectContext 使用上下文的连接
func (dsc *DualStackConnector) ConnectContext(ctx context.Context, host string, port int) (net.Conn, error) {
	// 解析主机名获取所有IP地址
	ips, err := dsc.resolveHost(host)
	if err != nil {
		return nil, err
	}

	// 如果没有可用的IP地址
	if len(ips) == 0 {
		return nil, net.UnknownNetworkError("no IP addresses found for host")
	}

	// 如果有且只有一个IP地址，直接连接
	if len(ips) == 1 {
		return dsc.dialSingleIP(ctx, ips[0], port)
	}

	// 多个IP地址，使用Happy Eyeballs算法
	return dsc.happyEyeballsConnect(ctx, ips, port)
}

// SetIPv4Delay 设置IPv4延迟
func (dsc *DualStackConnector) SetIPv4Delay(delay time.Duration) {
	dsc.v4Delay = delay
}

// SetFastFallback 设置快速回退
func (dsc *DualStackConnector) SetFastFallback(enabled bool) {
	dsc.fastFallback = enabled
}

// SetTimeout 设置连接超时
func (dsc *DualStackConnector) SetTimeout(timeout time.Duration) {
	dsc.timeout = timeout
}

// 私有方法

func (dsc *DualStackConnector) resolveHost(host string) ([]net.IP, error) {
	// 如果是IP地址，直接返回
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	// 解析主机名
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	// 过滤和排序IP地址
	return dsc.sortIPs(addrs), nil
}

func (dsc *DualStackConnector) sortIPs(ips []net.IP) []net.IP {
	var ipv6Addrs, ipv4Addrs []net.IP

	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4Addrs = append(ipv4Addrs, ip)
		} else {
			ipv6Addrs = append(ipv6Addrs, ip)
		}
	}

	// 根据preferIPv6决定顺序
	var sorted []net.IP
	if dsc.preferIPv6 {
		sorted = append(sorted, ipv6Addrs...)
		sorted = append(sorted, ipv4Addrs...)
	} else {
		sorted = append(sorted, ipv4Addrs...)
		sorted = append(sorted, ipv6Addrs...)
	}

	return sorted
}

func (dsc *DualStackConnector) dialSingleIP(ctx context.Context, ip net.IP, port int) (net.Conn, error) {
	network := "tcp4"
	if ip.To4() == nil {
		network = "tcp6"
	}

	addr := &net.TCPAddr{
		IP:   ip,
		Port: port,
	}

	return net.DialTCP(network, nil, addr)
}

func (dsc *DualStackConnector) happyEyeballsConnect(ctx context.Context, ips []net.IP, port int) (net.Conn, error) {
	dsc.mu.Lock()
	if dsc.results != nil {
		dsc.results = nil
	}
	dsc.results = make(chan connectResult, len(ips))

	// 创建取消上下文
	connectCtx, cancel := context.WithTimeout(ctx, dsc.timeout)
	dsc.cancelFn = cancel
	dsc.mu.Unlock()

	defer cancel()

	// 启动并行连接尝试
	var wg sync.WaitGroup
	firstIPv6 := true

	for _, ip := range ips {
		isIPv6 := ip.To4() == nil

		// Happy Eyeballs算法：第一个IPv6地址立即尝试，IPv4地址延迟尝试
		var delay time.Duration
		if isIPv6 {
			if firstIPv6 {
				delay = 0
				firstIPv6 = false
			} else {
				// 后续IPv6地址延迟50ms
				delay = 50 * time.Millisecond
			}
		} else {
			// IPv4地址延迟v4Delay
			delay = dsc.v4Delay
			if dsc.fastFallback && firstIPv6 {
				// 如果还没有找到IPv6地址，可以快速回退
				delay = 50 * time.Millisecond
			}
		}

		wg.Add(1)
		go func(ip net.IP, delay time.Duration, isIPv6 bool) {
			defer wg.Done()

			// 等待延迟
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-connectCtx.Done():
					timer.Stop()
					return
				}
			}

			// 开始连接
			start := time.Now()
			conn, err := dsc.dialSingleIP(connectCtx, ip, port)
			latency := time.Since(start)

			select {
			case dsc.results <- connectResult{
				conn:    conn,
				err:     err,
				isIPv6:  isIPv6,
				latency: latency,
			}:
			case <-connectCtx.Done():
				if conn != nil {
					conn.Close()
				}
			}
		}(ip, delay, isIPv6)
	}

	// 等待第一个成功连接或所有尝试完成
	go func() {
		wg.Wait()
		dsc.mu.Lock()
		if dsc.results != nil {
			close(dsc.results)
			dsc.results = nil
		}
		dsc.mu.Unlock()
	}()

	// 收集结果
	var firstConn net.Conn
	var firstErr error
	var bestConn net.Conn
	var bestLatency time.Duration = 1<<63 - 1 // 最大int64值
	var ipv6Conn net.Conn
	var ipv6Err error
	var ipv4Conn net.Conn
	var ipv4Err error

	for result := range dsc.results {
		if result.err == nil {
			// 记录第一个成功连接
			if firstConn == nil {
				firstConn = result.conn
				firstErr = nil
			}

			// 记录最佳延迟连接
			if result.latency < bestLatency {
				if bestConn != nil {
					bestConn.Close()
				}
				bestConn = result.conn
				bestLatency = result.latency
			} else if result.conn != bestConn && result.conn != firstConn {
				result.conn.Close()
			}

			// 记录IPv6和IPv4连接
			if result.isIPv6 && ipv6Conn == nil {
				ipv6Conn = result.conn
			} else if !result.isIPv6 && ipv4Conn == nil {
				ipv4Conn = result.conn
			}
		} else {
			// 记录错误
			if firstConn == nil && firstErr == nil {
				firstErr = result.err
			}
			if result.isIPv6 && ipv6Err == nil {
				ipv6Err = result.err
			} else if !result.isIPv6 && ipv4Err == nil {
				ipv4Err = result.err
			}
		}
	}

	// 选择最佳连接
	if bestConn != nil {
		// 关闭其他连接
		if ipv6Conn != nil && ipv6Conn != bestConn {
			ipv6Conn.Close()
		}
		if ipv4Conn != nil && ipv4Conn != bestConn {
			ipv4Conn.Close()
		}
		return bestConn, nil
	}

	// 如果没有最佳连接，返回第一个连接
	if firstConn != nil {
		return firstConn, nil
	}

	// 返回错误
	if ipv6Err != nil && ipv4Err != nil {
		return nil, ipv6Err // 优先返回IPv6错误
	}
	if ipv6Err != nil {
		return nil, ipv6Err
	}
	return nil, ipv4Err
}

// GetPreferredFamily 获取首选地址族
func (dsc *DualStackConnector) GetPreferredFamily() string {
	if dsc.preferIPv6 {
		return "IPv6"
	}
	return "IPv4"
}

// Stats 连接统计
type Stats struct {
	IPv6Attempts   int           `json:"ipv6_attempts"`
	IPv4Attempts   int           `json:"ipv4_attempts"`
	IPv6Success    int           `json:"ipv6_success"`
	IPv4Success    int           `json:"ipv4_success"`
	AvgIPv6Latency time.Duration `json:"avg_ipv6_latency"`
	AvgIPv4Latency time.Duration `json:"avg_ipv4_latency"`
}

// GetStats 获取连接统计信息（简化实现）
func (dsc *DualStackConnector) GetStats() *Stats {
	// TODO: 实现统计收集
	return &Stats{}
}