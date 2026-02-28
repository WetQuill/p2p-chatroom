package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IPv6ConfigManager IPv6配置管理器
type IPv6ConfigManager struct {
	configPath string
	config     *IPv6Config
}

// NewIPv6ConfigManager 创建IPv6配置管理器
func NewIPv6ConfigManager(configPath string) *IPv6ConfigManager {
	return &IPv6ConfigManager{
		configPath: configPath,
		config:     DefaultIPv6Config(),
	}
}

// Load 加载配置
func (cm *IPv6ConfigManager) Load() error {
	if cm.configPath == "" {
		// 使用默认配置
		return nil
	}

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，使用默认配置
			return nil
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config IPv6Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	cm.config = &config
	return nil
}

// Save 保存配置
func (cm *IPv6ConfigManager) Save() error {
	if cm.configPath == "" {
		return fmt.Errorf("配置路径未设置")
	}

	// 确保目录存在
	dir := filepath.Dir(cm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// GetConfig 获取当前配置
func (cm *IPv6ConfigManager) GetConfig() *IPv6Config {
	return cm.config
}

// SetConfig 设置配置
func (cm *IPv6ConfigManager) SetConfig(config *IPv6Config) {
	cm.config = config
}

// Validate 验证配置
func (cm *IPv6ConfigManager) Validate() error {
	cfg := cm.config

	// 验证模式
	validModes := map[string]bool{
		"preferred": true,
		"only":      true,
		"fallback":  true,
		"disabled":  true,
	}
	if !validModes[cfg.Mode] {
		return fmt.Errorf("无效的模式: %s", cfg.Mode)
	}

	// 验证加密方式
	validEncryptions := map[string]bool{
		"none":   true,
		"noise":  true,
		"tls":    true,
	}
	if !validEncryptions[cfg.Encryption] {
		return fmt.Errorf("无效的加密方式: %s", cfg.Encryption)
	}

	// 验证端口范围
	if cfg.UDPEnabled {
		if cfg.UDPPortStart <= 0 || cfg.UDPPortEnd <= 0 {
			return fmt.Errorf("UDP端口范围无效")
		}
		if cfg.UDPPortStart > cfg.UDPPortEnd {
			return fmt.Errorf("UDP起始端口不能大于结束端口")
		}
		if (cfg.UDPPortEnd - cfg.UDPPortStart + 1) < 10 {
			return fmt.Errorf("UDP端口范围至少需要10个端口")
		}
	}

	// 验证超时设置
	if cfg.ConnectionTimeout <= 0 {
		return fmt.Errorf("连接超时必须大于0")
	}
	if cfg.KeepAliveInterval <= 0 {
		return fmt.Errorf("保活间隔必须大于0")
	}
	if cfg.MaxRetries <= 0 {
		return fmt.Errorf("最大重试次数必须大于0")
	}

	// 验证密钥轮换
	if cfg.EnableAuth && cfg.KeyRotation < 1 {
		return fmt.Errorf("密钥轮换周期必须至少1小时")
	}

	return nil
}

// MergeWithMainConfig 与主配置合并
func (cm *IPv6ConfigManager) MergeWithMainConfig(mainCfg *Config) *Config {
	merged := *mainCfg

	// 这里可以根据需要将IPv6配置合并到主配置中
	// 目前保持独立，通过配置引用使用

	return &merged
}

// GenerateFromProbeResult 根据探测结果生成配置
func (cm *IPv6ConfigManager) GenerateFromProbeResult(result *ProbeResult) *IPv6Config {
	config := DefaultIPv6Config()

	// 根据探测结果调整配置
	if result.IPv6Supported && len(result.GlobalAddresses) > 0 {
		if result.Score >= 80 {
			config.Enabled = true
			config.Mode = "preferred"
			config.DualStack = true
			config.UDPEnabled = true
			config.Encryption = "noise"
			config.EnableAuth = true
			config.DHTEnabled = true
			config.MDNSEnabled = true
		} else if result.Score >= 60 {
			config.Enabled = true
			config.Mode = "fallback"
			config.DualStack = true
			config.UDPEnabled = true
			config.Encryption = "noise"
			config.EnableAuth = false
			config.DHTEnabled = false
			config.MDNSEnabled = true
		}
	} else {
		config.Enabled = false
		config.Mode = "disabled"
	}

	return config
}

// ProbeResult 探测结果（简化版，用于配置生成）
type ProbeResult struct {
	IPv6Supported   bool
	GlobalAddresses []string
	Score           int
}

// MigrateFromLegacy 从旧版本配置迁移
func (cm *IPv6ConfigManager) MigrateFromLegacy(oldConfig map[string]interface{}) (*IPv6Config, error) {
	config := DefaultIPv6Config()

	// 检查旧配置中是否有IPv6相关设置
	if enableIPv6, ok := oldConfig["enableIPv6"].(bool); ok && enableIPv6 {
		config.Enabled = true
		config.Mode = "preferred"
	}

	if mode, ok := oldConfig["ipv6Mode"].(string); ok {
		config.Mode = mode
	}

	if udpEnabled, ok := oldConfig["udpEnabled"].(bool); ok {
		config.UDPEnabled = udpEnabled
	}

	// 可以添加更多的迁移逻辑

	return config, nil
}

// ExportForUI 导出为UI可用的格式
func (cm *IPv6ConfigManager) ExportForUI() map[string]interface{} {
	cfg := cm.config
	return map[string]interface{}{
		"enabled":          cfg.Enabled,
		"mode":             cfg.Mode,
		"dualStack":        cfg.DualStack,
		"udpEnabled":       cfg.UDPEnabled,
		"encryption":       cfg.Encryption,
		"enableAuth":       cfg.EnableAuth,
		"dhtEnabled":       cfg.DHTEnabled,
		"mdnsEnabled":      cfg.MDNSEnabled,
		"keepAlive":        cfg.KeepAliveInterval,
		"connectionTimeout": cfg.ConnectionTimeout,
		"maxRetries":       cfg.MaxRetries,
	}
}