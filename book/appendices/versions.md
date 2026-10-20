# 附录 B　固定版本、兼容矩阵与资料来源

## 本书坐标

本书资料核对与首版验证日期为 2026-10-20。正文围绕下列固定 tag 展开，避免 main 分支变化后代码与讲解脱节。

| 用途 | 上游版本 | 对应 commit / 依据 |
| --- | --- | --- |
| Envoy Gateway 源码 | v1.9.2 | `f9f92a112a4033abed5c8d43a5f7e37da257403c` |
| go-control-plane 根模块 | v0.14.0 | `71637ad69bbc5f51fbb2562e612a4365292804a5` |
| 实验二 API Go 子模块 | envoy v1.36.0 | 根模块 v0.14.0 go.mod 的依赖基线 |
| 实验一、二代理镜像 | envoyproxy/envoy:v1.36.0 | 与实验 API 基线一起验证 |
| EG v1.9 兼容资料 | Gateway API v1.6.1、Kubernetes v1.33–v1.36、Envoy distroless-v1.39.1 | 固定 tag 的 compatibility matrix |
| 实验三节点镜像 | kindest/node:v1.34.0 | 位于 EG v1.9 支持范围 |

EG tag v1.9.2 使用 annotated tag 对象 `bdbfe06a726bec16febe609fb7ce4efad438494e` 指向上述 commit，tag 对象 SHA 不应与 commit 混淆。实验具体镜像配置仍以 compose.yaml、kind.yaml 和 Helm chart 为准。

## 为什么不全部使用 latest

不同组件独立发布。根 Go 模块 tag 是 `v0.14.0`，生成的 Envoy API 子模块 tag 类似 `envoy/v1.36.0`。在 GitHub 查 latest release，可能看到 `envoy/v1.40.0`；它不等于根模块存在 v1.40.0。Go 依赖应以模块路径加对应模块版本选择，不能仅靠仓库首页的 latest 字样。

同样，本书 Go 实验与 EG 实验属于两条路径。EG 内部 Go 依赖和所管理的 Envoy 镜像由其发行版定义，不应把实验二镜像强行替换进去。面向生产环境升级时，请查目标 EG 发行版、Gateway API、Kubernetes 与 Envoy 的兼容组合，再在独立环境验证。

## 如何复核与跟踪变化

源码可直接打开每章固定 tag 链接，也可下载相应仓库的 tag 到本机。源码只用于阅读，不需要把整个上游仓库纳入本书提交。升级学习基线时，至少核对：根/子模块 go.mod、缓存和服务端接口、CRD schema、策略字段、兼容矩阵、Helm 安装方式、镜像、Admin 输出与实验行为。

本书阅读时发现文档和类型冲突，优先核对同 tag 的 Go 类型与实际发布 CRD，并在隔离集群验证。EG v1.9.2 的本地限流示例使用 `rateLimit.local.rules[].limit`；不能把某个旧文档片段的字段直接套入新 CRD。第 19 章给出具体例子。

## 主要来源

- [go-control-plane v0.14.0 README](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/README.md) 与[根 go.mod](https://github.com/envoyproxy/go-control-plane/blob/v0.14.0/go.mod)。
- [Envoy Gateway v1.9.2 README](https://github.com/envoyproxy/gateway/blob/v1.9.2/README.md)、[v1.9 文档目录](https://github.com/envoyproxy/gateway/tree/v1.9.2/site/content/en/v1.9)、[兼容矩阵来源](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/news/releases/matrix.md)。
- [Envoy v1.36.0 文档](https://www.envoyproxy.io/docs/envoy/v1.36.0/)、[xDS 协议](https://www.envoyproxy.io/docs/envoy/v1.36.0/api-docs/xds_protocol)。
- [Gateway API 规范](https://gateway-api.sigs.k8s.io/)，学习时同时核对 EG v1.9 的功能支持与发布 CRD。

本书没有把“下载到源码”作为“已运行所有上游测试”的证据。真实执行范围、工具版本与未执行项见[验证记录](../../VALIDATION.md)。
