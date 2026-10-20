# 附录 A　中英术语与常见混淆

第一次见到术语时先建立大意，遇到具体问题再回到相应章节。下面的“区别”比记住缩写更重要。

| 术语 | 中文与含义 | 易混淆点 / 阅读位置 |
| --- | --- | --- |
| Envoy Proxy | 执行业务转发的代理进程 | 与管理它的 Envoy Gateway 分开；03 |
| control plane | 控制平面，决定并分发配置 | 通常不承载逐请求业务转发；07 |
| data plane | 数据平面，执行请求处理 | 有进程不等于每条路由健康；03、24 |
| downstream | 向当前代理发起连接的一侧 | 是相对方向，不恒指外网；02 |
| upstream | 当前代理要连接的目标一侧 | 一段调用中可再有下一台代理；02 |
| Listener | 接收下游连接的资源 | name 与监听端口不同；04 |
| filter chain | Listener 上的过滤器链 | 网络过滤器与 HTTP 过滤器层次不同；04 |
| HCM | HTTP connection manager | 网络过滤器，内部处理 HTTP 流与过滤器；05 |
| VirtualHost | 根据 Host/authority 匹配网站 | 与 DNS 解析、TLS SNI 各有职责；01、05 |
| Route | 请求匹配与处理规则 | 可以转发、重定向或直接响应；04、19 |
| Cluster | 一组上游及连接/发现配置 | 不是 Kubernetes cluster；04 |
| Endpoint | 可连接的具体地址与端口 | Service 对象并不保证存在 Ready 端点；16 |
| ClusterLoadAssignment | Envoy 端点集合消息 | EDS 交付的资源类型；10 |
| xDS | Envoy 发现服务族的统称 | x 代表不同资源，不是一个资源名；08 |
| LDS / RDS | Listener / Route Discovery Service | HCM 可内嵌路由也可引用 RDS；04、08 |
| CDS / EDS | Cluster / Endpoint Discovery Service | Cluster 存在仍可能无可用端点；08 |
| SDS | Secret Discovery Service | 分发证书相关配置，不是密码管理系统全体；08、20 |
| ADS | Aggregated Discovery Service | 一条流复用多种发现服务，不是全局事务；08 |
| SotW | State of the World | 针对某类型与订阅范围的完整状态，不是全代理状态；08 |
| Delta xDS | 按资源变化增删的协议模式 | 版本和订阅细节不等于 SotW；08 |
| ACK / NACK | 接受 / 拒绝某次协议更新 | ACK 不能证明后端可达；08、13 |
| version_info | 配置集合的版本标识 | 不要求是可排序数字；08、11 |
| nonce | 关联响应与反馈的标识 | 不是业务发布版本号；08 |
| Node.ID | 代理提供的节点标识 | 标识不等于经认证的身份；11、15 |
| SnapshotCache | 按配置组保存快照并提供 watch 的缓存 | SetSnapshot 不自动调用 Consistent；11 |
| Consistent() | 检查部分资源引用集合的方法 | 不是所有字段和业务语义的全面校验；11 |
| Any | 封装有类型 Protobuf 消息的容器 | 包含 type URL 与序列化值；09 |
| gRPC stream | 双向或单向的持续消息流 | 一个流能承载多个发现响应；12 |
| bootstrap | Envoy 启动配置 | 至少让代理知道怎样连接控制平面；12 |
| warming | 等待依赖/初始化的资源状态 | 搜到资源名字不代表 active；13、24 |
| reconcile | 读取并协调期望与实际状态 | 事件可能重复，需幂等；07、22 |
| IR | intermediate representation，中间表示 | 解耦平台输入与 xDS 输出；22 |
| GatewayClass | 标识某类 Gateway 的控制器与参数 | 集群作用域，不是转发进程；17 |
| Gateway | 一组入口 listener 与接入规则 | namespace 作用域，不等于一台固定 Pod；17 |
| HTTPRoute | HTTP 匹配与转发意图 | 还需被父 Gateway 接纳并解析引用；17 |
| parentRefs | Route 指向父资源的引用 | 引用不等于无条件接入成功；17 |
| allowedRoutes | Gateway 允许哪些 Route 接入 | 与引用后端的 ReferenceGrant 分开；17 |
| ReferenceGrant | 目标 namespace 授予跨空间引用许可 | 是目标方授权，不是源方自我声明；17、20 |
| Accepted | 是否接受/接入的条件 | 单独不能证明所有依赖或流量正常；18 |
| ResolvedRefs | 引用是否成功解析的条件 | 看正确 parent 与 observedGeneration；18 |
| Programmed | 配置是否已送达相关实现的状态条件 | 仍需要真实请求验证；18 |
| Service / EndpointSlice | Kubernetes 稳定服务抽象与端点信息 | Service.port、targetPort、Pod端口有区别；16 |
| TLS termination | 代理解密下游 TLS，再处理 HTTP | 不会自动建立上游 TLS；20 |
| SNI | TLS 握手里的目标服务器名字 | 与握手后 HTTP Host 不同；20 |
| mTLS | 双方都提供并验证证书的 TLS | 不等于最终用户身份授权；20 |
| local rate limit | 单代理/局部作用域的限流 | 多副本不自动共享一个全局计数；19 |
| retry budget | 限制重试的资源/次数预算 | 重试仍要考虑幂等和总超时；01、23 |
| observability | 从日志、指标、追踪解释运行状态 | 控制器指标与代理指标不同；21 |

若遇到“版本都是 v3，为什么不能混用所有镜像”：v3 是 API 主版本范畴，实际字段、扩展与二进制兼容范围仍随具体版本变化。使用固定版本 API 说明与兼容矩阵。
