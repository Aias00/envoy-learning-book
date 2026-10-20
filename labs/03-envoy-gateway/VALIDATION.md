# 实验三真实验证记录

日期：2026-10-20；以下为本次工作时真实执行结果，不是所有环境的可用性承诺。

## 环境与固定输入

macOS arm64，OrbStack提供Docker接口；kind v0.33.0，节点镜像kindest/node:v1.34.0，服务端Kubernetes v1.34.0，kubectl v1.33.9。主机Helm v4.3.1，容器内用于拉chart的官方Helm v3.19.0 linux-arm64。独立集群envoy-book、context kind-envoy-book；业务Namespace envoy-book。

Envoy Gateway官方OCI chart为gateway-helm-v1.9.2，appVersion v1.9.2，OCI digest：

```text
sha256:be034275b55deeddd6b7bc1f4da6eeb02b8efc418a4a29e2b1977851b24b1e63
```

运行中控制器与shutdown-manager镜像为docker.io/envoyproxy/gateway:v1.9.2；Envoy数据面为：

```text
docker.io/envoyproxy/envoy:distroless-v1.39.1@sha256:eb2c01c13125d1629637cb4e4cce7207009fb7cc2c8027f9742758549d15b6f4
```

后端为registry.k8s.io/gateway-api/echo-basic:v1.5.1，两个Pod Ready。

## 安装路径与环境问题

主机直接访问registry-1.docker.io时连接reset，包括IPv4；Helm OCI下载因此失败。已有Alpine容器内访问同一地址可获得正常401认证挑战。使用任务临时目录中的官方Linux Helm在Alpine容器内拉取相同v1.9.2 chart，首次DockerHub CDN返回EOF，第二次成功。主机随后通过ENVOY_BOOK_CHART指向下载的官方tgz，运行setup/smoke，仍由Helm安装，没有改用别的发行版或源码构建。

Docker公开拉取因用户现有凭据助手遇到锁定钥匙串时，使用任务专用的临时DOCKER_CONFIG；DOCKER_HOST仅为当前命令指定OrbStack socket。没有修改用户Docker配置、DNS、代理或系统网络设置。官方release install.yaml已下载备用，但没有应用。

示意执行方式（临时目录应替换为实际路径）：

```bash
DOCKER_CONFIG=/具体任务Docker临时目录 \
DOCKER_HOST=unix:///用户路径/.orbstack/run/docker.sock \
ENVOY_BOOK_CHART=/具体任务Helm临时目录/gateway-helm-v1.9.2.tgz \
  bash labs/03-envoy-gateway/smoke.sh
bash labs/03-envoy-gateway/tls.sh
```

## 通过的真实检查

| 检查 | 观察结果 |
| --- | --- |
| 官方Helm安装及重复执行 | release eg deployed，chart/appVersion v1.9.2；重复执行成功升级同一release |
| 当前资源状态 | GatewayClass Accepted；三条HTTPRoute对应book父对象Accepted/ResolvedRefs，BTP对应祖先Accepted；observedGeneration等于当前generation |
| 基础HTTP请求 | Host book.example返回200，JSON含backend Pod身份 |
| 不存在的Host | unknown.example返回404 |
| path/header规则 | routing.example/api带x-book-mode: debug返回x-book-route: debug-api |
| 本地限流 | 首次连续200、200、200、429；重复smoke立即429，符合已耗尽的实例本地令牌桶 |
| EndpointSlice | backend两个Ready端点，端口3000 |
| TLS终止正例 | 使用--cacert与--resolve，secure.book.example入口返回200，后端显示httpPort3000 |
| TLS信任反例 | 不提供自签CA时curl退出60 |
| TLS名称反例 | 保持正确SNI，openssl用错误verify_hostname验证，返回错误62 hostname mismatch |
| 日志格式 | 应用observability.yaml并引用后，真实JSON日志含duration_ms、status200、route和upstream地址 |
| 指标 | Envoy Pod19001的/stats/prometheus返回Prometheus格式指标 |
| 占用端口 | 已有转发占18888时，新port-forward退出；就绪检查拒绝继续请求 |
| 服务器schema校验 | base/routing/ratelimit/observability/tls五清单的12个资源server dry-run通过 |

kind没有LoadBalancer地址，Gateway Programmed=False，reason为AddressNotAssigned；同时本地port-forward业务请求通过。这个结果不证明公网DNS、云负载均衡、真实客户端源地址或外部入口已验证。

## 静态与未执行范围

脚本Bash语法、YAML解析、固定tag CRD字段结构与文档链接分别检查。真实运行不包含跨Namespace授权、上游TLS、全局限流、高可用故障演练、生产负载、滚动升级兼容性或完整遥测collector部署。

本次保留独立集群供继续观察；脚本退出后结束自身port-forward。临时证书含私钥，保存在任务临时目录，未复制到仓库。需要彻底重置时运行本目录cleanup.sh，仅删除kind集群envoy-book；主机临时文件按精确路径另行处理。
