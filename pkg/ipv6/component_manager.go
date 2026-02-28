package ipv6

import (
	"sync"
	"time"
)

// EventBus 简单的事件总线实现
type EventBus struct {
	mu       sync.RWMutex
	subscribers map[chan *Event]bool
	bufferSize  int
}

// NewEventBus 创建事件总线
func NewEventBus(bufferSize int) *EventBus {
	return &EventBus{
		subscribers: make(map[chan *Event]bool),
		bufferSize:  bufferSize,
	}
}

// Subscribe 订阅事件
func (eb *EventBus) Subscribe() <-chan *Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *Event, eb.bufferSize)
	eb.subscribers[ch] = true
	return ch
}

// Unsubscribe 取消订阅
func (eb *EventBus) Unsubscribe(ch <-chan *Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	delete(eb.subscribers, ch.(chan *Event))
}

// Publish 发布事件
func (eb *EventBus) Publish(event *Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	event.Time = time.Now()

	for ch := range eb.subscribers {
		select {
		case ch <- event:
			// 成功发送
		default:
			// 通道已满，丢弃事件
		}
	}
}

// PublishWithType 使用类型发布事件
func (eb *EventBus) PublishWithType(eventType EventType, source, message string, data interface{}) {
	eb.Publish(&Event{
		Type:    eventType,
		Source:  source,
		Message: message,
		Data:    data,
		Time:    time.Now(),
	})
}

// Close 关闭事件总线
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for ch := range eb.subscribers {
		close(ch)
		delete(eb.subscribers, ch)
	}
}

// SubscriberCount 获取订阅者数量
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return len(eb.subscribers)
}

// ComponentManager 组件管理器
type ComponentManager struct {
	mu         sync.RWMutex
	components map[string]Component
	eventBus   *EventBus
}

// Component 组件接口
type Component interface {
	Init() error
	Start() error
	Stop() error
	GetName() string
	GetStatus() interface{}
}

// NewComponentManager 创建组件管理器
func NewComponentManager() *ComponentManager {
	return &ComponentManager{
		components: make(map[string]Component),
		eventBus:   NewEventBus(100),
	}
}

// RegisterComponent 注册组件
func (cm *ComponentManager) RegisterComponent(name string, component Component) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.components[name]; exists {
		return ComponentError{Name: name, Reason: "component already registered"}
	}

	cm.components[name] = component
	cm.eventBus.PublishWithType(EventPeerConnected, "ComponentManager",
		"Component registered: "+name, map[string]string{"name": name})

	return nil
}

// GetComponent 获取组件
func (cm *ComponentManager) GetComponent(name string) (Component, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	component, exists := cm.components[name]
	return component, exists
}

// StartAll 启动所有组件
func (cm *ComponentManager) StartAll() error {
	cm.mu.RLock()
	components := make([]Component, 0, len(cm.components))
	for _, comp := range cm.components {
		components = append(components, comp)
	}
	cm.mu.RUnlock()

	var firstErr error
	for _, comp := range components {
		if err := comp.Start(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			cm.eventBus.PublishWithType(EventConnectionFailed, "ComponentManager",
				"Failed to start component: "+comp.GetName(), err)
		} else {
			cm.eventBus.PublishWithType(EventStarted, "ComponentManager",
				"Component started: "+comp.GetName(), nil)
		}
	}

	return firstErr
}

// StopAll 停止所有组件
func (cm *ComponentManager) StopAll() error {
	cm.mu.RLock()
	components := make([]Component, 0, len(cm.components))
	for _, comp := range cm.components {
		components = append(components, comp)
	}
	cm.mu.RUnlock()

	var firstErr error
	for _, comp := range components {
		if err := comp.Stop(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			cm.eventBus.PublishWithType(EventConnectionFailed, "ComponentManager",
				"Failed to stop component: "+comp.GetName(), err)
		} else {
			cm.eventBus.PublishWithType(EventStopped, "ComponentManager",
				"Component stopped: "+comp.GetName(), nil)
		}
	}

	return firstErr
}

// GetEventBus 获取事件总线
func (cm *ComponentManager) GetEventBus() *EventBus {
	return cm.eventBus
}

// GetComponentNames 获取所有组件名称
func (cm *ComponentManager) GetComponentNames() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	names := make([]string, 0, len(cm.components))
	for name := range cm.components {
		names = append(names, name)
	}
	return names
}

// ComponentError 组件错误
type ComponentError struct {
	Name   string
	Reason string
}

func (ce ComponentError) Error() string {
	return "component " + ce.Name + ": " + ce.Reason
}