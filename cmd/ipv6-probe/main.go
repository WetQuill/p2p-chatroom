package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/WetQuill/p2p-chatroom/cmd/ipv6-probe/internal/detector"
	"github.com/WetQuill/p2p-chatroom/cmd/ipv6-probe/internal/reporter"
)

func main() {
	fmt.Println("P2P聊天室IPv6连通性探测工具 v1.0")
	fmt.Println("==================================")

	// 执行探测
	result := detector.RunComprehensiveTest()

	// 生成报告
	report := reporter.GenerateReport(result)

	// 输出报告
	if len(os.Args) > 1 && os.Args[1] == "--json" {
		jsonData, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Printf("生成JSON报告失败: %v\n", err)
			os.Exit(2)
		}
		fmt.Println(string(jsonData))
	} else if len(os.Args) > 1 && os.Args[1] == "--brief" {
		reporter.PrintBriefReport(report)
	} else {
		reporter.PrintHumanReport(report)
	}

	// 退出码：0=优秀，1=一般，2=需要改进，3=检测失败
	exitCode := 0
	if report.Score >= 80 {
		exitCode = 0
		fmt.Println("\n✅ IPv6环境准备就绪，建议启用IPv6模式")
	} else if report.Score >= 60 {
		exitCode = 1
		fmt.Println("\n⚠️  IPv6环境一般，建议部分启用IPv6功能")
	} else {
		exitCode = 2
		fmt.Println("\n❌ IPv6环境需要改进，建议检查网络配置")
	}

	if len(os.Args) > 1 && os.Args[1] == "--gen-config" {
		config := reporter.GenerateConfig(result)
		configJSON, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			fmt.Printf("生成配置失败: %v\n", err)
		} else {
			fmt.Println("\n生成的IPv6配置：")
			fmt.Println(string(configJSON))
		}
	}

	os.Exit(exitCode)
}