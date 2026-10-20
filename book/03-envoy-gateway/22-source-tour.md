# 第22章 源码导读：从Kubernetes对象走到xDS

先修：前五章的资源、请求与策略。本章不要求能写Go，而要求你能沿真实函数找到一条配置的加工路线，解释provider、Gateway API translator、IR、xDS translator与infrastructure manager的职责。源码固定 [envoyproxy/gateway v1.9.2](https://github.com/envoyproxy/gateway/tree/v1.9.2)，以下路径都相对于这个仓库，不能拿主分支行号代替固定版本证据。

## 22.1 先找阶段，再读细节

大型控制器不能按文件字母顺序从头读完。先追踪输入输出：输入是Gateway API与实现扩展资源，输出分两路，一路是Envoy的动态配置，一路是运行Envoy所需的Kubernetes基础设施。状态还要回写给用户。因此最有用的路线图是：

```text
API Server中的对象
       ↓ Kubernetes provider：观察、筛选、收集依赖
Resources资源集合
       ↓ Gateway API translator：解析引用、校验、策略组合
       ├── Xds IR → xDS translator → 版本化缓存/服务器 → Envoy
       ├── Infra IR → infrastructure manager → Deployment/Service等
       └── 资源状态 → provider状态写回 → API Server
```

这里箭头描述控制流与数据流，不是每次业务请求的路径。Envoy处理一万个HTTP请求，不会要求控制器把同一份HTTPRoute翻译一万次。只有相关资源变化、重试或内部触发时，控制面才重新计算。

## 22.2 provider：把相关对象收集成输入

先打开 `internal/provider/kubernetes/controller.go`，查找 `Reconcile`。该函数读取受管理的GatewayClass，整理Gateway、Route、策略和被引用资源。`managedGatewayClasses` 按controllerName选择由本控制器负责的类；`processBackendRefs` 收集Service及其EndpointSlice；Secret、ConfigMap、ReferenceGrant等也按引用与功能需求加入资源集合。

继续查 `watchResources`，能看见各种Kubernetes对象的watch。Service更新、EndpointSlice变化，未必改变HTTPRoute spec，但仍可能改变最终端点。因此控制器不能只监听HTTPRoute。`processBackendRefs` 中实际存在EndpointSlice列表与索引处理，正是第16章“Service存在不代表端点可用”的源码落点。

Reconcile收到的请求是一次重新观察的机会，不应被想象成永不丢失的业务事件日志。控制器通常依据当前对象重算，而不是依赖某个“先发生A再发生B”的理想顺序。watch缓存、队列、并发和失败重试使最终一致性成为必需设计。

## 22.3 Gateway API translator：把产品语义变成内部表示

打开 `internal/gatewayapi/translator.go` 的 `Translate`。它先构造TranslatorContext，将Namespace、Service、Secret、ConfigMap、EndpointSlice等建立便于查找的结构，再选出相关Gateway并调用 `InitIRs`。随后处理listener、TLS、各类Route与策略。返回的TranslateResult不仅包含XdsIR和InfraIR，还包含需要更新状态的资源。

第19章的策略逻辑可以继续走到 `internal/gatewayapi/backendtrafficpolicy.go` 的 `ProcessBackendTrafficPolicies`。代码注释列出Route rule、Route、ListenerSet listener、ListenerSet、Gateway listener、Gateway的处理次序；同名目标的冲突状态在这里形成。读到“更具体优先”时，再看它如何与mergeType组合，而不只凭文档一句话想象所有字段合并。

这一层最适合定位资源语义问题：parentRefs是否允许附着、跨Namespace引用是否有Grant、后端端口是否存在、策略是否冲突。若缺少ReferenceGrant，错误首先属于资源语义与授权，而不是等待Envoy接到错误配置后自己猜测。

## 22.4 IR为什么分两种

IR是Intermediate Representation，即内部中间表示。`internal/ir/xds.go` 定义网关需要的代理语义，例如HTTP listener、route、destination与安全、遥测设置；它并不是把用户原始YAML直接塞到Envoy里。`internal/ir/infra.go` 则定义代理基础设施需求，例如名称、Namespace、listener端口及EnvoyProxy配置。

将二者分开，是因为“如何代理请求”和“运行多少代理、怎样暴露端口”相关却不同。只修改一个路由header，不一定需要滚动重建Envoy Pod；修改副本或Pod资源，可能主要影响基础设施。实际是否触发更新要看字段与渲染结果，不能承诺所有配置变更都零重启。

你可以把IR理解为一道清楚的边界：上游接收Kubernetes对象与引用规则，下游接收已整理的代理需求。它降低输入API与Envoyprotobuf细节之间的直接耦合，也让单元测试能分别比较每一阶段输出。IR不是公开稳定API，不宜把它当成长期外部集成契约。

## 22.5 xDS translator与服务器：形成Envoy能接受的资源

打开 `internal/xds/translator/translator.go` 的 `Translate`。它接收 `*ir.Xds`，生成 `ResourceVersionTable`，再由具体listener、route、cluster等文件构建Envoyprotobuf资源。函数会收集部分翻译错误；源码明确说明采用best effort，不把所有罕见扩展错误直接变成整个流程崩溃。这不等于所有错误都会被忽略，必须看返回错误与资源status。

然后读 `internal/xds/runner/runner.go`：Start创建快照缓存，建立带TLS配置的gRPC服务器，并注册xDS服务；后续订阅XdsIR的更新，翻译并发布版本化配置。它复用go-control-plane的服务组件，但还包含Envoy Gateway自己的缓存、身份与回调处理，不能把本项目简化成“只调用SetSnapshot”。

Envoy仍须接收、验证和应用资源。生成配置、服务器缓存更新、Envoy ACK、业务请求成功是不同检查点。扩展注入了不兼容filter时，Kubernetes清单可能已保存，但数据面会出现拒收或配置未按预期生效。此时需要结合控制器日志、ACK/NACK相关信息以及Envoy实际配置定位。

## 22.6 infrastructure manager与幂等reconcile

打开 `internal/infrastructure/runner/runner.go`，它订阅InfraIR并调用manager创建、更新或删除代理基础设施。再看 `internal/infrastructure/kubernetes/infra.go` 的 `createOrUpdate`，可以清楚看到ServiceAccount、ConfigMap、Deployment、Service及可选DaemonSet、HPA、PodDisruptionBudget等资源的处理。真实创建行为由配置决定，不代表每个Gateway总会生成列表中所有对象。

`internal/infrastructure/kubernetes/infra_client.go` 的 `ServerSideApply` 使用字段管理者envoy-gateway，Delete对不存在对象作容错。这些细节体现幂等思想：重复执行相同期望，应趋向同一结果，而不是每次再创建一套资源。幂等不是“永远不会出错”，网络失败、权限问题和字段所有权仍需处理和重试。

也因此，不应把手工编辑生成的Deployment当成长久配置接口。下次reconcile可能把它改回计算结果；需要通过EnvoyProxy等声明入口表达期望。源码中基础设施leader election分支还说明，多个控制器副本必须协调基础设施写入，而不是各自任意建立重复代理。

## 22.7 用成对测试样本理解边界

读完入口函数后，最有效的下一步不是立刻浏览全部辅助函数，而是看 `internal/gatewayapi/testdata` 中成对的 `.in.yaml` 与 `.out.yaml`。输入描述资源组合，输出记录翻译结果与状态。选一个最接近实验的HTTPRoute样本，先找Gateway、Route和Service，再看输出中的匹配条件、目标和错误条件。它们把复杂代码压缩为可对照的具体场景。

再到 `internal/xds/translator/testdata`，观察内部表示怎样生成Envoy资源。两层测试各自关注不同边界：第一层证明资源语义如何解释，第二层证明已解释的意图怎样编码成protobuf配置。即使两层单元测试都通过，也仍需要真实Envoy或端到端测试确认协议和运行环境，所以本书另外保留可执行的kind实验。

查找时先按可见现象搜索，例如后端引用、限流或证书，再沿调用关系逐步缩小范围。不要把某个测试文件中的一个输出名称推广成永久命名契约，尤其不要据此编写缺少版本约束的Patch。测试样本是理解当前tag行为的证据；公开API和兼容承诺仍要回到正式类型、CRD与版本文档核对。

## 22.8 练习与答案

练习一：Pod地址变了，HTTPRoute没变，谁触发更新？答案：provider观察EndpointSlice等相关变化，重新收集资源并推动后续翻译。

练习二：准备修改限流字段，在什么层开始找？答案：先查api/v1alpha1类型与CRD，再看gatewayapi/backendtrafficpolicy.go如何形成IR，最后看xds/translator对应filter输出。

练习三：想增加Envoy副本，应直接改生成Deployment吗？答案：长期配置应修改受支持的EnvoyProxy/provider基础设施设置，再观察InfraIR与渲染输出。直接改生成对象可能被reconcile覆盖。

## 固定版本阅读入口

- [provider/controller.go](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/provider/kubernetes/controller.go)
- [Gateway API translator](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/gatewayapi/translator.go)与[runner](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/gatewayapi/runner/runner.go)
- [Xds IR](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/ir/xds.go)与[Infra IR](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/ir/infra.go)
- [xDS translator](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/xds/translator/translator.go)与[xDS runner](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/xds/runner/runner.go)
- [基础设施manager](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/infrastructure/kubernetes/infra.go)与[写入封装](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/infrastructure/kubernetes/infra_client.go)

[上一章](21-observability.md) · [下一章](23-extensions-and-operations.md)
