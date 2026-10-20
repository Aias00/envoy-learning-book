# 第19章 路由与策略：先选请求，再决定怎样处理

先修：第17章引用关系与第18章成功转发。本章目标是正确组合 hostname、path、header，知道多个匹配的关系，使用真实 v1.9.2 策略字段，并解释策略冲突。路由解决“发往哪里”，策略解决“满足什么处理条件、怎样限制或保护流量”，两者会共同进入最终 Envoy 配置。

## 19.1 一条请求如何被选择

客户端连接 listener 后，HTTPRoute 的 hostnames 先限制站点范围。规则内的 matches 再匹配 path、header、method、query等属性。一个 matches 元素内部的多个条件通常是“并且”；多个 matches 元素表达可替代的匹配，即“或者”。不要把数组里每一项误读成必须同时满足。

本书 [routing.yaml](../../labs/03-envoy-gateway/routing.yaml) 为 routing.example 定义两条规则。第一条同时要求 PathPrefix `/api` 与请求头 `x-book-mode: debug`，响应中添加 `x-book-route: debug-api`；第二条只要求 `/api`。这样普通请求进入普通规则，带调试头的请求进入更具体规则。该头只用于教学，不应当成身份凭据，任何客户端都能伪造它。

PathPrefix 按路径段匹配。`/api` 可匹配 `/api`、`/api/`、`/api/users`，不应匹配 `/apix`。Exact 则要求精确路径；正则匹配属于实现能力和约束更强的领域，零基础阶段先用 Exact 与 PathPrefix。请求 path 与查询串也是不同部分，`/api?debug=true` 不代表路径本身叫 `/api?debug=true`。

```bash
kubectl --context kind-envoy-book apply -f labs/03-envoy-gateway/routing.yaml
curl --noproxy '*' -i -H 'Host: routing.example' -H 'x-book-mode: debug' \
  http://127.0.0.1:18888/api/users
```

手工 port-forward 须按第18章先启动。观察响应头比只观察200更有意义，因为两条规则都指向同一后端，200本身无法告诉你究竟选中了哪一条。

## 19.2 最具体的规则与后端权重

多条规则可以同时符合请求。Gateway API 定义路径类型、路径长度以及其他匹配条件的优先关系，实现还要按规范处理并列情况。因此不能把“YAML里第一条”当成所有场景的完整排序规则。Exact 相对匹配同一路径的 PathPrefix 更具体，更长的匹配前缀通常优先；需要复杂合并时，应结合该版本HTTPRoute规范和真实测试确认。

一条规则还可以有多个 backendRefs，通过 weight 表达相对比例。两个后端权重90和10的意思是按实现的负载分配机制，长期期望约90%与10%；不是每十次请求必须精确九次和一次。连接复用、样本量、会话保持及并发都会影响短窗口观察。实验先用一个 Service，避免把“多个Pod之间负载均衡”与“多个backendRef的流量切分”混成同一机制。

backendRef 必须解析到存在的对象和正确服务端口。一个无效后端不应被解释成自动安全回退。Gateway API 对无效引用及其权重对应流量有规定；若你需要故障转移，应该明确配置并测试该功能，而不是依赖拼错名字后“恰好还通”的现象。

## 19.3 Filter 修改当前请求，Policy 附着目标对象

HTTPRoute 的 filters 直接写在规则里。本书用标准 ResponseHeaderModifier 给响应加标记。RequestHeaderModifier 修改发往后端的头；URLRewrite 改写上游看到的URL；RequestRedirect 让客户端收到重定向并发起新请求。改写与重定向差别很大：前者是代理内部行为，后者会把状态码和Location交给客户端。不要在不了解端到端行为时把二者互换。

Envoy Gateway 的扩展Policy是独立资源。BackendTrafficPolicy管理限流、连接与韧性等；ClientTrafficPolicy面向客户端连接和入口行为；SecurityPolicy管理认证、授权及相关安全功能。名字中的Backend并不意味着该Policy只能target Service，本书的BackendTrafficPolicy明确target一个HTTPRoute。读它的targetRefs，才能知道附着点。

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: BackendTrafficPolicy
metadata:
  name: limited
  namespace: envoy-book
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: limited
  rateLimit:
    local:
      rules:
        - limit:
            requests: 3
            unit: Hour
```

这是从 v1.9.2 `ratelimit_types.go` 与实际CRD核对的写法。部分概念页还保留 `local.requests` 的旧示例，不能据此忽略schema。用 `kubectl apply --dry-run=server -f ...` 让已安装该版本CRD的API Server先校验结构，再检查控制器status判断语义。

## 19.4 本地限流为什么不是全局配额

应用 [ratelimit.yaml](../../labs/03-envoy-gateway/ratelimit.yaml) 后，请求 limited.example。单个Envoy实例上的对应路由使用自己的限流计数/令牌桶，耗尽后返回429。教材选择3次每小时，是为了在交互过程中明显看见拒绝，而不是推荐生产配额。

“本地”的边界是Envoy实例，且策略挂Gateway时，相关路由通常仍分别有自己的桶。两个Envoy副本并不会自动共享一个3次配额；流量分布也不保证让你恰好观察到总计6次。重启、重新配置及实现状态都可能影响桶。生产若需要跨副本、跨入口统一配额，应评估全局限流服务及它的存储、可用性、超时、故障时放行或拒绝行为。

重试同样需要预算。后端故障时每个请求重试三次，会放大后端负载；涉及扣款等操作还需要应用层幂等设计。入口超时、上游每次尝试超时、重试次数不能各自无限增大。Policy的目的在于形成可解释的整体行为，而不是把所有保护开关都打开。

## 19.5 同一流量遇到多份策略怎么办

对本书直接附着Gateway的场景，BackendTrafficPolicy大体按更具体作用域优先：Route rule、Route、Gateway listener、Gateway。相同层级、相同目标的多个Policy，先比较创建时间，较早者优先；时间相同时再按命名空间/名称排序。创建时间由API Server维护，示例中不要手工伪造creationTimestamp来抢优先权。冲突结果应检查策略的status.ancestors及conditions。

默认不应假设父子Policy会自动把每个字段叠加。v1.9.2支持Route侧用mergeType明确请求合并，并区分StrategicMerge与JSONMerge；数组的合并行为尤其需要阅读定义。若采用ListenerSet，父级路径还会经过ListenerSet及其listener，Gateway自有listener的策略不能直接当成它们的同一个作用域。第23章再讨论这类扩展的操作边界。

本书targetRefs使用本地引用，Policy与目标同Namespace。真实源码中的targetSelectors还支持显式namespace选择及目标侧ReferenceGrant；不能把旧概念文档里的“所有目标必须同Namespace”推广成该tag所有机制的绝对限制。初学者先用targetRefs减少隐式影响范围，团队配置中再按实际需求使用选择器。

## 19.6 常见错误、练习与答案

把header debug当认证，会让客户端伪造访问条件；把PathPrefix当普通字符串前缀，会错误放行/apix；把策略对象创建成功当成策略已经发布，会忽略异步传播；把每个Envoy的限流当成全局计费额度，会在扩容时改变实际配额。逐项构造反例，能更快找到规则边界。

练习一：同一个matches里有path与header，缺少header时能匹配这一项吗？答案：不能；但仍可能匹配其他matches或规则。本书有普通/api回退规则，所以不带头时不必404。

练习二：Gateway有连接上限Policy，Route又有更具体Policy，能假设连接上限自动保留吗？答案：不能。先确认该Policy类型的优先和合并语义，再读最终配置与status，必要时显式mergeType。

练习三：一次429能证明整个公司所有入口共享配额吗？答案：不能。它只证明这个请求在当前路径受到了限制，还需确认本地/全局机制、副本数量及计数作用域。

## 本章依据

- [v1.9.2 HTTP routing任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/traffic/http-routing.md)。
- [v1.9.2 限流类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/ratelimit_types.go)与[Policy目标类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/policy_helpers.go)。
- [v1.9.2 BackendTrafficPolicy翻译](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/gatewayapi/backendtrafficpolicy.go)：作用域处理、合并与冲突。
- [Gateway API v1.6.1 HTTPRoute类型](https://github.com/kubernetes-sigs/gateway-api/blob/v1.6.1/apis/v1/httproute_types.go)：匹配、排序、backendRefs及filter约束。

[上一章](18-first-gateway.md) · [下一章](20-tls-and-security.md)
