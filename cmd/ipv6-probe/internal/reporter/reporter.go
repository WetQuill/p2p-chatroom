package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/WetQuill/p2p-chatroom/cmd/ipv6-probe/internal/detector"
	"github.com/WetQuill/p2p-chatroom/config"
)

// Report 最终报告结构
type Report struct {
	Title         string                  `json:"title"`
	Timestamp     time.Time               `json:"timestamp"`
	Summary       string                  `json:"summary"`
	Score         int                     `json:"score"`
	ScoreLevel    string                  `json:"score_level"`
	Details       *detector.TestResult    `json:"details"`
	ConfigPreview *config.IPv6Config      `json:"config_preview,omitempty"`
}

// GenerateReport 生成完整报告
func GenerateReport(result *detector.TestResult) *Report {
	report := &Report{
		Title:     "IPv6连通性检测报告",
		Timestamp: time.Now(),
		Details:   result,
		Score:     result.Score,
	}

	report.ScoreLevel = getScoreLevel(result.Score)
	report.Summary = generateSummary(result)

	if len(result.GlobalAddresses) > 0 {
		report.ConfigPreview = GenerateConfig(result)
	}

	return report
}

// PrintHumanReport 打印人类可读的报告
func PrintHumanReport(report *Report) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("             IPv6连通性检测报告")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("报告时间: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("综合评分: %d/100 [%s]\n", report.Score, report.ScoreLevel)
	fmt.Println()

	fmt.Println("📋 系统信息:")
	fmt.Printf("  - 操作系统: %s/%s\n", report.Details.SystemInfo.OS, report.Details.SystemInfo.Arch)
	fmt.Printf("  - Go版本: %s\n", report.Details.SystemInfo.GoVersion)
	fmt.Printf("  - IPv6支持: %v\n", report.Details.IPv6Supported)

	fmt.Println("\n🌐 网络状态:")
	if len(report.Details.GlobalAddresses) == 0 {
		fmt.Println("  - 全局IPv6地址: 未找到")
	} else {
		fmt.Println("  - 全局IPv6地址:")
		for _, addr := range report.Details.GlobalAddresses {
			fmt.Printf("    • %s\n", addr)
		}
	}
	fmt.Printf("  - NAT/防火墙类型: %s\n", report.Details.NATType)
	fmt.Printf("  - 防火墙状态: %s\n", report.Details.FirewallStatus)

	fmt.Println("\n📡 连通性测试:")
	conn := report.Details.Connectivity
	fmt.Printf("  - DNS解析: %v\n", boolToIcon(conn.DNSTest))
	fmt.Printf("  - HTTP访问: %v\n", boolToIcon(conn.HTTPTest))
	fmt.Printf("  - HTTPS访问: %v\n", boolToIcon(conn.HTTPSTest))
	fmt.Printf("  - STUN服务: %v\n", boolToIcon(conn.STUNTest))
	fmt.Printf("  - 网络延迟: %.2f ms\n", conn.PingLatency)
	fmt.Printf("  - 丢包率: %.2f%%\n", conn.PacketLoss)

	fmt.Println("\n🔍 检测到的问题:")
	if len(report.Details.Problems) == 0 {
		fmt.Println("  - 无严重问题")
	} else {
		for _, problem := range report.Details.Problems {
			fmt.Printf("  - [%s] %s\n", problem.Level, problem.Message)
			if problem.FixHint != "" {
				fmt.Printf("      💡 建议: %s\n", problem.FixHint)
			}
		}
	}

	fmt.Println("\n💡 改进建议:")
	if len(report.Details.Recommendations) == 0 {
		fmt.Println("  - IPv6环境优秀，无需特别改进")
	} else {
		for i, rec := range report.Details.Recommendations {
			fmt.Printf("  %d. %s\n", i+1, rec)
		}
	}

	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println(report.Summary)
	fmt.Println(strings.Repeat("=", 60))

	if report.ConfigPreview != nil {
		fmt.Println("\n📝 推荐配置 (使用 --gen-config 查看完整配置):")
		fmt.Printf("  - 模式: %s\n", report.ConfigPreview.Mode)
		fmt.Printf("  - 双栈: %v\n", report.ConfigPreview.DualStack)
		fmt.Printf("  - UDP传输: %v\n", report.ConfigPreview.UDPEnabled)
		fmt.Printf("  - 加密: %s\n", report.ConfigPreview.Encryption)
	}
}

// PrintBriefReport 打印简要报告
func PrintBriefReport(report *Report) {
	fmt.Printf("IPv6检测: 评分%d/100 [%s]\n", report.Score, report.ScoreLevel)
	fmt.Printf("状态: %s\n", getStatusEmoji(report.Score))

	if len(report.Details.GlobalAddresses) > 0 {
		fmt.Printf("地址: %s\n", report.Details.GlobalAddresses[0])
	}

	fmt.Printf("DNS:%v HTTP:%v HTTPS:%v STUN:%v\n",
		boolToBrief(report.Details.Connectivity.DNSTest),
		boolToBrief(report.Details.Connectivity.HTTPTest),
		boolToBrief(report.Details.Connectivity.HTTPSTest),
		boolToBrief(report.Details.Connectivity.STUNTest))
}

// GenerateConfig 根据探测结果生成推荐的IPv6配置
func GenerateConfig(result *detector.TestResult) *config.IPv6Config {
	cfg := &config.IPv6Config{
		ModuleVersion: "1.0.0",
		KeyRotation:   24,
		MaxRetries:    3,
	}

	// 根据评分决定模式
	if result.Score >= 80 {
		cfg.Enabled = true
		cfg.Mode = "preferred"
		cfg.DualStack = true
		cfg.UDPEnabled = true
		cfg.Encryption = "noise"
		cfg.NoisePattern = "XX_25519_ChaChaPoly_BLAKE2s"
		cfg.EnableAuth = true
		cfg.DHTEnabled = true
		cfg.MDNSEnabled = true
	} else if result.Score >= 60 {
		cfg.Enabled = true
		cfg.Mode = "fallback"
		cfg.DualStack = true
		cfg.UDPEnabled = true
		cfg.Encryption = "noise"
		cfg.NoisePattern = "XX_25519_ChaChaPoly_BLAKE2s"
		cfg.EnableAuth = false
		cfg.DHTEnabled = false
		cfg.MDNSEnabled = true
	} else {
		cfg.Enabled = false
		cfg.Mode = "disabled"
		cfg.DualStack = false
		cfg.UDPEnabled = false
		cfg.Encryption = "none"
		cfg.EnableAuth = false
		cfg.DHTEnabled = false
		cfg.MDNSEnabled = false
	}

	// 设置默认端口
	cfg.UDPPortStart = 10100
	cfg.UDPPortEnd = 10200
	cfg.KeepAliveInterval = 30
	cfg.ConnectionTimeout = 10

	// 添加推荐的前缀
	if len(result.GlobalAddresses) > 0 {
		// 提取前缀示例
		cfg.PreferredPrefix = "2001::/32"
	}

	return cfg
}

// 辅助函数
func getScoreLevel(score int) string {
	switch {
	case score >= 90:
		return "优秀"
	case score >= 80:
		return "良好"
	case score >= 60:
		return "一般"
	case score >= 40:
		return "较差"
	default:
		return "不可用"
	}
}

func generateSummary(result *detector.TestResult) string {
	switch {
	case !result.IPv6Supported:
		return "系统不支持IPv6，需要启用IPv6协议栈"
	case len(result.GlobalAddresses) == 0:
		return "系统支持IPv6但未获取到全局地址，请联系ISP或检查网络配置"
	case result.Score >= 90:
		return "IPv6环境优秀，支持所有高级功能，建议启用IPv6优先模式"
	case result.Score >= 80:
		return "IPv6环境良好，适合使用IPv6优先模式"
	case result.Score >= 60:
		return "IPv6环境一般，建议使用IPv6回退模式"
	default:
		return "IPv6环境较差，建议使用传统IPv4模式"
	}
}

func boolToIcon(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}

func boolToBrief(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}

func getStatusEmoji(score int) string {
	switch {
	case score >= 80:
		return "✅"
	case score >= 60:
		return "⚠️"
	default:
		return "❌"
	}
}