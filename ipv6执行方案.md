## 1. 项目背景与目标

在现有的 P2P 架构基础上，增加 **IPv6 优先模式**。利用 IPv6 海量的全局单播地址（GUA），绕过复杂的 NAT 穿透流程，实现设备间的端到端直连，提升连接成功率并降低通信延迟。

---

## 2. 技术架构设计

### 2.1 网络层协议

- **传输层**：优先使用 **UDP** 进行打洞和数据传输，辅以 **TCP** 作为大文件传输的备选。
    
- **地址协议**：支持 IPv6 全局单播地址（2000::/3）。
    
- **双栈策略**：采用 Happy Eyeballs 算法（类似 RFC 8305），同时发起 IPv4 和 IPv6 连接尝试，优先保留 IPv6 链路。
    

### 2.2 核心组件

|**组件名称**|**职责描述**|
|---|---|
|**Address Manager**|动态获取本地 IPv6 GUA 及其变动监听（处理隐私扩展导致的 IP 变更）。|
|**Identity Service**|生成基于 Ed25519 的 Peer ID，作为跨 IP 变更的唯一身份标识。|
|**Discovery DHT**|构建基于 Kademlia 算法的 IPv6 专用 DHT 网络，存储 `{PeerID: IPv6_Address}`。|
|**Hole Punching**|针对 IPv6 状态防火墙进行 UDP 打洞，确保持续的入站权限。|

---

## 3. 详细执行流程

### 第一阶段：环境探测与地址绑定

1. **网卡枚举**：通过 Go 的 `net.Interfaces()` 检索带有 `FlagUp` 且非回环的 IPv6 地址。
    
2. **有效性验证**：过滤掉链路本地地址（fe80::/10），仅保留全局单播地址。
    
3. **端口监听**：在 `[::]:port` 上开启监听，准备处理入站连接。
    

### 第二阶段：节点发现（Discovery）

1. **静态引导**：连接至预设的 IPv6 引导节点（Bootstrap Nodes）。
    
2. **DHT 公告**：将自己的 `PeerID` 和当前 `IPv6` 地址广播至 DHT 网络。
    
3. **寻址查询**：当需要联系目标用户时，通过其 `PeerID` 在 DHT 中检索最新的 IPv6 映射。
    

### 第三阶段：连接建立与安全

1. **直接握手**：直接向目标的 IPv6 地址发起加密握手。
    
2. **加密协议**：使用 **Noise Protocol Framework** 或 **TLS 1.3** 建立端到端加密通道。
    
3. **心跳维持**：每 30-60 秒发送一次空包（Keep-alive），防止防火墙丢弃该 UDP 映射条目。
    

---

## 4. 关键代码逻辑实现（Go 伪代码）

Go

```
// 获取本地全局 IPv6 地址
func GetGlobalIPv6() ([]net.IP, error) {
    addrs, _ := net.InterfaceAddrs()
    var globalIPs []net.IP
    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() == nil && isGlobalUnicast(ipnet.IP) {
                globalIPs = append(globalIPs)
            }
        }
    }
    return globalIPs, nil
}

// 建立连接逻辑
func DialPeer(targetIPv6 string) {
    // 优先尝试 IPv6 UDP 直连
    conn, err := net.Dial("udp6", "["+targetIPv6+"]:port")
    if err != nil {
        // 触发回退逻辑：尝试 IPv4 或 Relay
    }
}
```

---

## 5. 风险评估与对策

|**风险点**|**影响**|**对策**|
|---|---|---|
|**隐私 IP 频繁更换**|导致连接中断|监听网卡信号，一旦 IP 变更，立即更新 DHT 记录并通知活跃 Peer。|
|**防火墙拦截入站**|无法被动接入|强制实施双向 UDP 打洞逻辑；如果失败，则请求中继节点。|
|**网络环境不支持**|彻底无法连接|实现 IPv4 隧道（6to4）或传统的 TURN 中继作为保底。|

---

## 6. 测试与验收标准

- **指标 1**：在移动 5G 网络环境下，两台物理设备在无中继器情况下实现 100% 直连。
    
- **指标 2**：在 IP 地址发生隐私变换后，DHT 映射更新时间延迟小于 5 秒。
    
- **指标 3**：连接握手延迟（Handshake Latency）在同城环境下低于 50ms。

## 先编写一个简单的 **IPv6 连通性探测工具**