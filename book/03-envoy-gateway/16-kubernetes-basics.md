# 第16章 从进程到集群：第一次认识 Kubernetes

本章先修只有前面讲过的进程、IP、端口与 HTTP。学完后，你应能解释 Pod、Deployment、Service、Namespace 的分工，读懂一份最小清单，并沿 Service 找到真正接收请求的实例。暂时不用记住所有命令，先把“谁保存期望、谁执行工作、请求经过哪里”分清。

## 16.1 为什么不能只运行一个容器

在本机启动 Envoy 时，你知道进程在哪台机器、监听哪个端口。换成十台机器后，问题变多：进程退出由谁重启？两份后端应该放在哪些机器？更新镜像时，旧请求怎么办？这些问题属于运行环境的管理。Kubernetes 把机器组成集群，让你提交“希望存在两个后端实例”的声明，然后由控制器持续接近期望。

集群由控制面和工作节点组成。API Server 是管理入口，负责认证、授权、校验与保存资源；它不是业务请求的必经代理。调度器选择实例运行的节点；节点上的 kubelet 管理容器生命周期。控制器读取资源状态并执行修正。命令行工具 kubectl 调用 API Server，因此 `kubectl apply` 成功只表示声明被接收，不表示容器已启动、更不表示用户请求已成功。

本书用 kind 把 Kubernetes 节点运行在容器里。一台笔记本可以承载一个小集群，但它仍有 API Server、节点和集群网络。先确认 Docker 或兼容运行时可用，之后再创建名为 `envoy-book` 的独立集群。不要拿公司现有集群练手。

## 16.2 Pod 是运行单位，Deployment 是恢复承诺

Pod 是 Kubernetes 调度的基本单位，通常包含一个应用容器。一个 Pod 内的容器共享网络环境，可以通过 localhost 通信；不同 Pod 有各自的地址。Pod 重建后，名字和 IP 都可能变化。把业务路由永久写成某个 Pod IP，就像把顾客地址簿写成“今天值班员工的临时工位”，无法应对替换。

Deployment 保存副本数量和 Pod 模板，通过 ReplicaSet 维持实例。实验中 `replicas: 2` 表示期望两个后端 Pod。删除其中一个 Pod，控制器会创建替代者；修改容器镜像，Deployment 组织滚动更新。它不会自动保证业务没有错误，所以还要有 readinessProbe：实例准备接收流量之后，才应成为可用后端。

观察对象的关系：

```text
Deployment → ReplicaSet → 两个 Pod → 各自的容器进程
```

左边是管理关系，箭头不代表 HTTP 请求会依次经过这些对象。真实请求最终进入 Pod 中的监听端口。`containerPort: 3000` 描述容器端口，也可以给端口命名；应用进程本身仍须真正监听 3000，写一个字段不会替进程打开 socket。

## 16.3 Service 与 EndpointSlice：稳定名字和变化地址

Service 提供稳定的服务名称与一组端口。例如实验的 `backend` Service 将 `port: 3000` 映射到 Pod 的命名端口 `http`。它通过 `selector: app: book-backend` 选择带同样 label 的 Pod。label 是元数据键值，不是文件目录，也不要求资源名字一致。

EndpointSlice 保存该 Service 对应的端点信息，通常包含 Pod IP、端口以及 ready 等条件。Kubernetes 的端点控制器根据选择器和 Pod 状态维护这些数据。Envoy Gateway 的 Kubernetes provider 会读取相关 Service 和 EndpointSlice，翻译出 Envoy 可用的上游地址。对于本书默认配置，应理解为 Envoy 可以直接连接端点，而不是断言每个请求必经 Service 的 ClusterIP。

区分三种端口十分重要：Gateway listener 的 80 是客户端入口；Service 的 3000 是 HTTPRoute 引用的服务端口；Pod 的目标端口是应用实际接收连接的位置。它们可以碰巧相同，但不要求相同。HTTPRoute 的 `backendRefs.port` 应填 Service 的 `port`，不能凭容器端口猜测。

## 16.4 Namespace、作用域与权限

Namespace 把命名空间级资源分组。`envoy-book/backend` 与 `another/backend` 是两个不同 Service。Deployment、Pod、Service、Secret、Gateway、HTTPRoute 通常都属于某个 Namespace；GatewayClass、Namespace、CRD 则是集群级对象，没有所属 Namespace。给集群级 GatewayClass 加 `-n envoy-book` 不会让它变成私有资源。

本书把业务清单放在 `envoy-book`，控制器和默认生成的数据面放在 `envoy-gateway-system`。这解释了为什么看业务 Pod 时用前者，看 Envoy Pod 时用后者。Namespace 便于组织和授权，但它本身不保证网络隔离；网络隔离还需要网络插件支持的 NetworkPolicy，以及适当 RBAC。能创建 HTTPRoute 也不等于能读取任意 Namespace 的 Secret。

上下文 context 是 kubectl 的连接配置，包含集群、身份和默认 Namespace。本书命令显式写 `--context kind-envoy-book`，避免当前默认上下文悄悄指向另一套环境。清理也只删除这个独立 kind 集群。

## 16.5 读一份 YAML，而不是背一份 YAML

打开 [base.yaml](../../labs/03-envoy-gateway/base.yaml)。每段以 `apiVersion` 指明 API 版本，`kind` 指明资源类型，`metadata` 给出身份，`spec` 描述期望。`---` 分隔多个独立对象。缩进表达层级，所以 `selector` 属于 Service spec，不应写到 metadata 下。`status` 由控制器填写，通常不应复制旧 status 来当配置。

完成第18章安装后执行：

```bash
kubectl --context kind-envoy-book -n envoy-book get deployment,pod,service
kubectl --context kind-envoy-book -n envoy-book get endpointslice \
  -l kubernetes.io/service-name=backend -o yaml
kubectl --context kind-envoy-book -n envoy-book describe deployment backend
```

第一条看到期望副本与 Ready 数量；第二条找到实际端点和就绪条件；第三条查看 Deployment 事件。如果 Pod 是 `ImagePullBackOff`，先看镜像拉取事件与网络，不要先改 HTTPRoute。如果 Pod 正常但 EndpointSlice 没有可用地址，检查 selector、Pod label、readinessProbe 与目标端口。

## 16.6 名称解析、探针与一次完整定位

除了稳定地址，Service还有集群内部域名。同Namespace的应用通常能用backend访问本地Service，完整形式还包含Namespace与集群域名。这种名称由集群DNS解析，主机上的普通浏览器并不自动拥有同样的解析能力。因此“容器内能访问backend，笔记本curl不能解析backend”不矛盾。第18章使用本机端口转发，正是为主机提供明确的测试入口。

探针也要分清目标。readinessProbe判断当前实例是否应接收流量；livenessProbe判断是否需要重启容器；startupProbe用于给启动较慢的应用更合适的启动检查窗口。把依赖数据库的一次短暂失败直接做成激进的存活失败，可能导致大量Pod反复重启，故障更难恢复。本书先用简单就绪探针，帮助观察端点何时进入可用集合。

遇到“网关通了但应用不通”时，可以按身份链逐步核对：HTTPRoute到底引用哪个Namespace、哪个Service与哪个端口；Service选择器到底匹配哪些Pod；这些Pod是否Ready；EndpointSlice里的地址和目标端口是否正确；应用是否真的在对应地址与端口监听。每一步只回答一个问题，前一步的存在不能替下一步作保证。

最后留意配置的持久位置。手工给某个现存Pod补label，会改变当下选择结果，但下次Deployment重建Pod时，模板仍可能生成旧label。长期修正应该落在Deployment的Pod模板或其他真正的声明来源上。相同原则也适用于控制器生成的Envoy资源：先找到负责它的上层配置，再修改期望，而不是追着不断重建的运行对象改动。

## 16.7 常见误解与练习

`Running` 说明 Pod 中容器处于运行状态，不保证应用已经 Ready；Service 对象存在，也不保证选中了任何 Pod；`apply` 没报错，更不保证应用的依赖数据库正常。排障时，每一步都要寻找对应证据。

练习一：Service 选择 `app: book-backend`，Pod 只有 `app: backend`，需要改 HTTPRoute 吗？答案：先修复 Service selector 或 Pod 模板 label 的不一致。路由引用正确 Service 时，改路由无法制造端点。

练习二：Service 的 `port: 8080`、`targetPort: 3000`，HTTPRoute 应引用哪一个？答案：8080。它引用 Service 的服务端口；控制器再解析目标端点端口。

练习三：两个 Namespace 都有名为 backend 的 Service，省略 backendRef.namespace 会选谁？答案：默认选择 HTTPRoute 所在 Namespace 的对象；跨 Namespace 需要明确引用以及后面介绍的授权。

## 本章依据

- [v1.9.2 官方最小示例](https://github.com/envoyproxy/gateway/blob/v1.9.2/examples/kubernetes/quickstart.yaml)：Deployment、Service 与 HTTPRoute 的真实组合。
- [v1.9.2 Kubernetes provider](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/provider/kubernetes/controller.go)：`processBackendRefs` 读取 Service/EndpointSlice，`watchResources` 注册观察。
- [Kubernetes v1.34 Service 概念](https://v1-34.docs.kubernetes.io/docs/concepts/services-networking/service/)与[Deployment 概念](https://v1-34.docs.kubernetes.io/docs/concepts/workloads/controllers/deployment/)。

[上一章：控制面生产边界](../02-go-control-plane/15-production-boundaries.md) · [下一章：Gateway API](17-gateway-api.md)
