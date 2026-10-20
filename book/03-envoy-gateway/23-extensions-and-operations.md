# 第23章 扩展与运维：从实验走向可维护的入口

先修：第19章策略、第20章安全、第22章翻译流水线。学习目标是判断需求应该放在标准API、实现扩展还是自定义xDS扩展，理解升级和故障边界，并形成一份能演练的网关变更过程。本章不把本地kind成功等同于生产就绪；实验提供核心机制，生产还必须结合流量、组织权限和故障要求设计。

## 23.1 按需求选择最窄的扩展层

首先判断标准Gateway API能否表达需求：HTTPRoute的匹配、权重、header修改、重定向等已经覆盖许多日常场景。标准API便于不同实现共享概念，但实现支持级别仍要逐项核对，不能只看字段在规范里存在。

若需要Envoy Gateway特有的连接、限流、身份或遥测能力，优先使用ClientTrafficPolicy、BackendTrafficPolicy、SecurityPolicy、EnvoyProxy等有明确类型和status的扩展。EnvoyProxy主要描述代理运行环境与遥测等，Policy附着特定资源，HTTPRouteFilter通过规则的ExtensionRef提供扩展行为。选择适合的作用域，可降低一项应用需求意外影响整个Gateway的概率。

再往下才是EnvoyPatchPolicy与Extension Server。前者用JSON Patch修改生成的xDS，后者通过gRPC hook在发送给Envoy前修改资源。它们进入第22章翻译后段，对底层资源有更强控制力，同时与实现内部资源名称、结构和执行顺序形成更紧耦合。

## 23.2 为什么不把Patch当成普通配置捷径

v1.9.2官方文档明确说EnvoyPatchPolicy默认关闭，API不稳定，适合非常熟悉xDS语义的用户。需要启用extensionApis.enableEnvoyPatchPolicy，并且必须保留既有EnvoyGateway启动配置的其他字段，不能为了一个开关覆盖整份ConfigMap。本书基础实验不启用它，因为我们的学习目标都能通过受支持资源完成。

一个Patch可能依赖生成的listener/route名称。升级后名称规则变了，Patch即使结构仍合法，也可能找不到目标或改变错误对象。补丁还可以影响认证filter、网络访问与Secret相关配置，因此写Patch的权限应视作强管理权限。原生HTTPRoute编辑权限不应顺手扩大为任意xDS修改权限。

对确实需要Patch的需求，先保存目标版本生成配置，定义修改前后的可检验差异，再测试正常请求、拒绝路径、上游失败和升级。不要仅以“Envoy没有崩溃”作为验证标准。应核对最终配置与访问行为，且避免在调试记录或PR中泄露配置里的证书、密钥和token。

## 23.3 Extension Server位于控制路径

Extension Server可以接到Route、Cluster、VirtualHost、HTTPListener及整体Translation等hook，范围依配置而定。比如PostRouteModifyHook能处理受扩展目标影响的route，PostTranslateModifyHook可处理最终资源集合。它不是每次HTTP请求必经的外部认证服务；控制面翻译时的hook和数据面按请求调用的ext_authz，生命周期与故障影响完全不同。

官方v1.9.2扩展文档还特别指出，自定义扩展Policy挂Gateway与挂Route时，Envoy Gateway不会替扩展作者自动执行与原生BackendTrafficPolicy相同的覆盖、合并和冲突语义。扩展服务必须定义自己的组合规则。读到targetRefs相同字段名，不能推断所有Policy类型都拥有同一套继承行为。

扩展服务改变了配置供应链，部署权限、连接认证、超时、可用性、版本与生成配置兼容性都要纳入运维。能够注入xDS的扩展属于受信任管理组件，其输入不可直接当作普通租户不受约束的自由文本。把扩展运行在另一个Pod，并不会自动给它输出的Envoy配置提供沙箱隔离。

## 23.4 控制器副本与代理副本是两件事

增加Envoy Gateway副本，提高的是控制面可用性和连接服务能力；增加Envoy数据面副本，影响的是业务处理容量、流量分布与故障承受。修改Helm deployment.replicas不能自动等同于增加每个Gateway的Envoy副本。控制面和数据面还可能有不同的拓扑、资源限制、PodDisruptionBudget与扩容策略。

控制面短暂不可用时，已有Envoy通常能继续使用已接受的配置服务现有业务，但新路由、端点和证书变更的传播会受影响；具体行为还取决于资源TTL、连接、进程是否重启等条件。不能据此承诺“控制面可以一直停掉”。同样，控制面恢复后还要确认重新同步，而不是只看Pod Running。

容量规划应同时考虑业务请求率、连接数、TLS握手、header/body大小、遥测开销与配置规模。一个每秒100次请求的大文件下载站与100次小JSON API，对连接、内存与带宽的需求不同。扩容前也要复查本地限流：每实例独立的额度可能随副本变化，不能用它承担稳定的全局计费语义。

## 23.5 把升级拆成可回退的检查点

固定版本只保证教材可复现，不表示该版本永久适合生产。升级前读取目标版本release notes、兼容矩阵与迁移说明，比较Gateway API版本、CRD served/storage版本、废弃字段、Envoy版本和扩展兼容性。v1.9.2安装文档特别提醒Gateway API v1.6相关CRD升级次序，不能只升级控制器镜像而忽略API变更。

Helm对CRD的处理与普通模板资源不同，`helm upgrade`不应被假设自动安全完成所有CRD迁移。先按固定版本官方步骤处理CRD并验证现有对象，再升级控制器，最后观察数据面与业务。保存配置与对象备份时要控制Secret访问，回退计划也应明确是否涉及不可逆schema或存储版本变化。

一个可演练过程包括：在隔离集群使用相同版本组合复现；对清单做server dry-run；应用候选变更；检查generation对应状态；检查配置发布与Envoy接收情况；发送成功、拒绝和故障路径请求；最后观察一段符合业务流量的窗口。每一步应有具体通过条件，例如“未授权请求持续401/403”比“认证看起来正常”更可检验。

## 23.6 故障时先收集什么

发生故障先记录时间、影响域名/路径、请求ID、状态码与是否所有客户端都受影响。随后检查GatewayClass/Gateway/Route/Policy的状态、控制器和数据面Pod、Service/EndpointSlice、访问日志与最近变更。不要第一步删除全部资源重装，这会丢失现场并引入新变量。

变更回退要针对具体资源与版本，不应在不清楚字段所有权时直接覆盖控制器生成的对象。若暂停一次路由发布，需要明确暂停的是GitOps同步、应用变更还是整个控制器，各自影响不同。Admin与指标端点只给受控运维访问；生产排障复制config_dump时应检查敏感数据。

本书清理脚本只删除独立kind集群envoy-book，适合重置教学环境。生产环境不能照抄这种整体重置方式。生产删除Gateway前，还要考虑外部地址、DNS、客户端连接和关联路由，不应把对象删除完成当成流量迁移完成。

## 23.7 综合练习与参考答案

练习一：要给某一API添加响应头，是否应该启用Patch？答案：先使用标准HTTPRoute ResponseHeaderModifier，范围明确、验证容易，已有能力无需下沉。

练习二：增加控制器副本后，后端吞吐没变，为什么？答案：控制器负责配置管理，业务走Envoy数据面；还需测量真正瓶颈，调整数据面、后端或网络，而不是混淆两种副本。

练习三：升级后Route Accepted=True，但扩展认证失效，下一步？答案：核对扩展策略附着、生成filter与顺序、xDS发布/接收、扩展服务日志，再发送未授权反例请求。只验证成功请求会漏掉“所有人都被放行”的危险回归。

练习四：完成全书后应能怎样解释一次请求？参考答案：客户端找到入口并建立连接，Envoy根据listener、域名、路径与策略处理请求，选出由Service/EndpointSlice推导的后端；控制器在旁边通过provider、translator、IR、基础设施管理和xDS持续维护这套状态。请求数据路径与配置管理路径分别有证据，排障就有了方向。

## 本章依据

- [v1.9.2 EnvoyPatchPolicy官方任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/extensibility/envoy-patch-policy.md)：默认关闭、配置命名兼容与权限边界。
- [v1.9.2 Extension Server任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/extensibility/extension-server.md)：hook与自定义策略组合语义。
- [v1.9.2 Helm安装/升级说明](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/install/install-helm.md)。
- [v1.9.2 EnvoyProxy类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/envoyproxy_types.go)与[基础设施runner](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/infrastructure/runner/runner.go)。

[上一章](22-source-tour.md) · [下一章：系统排障](../04-practice/24-troubleshooting.md) · [实验汇总](../../labs/03-envoy-gateway/README.md)
