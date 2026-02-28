package ipv6_test

import (
	"testing"
	"time"

	"github.com/WetQuill/p2p-chatroom/config"
	"github.com/WetQuill/p2p-chatroom/pkg/ipv6"
)

func TestModuleCreation(t *testing.T) {
	module := ipv6.NewModule()
	if module == nil {
		t.Fatal("NewModule() returned nil")
	}
}

func TestModuleLifecycle(t *testing.T) {
	module := ipv6.NewModule()

	// 使用默认配置初始化
	cfg := config.DefaultIPv6Config()
	err := module.Init(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// 检查初始状态
	status := module.GetStatus()
	if status.State != "stopped" {
		t.Errorf("Expected state 'stopped', got %s", status.State)
	}

	// 启动模块
	err = module.Start()
	if err != nil {
		t.Logf("Start returned error (might be expected): %v", err)
	}

	// 检查运行状态
	status = module.GetStatus()
	t.Logf("Module state: %s", status.State)

	// 停止模块
	err = module.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// 检查停止状态
	status = module.GetStatus()
	if status.State != "stopped" {
		t.Errorf("Expected state 'stopped' after stop, got %s", status.State)
	}
}

func TestEventBus(t *testing.T) {
	eventBus := ipv6.NewEventBus(10)

	// 订阅事件
	ch := eventBus.Subscribe()
	defer eventBus.Unsubscribe(ch)

	// 发布测试事件
	testEvent := &ipv6.Event{
		Type:    "test_event",
		Source:  "test",
		Message: "Test message",
		Time:    time.Now(),
	}

	eventBus.Publish(testEvent)

	// 接收事件
	select {
	case event := <-ch:
		if event.Type != "test_event" {
			t.Errorf("Expected event type 'test_event', got %s", event.Type)
		}
		if event.Message != "Test message" {
			t.Errorf("Expected message 'Test message', got %s", event.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for event")
	}

	// 测试发布带类型的事件
	eventBus.PublishWithType("another_event", "test", "Another message", nil)

	select {
	case event := <-ch:
		if event.Type != "another_event" {
			t.Errorf("Expected event type 'another_event', got %s", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for another event")
	}
}

func TestComponentManager(t *testing.T) {
	cm := ipv6.NewComponentManager()

	// 创建测试组件
	testComp := &testComponent{name: "test"}

	// 注册组件
	err := cm.RegisterComponent("test", testComp)
	if err != nil {
		t.Fatalf("RegisterComponent failed: %v", err)
	}

	// 获取组件
	comp, exists := cm.GetComponent("test")
	if !exists {
		t.Fatal("Component not found after registration")
	}
	if comp.GetName() != "test" {
		t.Errorf("Expected component name 'test', got %s", comp.GetName())
	}

	// 获取组件名称列表
	names := cm.GetComponentNames()
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("Expected component names ['test'], got %v", names)
	}
}

// 测试组件实现
type testComponent struct {
	name string
}

func (tc *testComponent) Init() error {
	return nil
}

func (tc *testComponent) Start() error {
	return nil
}

func (tc *testComponent) Stop() error {
	return nil
}

func (tc *testComponent) GetName() string {
	return tc.name
}

func (tc *testComponent) GetStatus() interface{} {
	return map[string]string{"name": tc.name}
}

func TestConfigValidation(t *testing.T) {
	cfg := config.DefaultIPv6Config()

	// 创建配置管理器
	manager := config.NewIPv6ConfigManager("")
	manager.SetConfig(cfg)

	// 验证默认配置
	err := manager.Validate()
	if err != nil {
		t.Errorf("Default config validation failed: %v", err)
	}

	// 测试无效模式
	cfg.Mode = "invalid_mode"
	err = manager.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid mode")
	}

	// 恢复有效模式
	cfg.Mode = "disabled"
	cfg.UDPEnabled = true
	cfg.UDPPortStart = 10000
	cfg.UDPPortEnd = 9999 // 无效的范围

	err = manager.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid UDP port range")
	}
}

func TestProbeResultToConfig(t *testing.T) {
	manager := config.NewIPv6ConfigManager("")

	// 测试优秀的探测结果
	goodResult := &config.ProbeResult{
		IPv6Supported:   true,
		GlobalAddresses: []string{"2409:8a20:441:2371:2431:54af:ece2:7"},
		Score:           90,
	}

	config := manager.GenerateFromProbeResult(goodResult)
	if !config.Enabled {
		t.Error("Expected enabled for good probe result")
	}
	if config.Mode != "preferred" {
		t.Errorf("Expected mode 'preferred', got %s", config.Mode)
	}
	if !config.UDPEnabled {
		t.Error("Expected UDP enabled for good probe result")
	}
	if config.Encryption != "noise" {
		t.Errorf("Expected encryption 'noise', got %s", config.Encryption)
	}

	// 测试一般的探测结果
	averageResult := &config.ProbeResult{
		IPv6Supported:   true,
		GlobalAddresses: []string{"2409:8a20:441:2371:2431:54af:ece2:7"},
		Score:           70,
	}

	config = manager.GenerateFromProbeResult(averageResult)
	if !config.Enabled {
		t.Error("Expected enabled for average probe result")
	}
	if config.Mode != "fallback" {
		t.Errorf("Expected mode 'fallback', got %s", config.Mode)
	}
	if config.EnableAuth {
		t.Error("Expected auth disabled for average probe result")
	}
	if config.DHTEnabled {
		t.Error("Expected DHT disabled for average probe result")
	}

	// 测试较差的探测结果
	poorResult := &config.ProbeResult{
		IPv6Supported:   false,
		GlobalAddresses: []string{},
		Score:           30,
	}

	config = manager.GenerateFromProbeResult(poorResult)
	if config.Enabled {
		t.Error("Expected disabled for poor probe result")
	}
	if config.Mode != "disabled" {
		t.Errorf("Expected mode 'disabled', got %s", config.Mode)
	}
}