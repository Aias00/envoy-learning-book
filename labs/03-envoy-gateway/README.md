# Envoy Gateway v1.9.2 实验

本实验配合第16–23章。所有 Kubernetes 操作显式使用 `kind-envoy-book`，业务 Namespace 为 `envoy-book`。只有 GatewayClass、CRD 等规范要求的资源属于集群级；控制器和默认数据面运行在 `envoy-gateway-system`。

## 前置与版本

需要已启动的 Docker 兼容运行时、Bash、curl、ripgrep（rg）、jq、kubectl、kind、Helm；TLS步骤另外需要支持 `req -addext` 的 OpenSSL。建议至少给运行时分配4 CPU、6GiB内存与若干GiB可用磁盘；这是本地实验建议，不是软件最低资源承诺。

固定 Kubernetes 节点 `kindest/node:v1.34.0`，建议 kind v0.30.0+，Helm 3.19+（Helm 4 也可使用官方 OCI chart），kubectl 版本应与 Kubernetes v1.34 保持支持的版本差。Envoy Gateway 固定 `v1.9.2`。该tag兼容矩阵列出 Gateway API v1.6.1、Kubernetes v1.33–v1.36，chart 默认安装相应CRD。后端镜像使用与官方示例一致的 `registry.k8s.io/gateway-api/echo-basic:v1.5.1`。

所有命令从仓库根目录执行。名为 `envoy-book` 的 kind 集群应专用于本实验；setup会复用现存同名集群，不应把别的工作负载放进去。

## 最短可执行路径

```bash
bash labs/03-envoy-gateway/smoke.sh
```

smoke调用setup创建集群、安装官方Helm chart、应用基础资源，再检查Host转发200、未知Host 404、path/header路由和本地限流429。脚本按parent名称、Namespace及controllerName选择状态，等待当前generation的Route Accepted/ResolvedRefs与策略Accepted；先确认本次port-forward进程存活且日志出现Forwarding，再发送请求。HTTP探测每次连接超时2秒、总超时5秒，轮询有界。需要网络访问DockerHub和registry.k8s.io；首次拉取可能较慢。脚本保留集群，仅结束自己启动的本地port-forward，终端会报告日志目录。端口18888被占用时：

```bash
ENVOY_BOOK_PORT=18889 bash labs/03-envoy-gateway/smoke.sh
```

脚本的PASS仅覆盖它实际执行的检查。静态文件解析不能替代真实集群运行；本实验不包含生产负载、高可用或升级兼容性保证。

## 网络受限时使用同一官方chart

如果主机不能访问DockerHub，但可以从另一条受信网络下载同一chart，可先拉取官方固定版本，再给脚本指定本地tgz。setup会核对chart版本必须为v1.9.2：

```bash
helm pull oci://docker.io/envoyproxy/gateway-helm --version v1.9.2 --destination /具体任务临时目录
ENVOY_BOOK_CHART=/具体任务临时目录/gateway-helm-v1.9.2.tgz \
  bash labs/03-envoy-gateway/smoke.sh
```

占位目录需要替换为实际存在的精确路径。本次实测中，主机到registry-1.docker.io连接被reset，已有Alpine容器内部可达，因此在任务临时目录下载官方Linux Helm，在容器内执行相同helm pull，将tgz挂载回主机后安装。首次容器拉取遇DockerHub CDN EOF，第二次成功；没有修改用户DNS、代理或全局Docker配置。此方法解决chart获取，不保证节点拉取运行镜像的网络也通畅。实际版本、digest与检查结果见[验证记录](VALIDATION.md)。

## 分步观察

```bash
bash labs/03-envoy-gateway/setup.sh
kubectl --context kind-envoy-book -n envoy-book get deployment,pod,service
kubectl --context kind-envoy-book get gatewayclass envoy-book -o yaml
kubectl --context kind-envoy-book -n envoy-book get gateway book -o yaml
kubectl --context kind-envoy-book -n envoy-book get httproute backend -o yaml
kubectl --context kind-envoy-book -n envoy-book get endpointslice \
  -l kubernetes.io/service-name=backend -o yaml
```

setup内的安装命令为：

```bash
helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
  --version v1.9.2 --kube-context kind-envoy-book \
  -n envoy-gateway-system --create-namespace --wait --timeout 10m
```

`--version` 固定chart；`--kube-context` 固定目标集群；`--wait` 等待chart资源就绪。Helm release名eg与Gateway资源名book没有引用关系。chart装的是控制器；Gateway被接受后，控制器另外创建Envoy数据面。

预期应用两个Pod Ready，Service backend端口3000，EndpointSlice内有可用地址。GatewayClass应Accepted=True；Route的相应parent应Accepted=True、ResolvedRefs=True。kind没有云负载均衡，Gateway地址可能为空或pending，使用port-forward进行本地验证。

手工保留转发入口（此命令会持续占用终端）：

```bash
ENVOY_SERVICE=$(kubectl --context kind-envoy-book -n envoy-gateway-system \
  get service -l gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book \
  -o jsonpath='{.items[0].metadata.name}')
kubectl --context kind-envoy-book -n envoy-gateway-system \
  port-forward "service/$ENVOY_SERVICE" 18888:80
```

另开终端：

```bash
curl --noproxy '*' -i -H 'Host: book.example' http://127.0.0.1:18888/
curl --noproxy '*' -i -H 'Host: unknown.example' http://127.0.0.1:18888/
kubectl --context kind-envoy-book apply -f labs/03-envoy-gateway/routing.yaml
curl --noproxy '*' -i -H 'Host: routing.example' -H 'x-book-mode: debug' \
  http://127.0.0.1:18888/api
```

最后一个响应应包含 `x-book-route: debug-api`。同一路径不带x-book-mode头仍可匹配普通/api规则；/apix不属于PathPrefix /api的路径段匹配。

## 本地限流

```bash
kubectl --context kind-envoy-book apply -f labs/03-envoy-gateway/ratelimit.yaml
kubectl --context kind-envoy-book -n envoy-book get backendtrafficpolicy limited -o yaml
for attempt in 1 2 3 4 5 6; do
  curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' \
    -H 'Host: limited.example' http://127.0.0.1:18888/
done
```

策略生效后，一个Envoy实例的对应路由令牌桶允许有限突发，随后应出现429。此处用Hour方便人工观察。它不是跨副本总配额；再次运行可能立即429，因为令牌已消耗。配置发布是异步的，刚apply后不能把第一条响应当成生效边界。v1.9.2字段为 `rateLimit.local.rules[].limit`，不要抄旧示例的local.requests。

## TLS终止

在setup完成后执行：

```bash
bash labs/03-envoy-gateway/tls.sh
```

脚本生成有效期两天、含SAN `secure.book.example` 的临时自签证书，在envoy-book创建TLS Secret，应用独立Gateway book-tls并通过18443验证。curl显式使用 `--cacert` 和 `--resolve`，因此同时检查信任与主机名，不依赖 `-k`。端口冲突可设置 `ENVOY_BOOK_TLS_PORT=18444`。结束时报告证书目录，包含私钥，请妥善处理，勿提交。这个实验只加密客户端到Envoy；上游仍是HTTP，不能据此宣称端到端TLS。

## 日志与指标

Envoy默认已有stdout访问日志。查看默认日志：

```bash
kubectl --context kind-envoy-book -n envoy-gateway-system logs \
  -l gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book \
  -c envoy --tail=20
```

要启用本书较短JSON格式，先创建EnvoyProxy，再给Gateway增加引用：

```bash
kubectl --context kind-envoy-book apply -f labs/03-envoy-gateway/observability.yaml
kubectl --context kind-envoy-book -n envoy-book patch gateway book --type=merge \
  -p '{"spec":{"infrastructure":{"parametersRef":{"group":"gateway.envoyproxy.io","kind":"EnvoyProxy","name":"book-observability"}}}}'
```

配置生效后重新请求，观察status、flags、detail、route、upstream、request_id。可在第三个终端临时访问Envoy指标端口：

```bash
ENVOY_POD=$(kubectl --context kind-envoy-book -n envoy-gateway-system get pod \
  -l gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book \
  -o jsonpath='{.items[0].metadata.name}')
kubectl --context kind-envoy-book -n envoy-gateway-system port-forward "pod/$ENVOY_POD" 19001:19001
```

另开终端运行 `curl --noproxy '*' -fsS http://127.0.0.1:19001/stats/prometheus`。该端口是受限运维入口，只通过本机调试转发访问，不能公开给业务客户端。

## 排障

| 现象 | 首先查什么 |
| --- | --- |
| Docker无法连接 | 运行时是否启动，docker context是否指向预期环境 |
| Chart/镜像拉取失败 | DNS、代理、镜像仓库可达性与限额，保留原始错误 |
| no matches for kind | 当前context、CRD安装状态与served API版本 |
| Pod ImagePullBackOff | describe pod的Events，镜像地址与网络 |
| HTTPRoute未Accepted | status.parents对应的parentRef、controllerName、conditions |
| 本地连接拒绝 | port-forward进程与端口冲突 |
| 404 | Host/path、listener hostname、Route附着 |
| 503 | EndpointSlice就绪端点、Service port、应用监听和网络 |
| TLS主机名不匹配 | URL名称、SNI、SAN；单独改Host头不能修改SNI |

需要服务器端schema校验时，先完成setup再运行：

```bash
kubectl --context kind-envoy-book apply --dry-run=server \
  -f labs/03-envoy-gateway/routing.yaml -f labs/03-envoy-gateway/ratelimit.yaml \
  -f labs/03-envoy-gateway/observability.yaml
```

## 明确清理

先在所有手工port-forward终端按Ctrl-C，再执行：

```bash
bash labs/03-envoy-gateway/cleanup.sh
```

只删除独立kind集群envoy-book，其中实验对象与Secret随集群消失。它不会清理其他集群、其他Namespace或主机镜像。脚本打印的临时日志/证书目录仍在主机上；可确认其精确路径后自行移到废纸篓。清单保留，可重新setup重建。

## 固定版本来源

- [官方v1.9.2 quickstart](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/quickstart.md)
- [官方v1.9.2 Helm安装](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/install/install-helm.md)
- [限流真实API类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/ratelimit_types.go)
- [TLS官方任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/security/tls-termination.md)
- [日志真实API类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/envoyproxy_accesslogging_types.go)
