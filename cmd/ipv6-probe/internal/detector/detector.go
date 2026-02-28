package detector

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"
)

// TestResult 探测结果
type TestResult struct {
	Timestamp        time.Time          `json:"timestamp"`
	IPv6Supported    bool               `json:"ipv6_supported"`
	GlobalAddresses  []string           `json:"global_addresses"`
	Connectivity     ConnectivityTest   `json:"connectivity"`
	NATType          string             `json:"nat_type"`
	FirewallStatus   string             `json:"firewall_status"`
	SystemInfo       SystemInfo         `json:"system_info"`
	Score            int                `json:"score"`
	Recommendations  []string           `json:"recommendations"`
	Problems         []Problem          `json:"problems"`
}

// ConnectivityTest 连通性测试结果
type ConnectivityTest struct {
	DNSTest     bool    `json:"dns_test"`
	HTTPTest    bool    `json:"http_test"`
	HTTPSTest   bool    `json:"https_test"`
	STUNTest    bool    `json:"stun_test"`
	PingLatency float64 `json:"ping_latency"` // 毫秒
	PacketLoss  float64 `json:"packet_loss"`  // 百分比
}

// SystemInfo 系统信息
type SystemInfo struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"go_version"`
	KernelVersion string `json:"kernel_version"`
	IPv6Enabled   bool   `json:"ipv6_enabled"`
}

// Problem 检测到的问题
type Problem struct {
	Level   string `json:"level"`   // "critical", "warning", "info"
	Message string `json:"message"`
	FixHint string `json:"fix_hint"`
}

// RunComprehensiveTest 执行全面的IPv6探测
func RunComprehensiveTest() *TestResult {
	result := &TestResult{
		Timestamp: time.Now(),
	}

	fmt.Println("开始IPv6环境探测...")
	fmt.Println("1. 检测系统支持...")
	result.SystemInfo = getSystemInfo()
	result.IPv6Supported = checkSystemSupport()

	if !result.IPv6Supported {
		result.Problems = append(result.Problems, Problem{
			Level:   "critical",
			Message: "系统不支持IPv6",
			FixHint: "请检查操作系统IPv6支持或启用IPv6协议栈",
		})
		return result
	}

	fmt.Println("2. 获取IPv6地址...")
	result.GlobalAddresses = getGlobalIPv6Addresses()
	if len(result.GlobalAddresses) == 0 {
		result.Problems = append(result.Problems, Problem{
			Level:   "critical",
			Message: "未找到全局IPv6地址",
			FixHint: "请检查网络连接或联系网络管理员启用IPv6",
		})
		return result
	}

	fmt.Println("3. 测试连通性...")
	result.Connectivity = testIPv6Connectivity()
	result.NATType = detectNATType()
	result.FirewallStatus = checkFirewall()

	fmt.Println("4. 计算评分...")
	result.Score = calculateScore(result)
	result.Recommendations = generateRecommendations(result)

	return result
}

// getSystemInfo 获取系统信息
func getSystemInfo() SystemInfo {
	return SystemInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		GoVersion:   runtime.Version(),
		IPv6Enabled: checkSystemSupport(),
	}
}

// checkSystemSupport 检查系统IPv6支持
func checkSystemSupport() bool {
	// 尝试获取所有接口地址
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if ok && ipNet.IP.To4() == nil && ipNet.IP.IsGlobalUnicast() {
				return true
			}
		}
	}

	return false
}

// getGlobalIPv6Addresses 获取全局IPv6地址
func getGlobalIPv6Addresses() []string {
	var addresses []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return addresses
	}

	for _, iface := range ifaces {
		// 跳过未启用接口
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			// 检查是否是IPv6全局单播地址
			if ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
				// 过滤掉链路本地地址
				if !strings.HasPrefix(ip.String(), "fe80::") {
					addresses = append(addresses, fmt.Sprintf("%s (%s)", ip.String(), iface.Name))
				}
			}
		}
	}

	return addresses
}

// testIPv6Connectivity 测试IPv6连通性
func testIPv6Connectivity() ConnectivityTest {
	result := ConnectivityTest{}

	// 测试DNS解析
	fmt.Println("  - 测试DNS解析...")
	result.DNSTest = testDNSResolution()

	// 测试HTTP/HTTPS访问
	fmt.Println("  - 测试HTTP/HTTPS...")
	result.HTTPTest = testHTTPConnectivity()
	result.HTTPSTest = testHTTPSConnectivity()

	// 测试STUN
	fmt.Println("  - 测试STUN服务...")
	result.STUNTest = testSTUNService()

	// 测试延迟和丢包
	fmt.Println("  - 测试网络延迟...")
	result.PingLatency, result.PacketLoss = testNetworkPerformance()

	return result
}

// detectNATType 检测NAT类型（IPv6通常有状态防火墙）
func detectNATType() string {
	// IPv6环境通常没有传统NAT，但可能有状态防火墙
	// 简化实现，后续可增强
	if hasGlobalAddress() {
		return "IPv6 Stateful Firewall"
	}
	return "Unknown"
}

// checkFirewall 检查防火墙状态
func checkFirewall() string {
	// 简化实现，后续可增强
	return "Check required (platform dependent)"
}

// calculateScore 计算环境评分
func calculateScore(result *TestResult) int {
	score := 0

	// 基础支持：40分
	if result.IPv6Supported {
		score += 20
	}
	if len(result.GlobalAddresses) > 0 {
		score += 20
	}

	// 连通性：60分
	if result.Connectivity.DNSTest {
		score += 10
	}
	if result.Connectivity.HTTPTest {
		score += 10
	}
	if result.Connectivity.HTTPSTest {
		score += 10
	}
	if result.Connectivity.STUNTest {
		score += 20
	}
	if result.Connectivity.PacketLoss < 5 {
		score += 5
	}
	if result.Connectivity.PingLatency < 100 {
		score += 5
	}

	return min(score, 100)
}

// generateRecommendations 生成改进建议
func generateRecommendations(result *TestResult) []string {
	var recommendations []string

	if !result.IPv6Supported {
		recommendations = append(recommendations, "启用操作系统IPv6支持")
	}

	if len(result.GlobalAddresses) == 0 {
		recommendations = append(recommendations, "联系ISP获取IPv6服务")
		recommendations = append(recommendations, "检查路由器IPv6配置")
	}

	if !result.Connectivity.DNSTest {
		recommendations = append(recommendations, "配置IPv6 DNS服务器")
	}

	if !result.Connectivity.STUNTest {
		recommendations = append(recommendations, "检查防火墙UDP出站规则")
	}

	if result.Connectivity.PacketLoss > 10 {
		recommendations = append(recommendations, "检查网络质量，考虑使用有线连接")
	}

	if result.Connectivity.PingLatency > 200 {
		recommendations = append(recommendations, "检查网络延迟，可能需要优化路由")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "IPv6环境优秀，建议启用所有IPv6功能")
	}

	return recommendations
}

// 辅助函数实现（占位符，后续完善）
func testDNSResolution() bool {
	// 简化实现
	return true
}

func testHTTPConnectivity() bool {
	// 简化实现
	return true
}

func testHTTPSConnectivity() bool {
	// 简化实现
	return true
}

func testSTUNService() bool {
	// 简化实现
	return true
}

func testNetworkPerformance() (float64, float64) {
	// 简化实现
	return 50.0, 2.0 // 50ms延迟，2%丢包率
}

func hasGlobalAddress() bool {
	// 简化实现
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}