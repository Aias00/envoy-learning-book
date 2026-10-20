# 第21章 可观测性：把“打不开”变成可定位的问题

先修：第18章的请求路径和第19章路由。本章学习目标是分别观察Kubernetes状态、控制器日志、Envoy访问日志与指标，知道追踪能补充什么信息，并用一次404和一次429练习证据推理。可观测性不是先装满仪表盘，而是能回答：期望是否被接受、配置有没有形成、请求在哪里结束、哪个依赖变慢。

## 21.1 两个平面需要不同证据

Envoy Gateway控制面负责观察资源、翻译配置和管理基础设施；Envoy数据面负责连接和HTTP处理。控制器没有报错，不代表后端健康；业务请求返回200，也不证明所有Gateway的最新配置都已发布。要把证据对应到正确对象。

先用kubectl看资源status与事件，再用控制器日志看reconcile/translation是否失败，随后查看Envoy的访问日志和指标，最后用真实请求确认用户可见行为。它们是互补关系，不是一份日志能包办全部事实。

```bash
kubectl --context kind-envoy-book -n envoy-gateway-system logs \
  deployment/envoy-gateway --tail=100
kubectl --context kind-envoy-book -n envoy-book get gateway,httproute -o yaml
kubectl --context kind-envoy-book -n envoy-book get events --sort-by=.metadata.creationTimestamp
```

事件是有保留期限的运行记录，不应被当作长期审计数据库。控制器日志也可能被轮转；排障时记录时间、资源namespace/name、generation和测试请求，才能在多处数据之间对齐。

## 21.2 从一条访问日志读出请求路径

默认Envoy访问日志写到stdout，包含状态码、flags、detail、route、upstream_host等字段。使用归属label找对数据面，不要误看控制器日志：

```bash
kubectl --context kind-envoy-book -n envoy-gateway-system logs \
  -l gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book \
  -c envoy --tail=30
```

响应码描述结果：200通常是应用成功响应；404可能是网关没有路由，也可能是应用主动返回不存在；503可能涉及无可用上游或连接故障；429可能由网关或应用限流。只看状态码无法确定责任层，因此还要看response_code_details、response_flags和upstream_host。

例如向unknown.example发送请求，如果日志显示没有匹配路由，且没有上游主机，说明请求在网关路由阶段结束。向book.example请求一个应用不存在的路径，即使也返回404，若日志有上游地址并显示经由上游返回，就属于不同故障。不要把所有404都归给HTTPRoute，也不要把所有503都归给应用代码。

flags是Envoy对异常类别的缩写，detail进一步说明原因；具体值必须对照使用版本的Envoy文档。duration通常是请求处理总体耗时，不能简单当成后端CPU执行时间；它可能包括排队、连接、传输、重试和客户端接收等影响。upstream_host表明本次选择的地址，不是整个Service的完整端点列表。

## 21.3 用EnvoyProxy缩小日志格式

本书提供 [observability.yaml](../../labs/03-envoy-gateway/observability.yaml)，将method、path、status、flags、detail、route、upstream、request_id和duration_ms写为JSON。EnvoyProxy是实现扩展资源，创建它以后还须明确挂到Gateway或GatewayClass，否则它不会自动影响所有代理。

```bash
kubectl --context kind-envoy-book apply -f labs/03-envoy-gateway/observability.yaml
kubectl --context kind-envoy-book -n envoy-book patch gateway book --type=merge \
  -p '{"spec":{"infrastructure":{"parametersRef":{"group":"gateway.envoyproxy.io","kind":"EnvoyProxy","name":"book-observability"}}}}'
```

本例的EnvoyProxy与Gateway同Namespace，所以使用Gateway infrastructure.parametersRef。本地日志sink指向/dev/stdout，便于kubectl logs。修改后重新请求并观察新日志，避免把变更前的旧行当成新配置证据。若Gateway已有parametersRef，应先合并所需配置，不能无意覆盖别的基础设施设置；本实验初始配置没有该引用。

格式清晰还不够，日志会消耗存储与I/O。不要默认记录Authorization、Cookie、完整token或所有请求体。path可能含用户标识与查询参数，应根据业务做脱敏、保留期限与访问控制。request_id可帮助关联一次请求，但外部客户端可以带入请求头，不能把任意request_id当成可信安全身份。

## 21.4 指标适合看趋势，不替代单请求证据

指标将许多事件聚合成计数、当前值和分布。Counter适合表示累计请求数，判断每秒速率应计算时间窗口内增量；Gauge适合表示当前连接或队列数量；Histogram适合记录延迟分布。累计请求数在进程重启后可能重置，不能直接用两个任意时刻的原始值推断可靠速率。

在实验中找到book对应Envoy Pod，转发其19001端口，命令见实验README，然后读取：

```bash
curl --noproxy '*' -fsS http://127.0.0.1:19001/stats/prometheus
```

此处19001是Envoy Gateway生成的统计listener；真实Envoy Admin默认绑定Pod loopback的19000。二者不要混淆，更不要把完整Admin接口暴露公网。控制器本身的Prometheus端点则是控制器Pod上的 `/metrics`；即使也使用19001，仍是另一个Pod、另一个进程。采集目标选错，路径和指标含义都会不同。

生产先关注请求速率、错误率、尾部延迟、活动连接、上游连接失败与控制面配置发布异常。平均延迟很低并不排除少部分请求极慢；P95/P99比平均值更能提示尾部体验，但分位数也需要足够样本与合适直方图桶。不要把每个用户ID或完整URL作为指标label，基数爆炸会放大内存与存储成本。

## 21.5 追踪展示分段时间，但需要传播

分布式追踪把一次请求跨服务的调用表示为span。Envoy产生网关span，应用继续传播trace context并记录自己的span，才能连接完整链路。只启用网关采样，并不会自动看见数据库内部执行时间；只生成x-request-id也不等于建立了一条分布式trace。

追踪通常要配置collector地址、协议、采样率与传输安全，具体字段读v1.9.2的EnvoyProxy tracing类型。低采样可能错过罕见故障，100%采样则增加数据量和成本；采样策略需要与错误保留、日志关联一起设计。本书最小实验不部署完整遥测栈，避免把监控依赖的故障与网关核心路径混在第一次学习中。

## 21.6 给告警一个明确的用户含义

“某个计数器变大”不一定意味着用户受损。例如未知Host得到404是本书预期边界，超额流量得到429也可能表示保护正常工作。更有效的告警描述应包含对象、时间窗口、阈值和影响，例如某业务入口在连续窗口内的有效请求错误率超过目标，同时成功请求速率下降。阈值由业务目标与历史流量决定，不能把教材中的数字当作通用生产标准。

关联数据时还要统一时间。客户端看到超时的时间、代理完成日志的时间和后端记录的时间可能不同；主机时钟偏差会进一步扰乱排序。记录时区和请求ID，把同一次请求的证据放在同一时间范围，再判断是入口处理慢、上游建连慢还是应用处理慢。仅凭两行先后打印的日志，不足以证明分布式因果顺序。

观察系统本身也有故障边界。日志采集器阻塞、collector不可达或Prometheus采集失败，都不自动等于业务转发失败，但可能让故障无法及时解释。检查告警时应区分“采集不到数据”和“指标为零”，并保留本地日志等替代诊断入口。遥测设计应服务业务可靠性，同时控制资源消耗，避免过量记录反过来挤占代理处理能力。

## 21.7 练习：用证据区分故障

先请求unknown.example，再请求limited.example直到429。分别记录时间、状态码、route、upstream和detail。问题一：为什么前者不能证明后端宕机？答案：请求可能根本没选出后端，404来自网关匹配。问题二：429一定需要修改HTTPRoute吗？答案：不一定，可能是BackendTrafficPolicy按预期拒绝，先读限流策略与日志。

问题三：控制器日志安静，业务却503，应怎样查？答案：检查Route引用状态与最新generation，再检查Service端口、EndpointSlice Ready地址、Envoy上游连接证据和应用监听。控制器不持续打印错误，只表示没有这类日志证据。

问题四：指标显示错误率升高，如何找具体请求？答案：按时间、入口、Route或上游维度缩小范围，再查对应访问日志及采样trace。指标适合发现与定位范围，日志和追踪适合解释单次路径。

## 本章依据

- [v1.9.2 访问日志任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/observability/proxy-accesslog.md)与[日志类型定义](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/envoyproxy_accesslogging_types.go)。
- [v1.9.2 代理指标任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/observability/proxy-metric.md)与[控制面指标任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/observability/gateway-observability.md)。
- [v1.9.2 bootstrap实现](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/xds/bootstrap/bootstrap.go)：Admin与stats端口。
- [v1.9.2 tracing类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/envoyproxy_tracing_types.go)。

[上一章](20-tls-and-security.md) · [下一章](22-source-tour.md)
