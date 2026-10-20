# 第17章 Gateway API：把入口管理拆成明确的责任

先修：第16章的资源、Namespace、Service 与 EndpointSlice。学习目标：能画出 GatewayClass、Gateway、HTTPRoute 的引用关系，解释跨 Namespace 的两种授权，正确读取 status。Gateway API 是一套 Kubernetes API 规范；Envoy Gateway 是实现这套规范的控制器之一；Envoy 则负责实际代理请求。三个名字相近，职责不同。

## 17.1 为什么要分成三个对象

假设公司有平台团队和支付团队。平台团队负责公网负载均衡、证书和入口容量；支付团队只想把 `pay.example/api` 发往支付 Service。如果所有人都修改同一份入口配置，路由变更与基础设施变更会挤在一起，权限也难以限制。

GatewayClass 表示一类网关实现，通常由集群管理员管理。它是集群级对象；`spec.controllerName` 告诉控制器这类对象由谁负责。实验使用 `gateway.envoyproxy.io/gatewayclass-controller`。该字符串是控制器标识，不是 Kubernetes DNS 名称，也不是你要运行的 Service 名。随意换成 `envoy-book` 会使默认配置的 Envoy Gateway 不接管它。

Gateway 是命名空间级对象，引用 GatewayClass，描述入口 listener：端口、协议、可选 hostname、TLS 证书与允许哪些 Route 附着。HTTPRoute 是应用侧规则，描述哪些 HTTP 请求发往哪些后端。基础关系如下：

```text
GatewayClass/envoy-book（集群级）
          ↑ gatewayClassName
envoy-book/Gateway/book（端口80，listener名http）
          ↑ parentRefs，sectionName: http
envoy-book/HTTPRoute/backend（Host book.example）
          ↓ backendRefs
envoy-book/Service/backend:3000 → EndpointSlice → Pod
```

引用方向表达资源依赖，业务流量不会请求 GatewayClass。控制器读取这组对象，生成基础设施和 Envoy 配置，客户端随后连接数据面。

## 17.2 listener 和 parentRefs 是双向约束

Route 的 `parentRefs` 表达“我希望挂到哪个入口”。例如 `name: book` 默认指同 Namespace 的 Gateway；`sectionName: http` 进一步指向 listener 的 `name`，不是指端口数字。写成 `sectionName: 80` 不会自动匹配端口80。

Gateway 的 `allowedRoutes` 表达“我允许谁挂上来”。本书显式使用 `namespaces.from: Same`，只接受同 Namespace 的 Route。跨 Namespace 时，Route 必须在 parentRef 里写 Gateway 的 namespace，而 Gateway 必须允许来源 Namespace。可用 `from: Selector` 配合 Namespace label 精确授权；`from: All` 范围更大，应先判断入口是否真的需要对所有租户开放。

两边满足 Namespace 约束还不够：Route 类型必须适合 listener 协议，listener hostname 与 Route hostnames 必须有交集，所选 section 必须存在。一个 HTTPS listener 限定 `shop.example`，Route 只声明 `pay.example`，即使资源名字引用正确，也不能提供有效附着。

## 17.3 ReferenceGrant 管的是另一条边

现在考虑 HTTPRoute 在 team-a，却引用 team-b 的 backend Service。仅凭 team-a 能写引用，不应获得访问 team-b 后端的权力。ReferenceGrant 应创建在被引用资源所在的 team-b，由资源所有者授权来自 team-a 的指定类型引用。

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: allow-team-a-backend
  namespace: team-b
spec:
  from:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      namespace: team-a
  to:
    - group: ""
      kind: Service
      name: backend
```

这里空字符串 group 表示 Kubernetes 核心 API 组，Service 不属于 gateway.networking.k8s.io。`to.name` 缩小到一个 Service；不写名称意味着允许这一类型下更广的目标。Grant 不创建 Service，也不替代 RBAC 或 NetworkPolicy。

必须分清：Route 跨 Namespace 附着 Gateway 主要通过 Gateway 的 allowedRoutes 同意；Route 跨 Namespace 引用 backend Service 通过目标 Namespace 的 ReferenceGrant 同意。给 Service 创建 Grant 并不能绕过 Gateway 的 Same 限制；把 allowedRoutes 改成 All，也不会自动允许跨 Namespace backendRef。Gateway 跨 Namespace 引用证书 Secret 同样涉及目标侧引用授权，不能只扩大 Route 的接入范围。

## 17.4 status 是控制器写给人的回执

`spec` 是你的期望，`status` 是控制器根据当前资源与实现写出的结果。读取 HTTPRoute 时，需要看 `status.parents` 中对应哪个 parentRef、哪个 controllerName 的条目，再看其中 conditions。一个 Route 可以有多个父对象，不能把数组第一项永远当成唯一真相；本书 smoke 只有一个父对象才简化读取第一项。

常见条件包括 Accepted：该资源或附着关系被接受；ResolvedRefs：引用的对象、端口和授权等能正确解析；Programmed：相关配置已被实现编程到数据面或基础设施。条件的具体归属与含义以对应资源规范为准。它们不是“所有业务依赖健康”的替代证明。Route Accepted=True、ResolvedRefs=True，仍可能因为 Pod 无可用端点返回503。

condition 不只有真假，还包含 reason、message、lastTransitionTime 和 observedGeneration。若你刚改了 spec，而 observedGeneration 仍是旧 generation，这条状态可能尚未反映最新期望。看到 False 时先读 reason 与 message，比把相同 YAML 重复提交十次有效。

```bash
kubectl --context kind-envoy-book get gatewayclass envoy-book -o yaml
kubectl --context kind-envoy-book -n envoy-book get gateway book -o yaml
kubectl --context kind-envoy-book -n envoy-book get httproute backend -o yaml
```

kind 没有 LoadBalancer 实现时，外部地址可能一直未分配。应同时看具体条件与事件，再用本书 port-forward 验证请求路径；不能把 EXTERNAL-IP 的 pending 单独当成路由失败结论。

## 17.5 API 版本不是软件发布版本

`gateway.networking.k8s.io/v1` 中的 v1 是资源 API 版本，不是 Envoy Gateway 1.x 的简称。Envoy Gateway v1.9.2 的仓库兼容矩阵列出 Gateway API v1.6.1，并支持 Kubernetes v1.33–v1.36。安装 chart 会引入相应 CRD；CRD 告诉 API Server 如何校验与保存这些自定义对象。只安装 CRD，没有控制器，资源可能保存成功却没人执行。

也不要把网页里可见的所有示例都当作当前 schema。我们在 v1.9.2 的概念页中仍能看到旧限流写法，真实 Go 类型与 CRD 已使用 rules 数组。固定 tag、检查 CRD served versions、用服务器 dry-run 校验，是把示例变成可靠配置的重要步骤。

## 17.6 用一个跨团队例子检查授权方向

设入口Gateway放在infra，商店应用的HTTPRoute放在shop，库存Service放在stock。第一步不是把所有允许项改成All，而是确定三方责任。infra的管理员控制谁能附着入口；shop的开发者控制哪些请求希望发往库存；stock的管理员控制谁可以引用库存服务。三者各写自己拥有的对象，授权便能从配置中读出来。

shop的Route需要在parentRefs中写infra和Gateway名。infra的Gateway允许带指定团队标签的Namespace，且shop这个Namespace本身要有该标签。标签选择的是Namespace对象，不能只给Route加同名标签就期望通过Namespace选择。然后Route在backendRefs中写stock与库存Service名；stock的ReferenceGrant允许来自shop的HTTPRoute引用这个指定Service。任何一侧未同意，引用链都不完整。

这一过程还揭示了为什么引用授权与网络连通不是同一个问题。Grant只允许控制器把资源引用当成合法输入，不会给Pod创建网卡，也不会修改防火墙。引用已解析，后端仍可能因NetworkPolicy、应用监听或证书失败而无法连接。反过来，即使网络层恰好能直接访问某Pod，也不代表某条跨Namespace引用就符合Gateway API授权规则。

看status时，用资源的最新generation关联控制器回执。假设旧配置有Accepted=True，你把sectionName改成不存在的listener，控制器尚未处理时，终端可能短暂仍显示旧状态。此时应等待observedGeneration追上当前generation，并重新读取对应父条目。复制一张旧截图或只看列表里的True，容易把异步更新误认为配置被认可。

对象归属也影响删除。删除HTTPRoute一般只撤去相应路由意图；删除Gateway则影响它承载的入口及附着路由；删除集群级GatewayClass会触及更广的管理关系。练习环境可以整体删除独立集群，真实团队则必须先梳理哪些资源共享入口，避免把某个应用的下线扩大成多个应用的入口中断。

## 17.7 练习与答案

练习一：修改 controllerName 为 `example.net/controller` 后，为何不再被默认 Envoy Gateway 处理？答案：控制器只管理匹配自身配置标识的 GatewayClass，资源存在不等于有实现认领。

练习二：Route 在 blue、Gateway 在 infra，backend 在 green，需要什么授权？答案：parentRef 指明 infra，Gateway allowedRoutes 允许 blue；backendRef 指明 green，green 中的 ReferenceGrant 允许 blue 的 HTTPRoute 引用目标 Service。还需对象、端口、协议等有效。

练习三：Accepted=True 能否证明返回200？答案：不能。继续看引用条件、端点、数据面配置，以及真实 HTTP 响应。控制层接受与应用层成功是不同证据。

## 本章依据

- [v1.9.2 HTTP routing 官方任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/traffic/http-routing.md)。
- [v1.9.2 内置 Gateway API CRD](https://github.com/envoyproxy/gateway/blob/v1.9.2/charts/gateway-helm/charts/crds/crds/gatewayapi-crds.yaml)：作用域、API served versions 与字段校验。
- [v1.9.2 兼容矩阵](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/news/releases/matrix.md)。
- [Gateway API v1.6.1 规范源码](https://github.com/kubernetes-sigs/gateway-api/tree/v1.6.1/apis)：Gateway、HTTPRoute、ReferenceGrant 定义与条件注释。

[上一章](16-kubernetes-basics.md) · [下一章](18-first-gateway.md)
