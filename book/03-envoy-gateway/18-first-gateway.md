# 第18章 在独立集群跑通第一条请求

先修：第16、17章，以及能在终端执行命令。本章目标是安装固定版本 Envoy Gateway、部署后端，观察一次 HTTP 请求真正经过数据面。配套目录是 [labs/03-envoy-gateway](../../labs/03-envoy-gateway/README.md)。实验会下载 Kubernetes 节点、控制器及 Envoy 镜像，需要可访问相关镜像仓库的网络与足够磁盘。

## 18.1 先说清楚会创建什么

本书使用名为 `envoy-book` 的 kind 集群，对应 context `kind-envoy-book`；业务 Namespace 也是 `envoy-book`，控制器 Namespace 为 `envoy-gateway-system`。kind 配置固定节点镜像 `kindest/node:v1.34.0`。该版本在 Envoy Gateway v1.9 支持的 Kubernetes 范围中。建议 kind v0.30.0 或更新版本，Helm 3.19 或更新版本，kubectl 与服务端版本保持官方允许的兼容范围。

容器运行时必须先启动。macOS 可使用 Docker Desktop 或提供 Docker 接口的 OrbStack；Linux 通常使用 Docker。运行 `docker info` 应能返回服务端信息。如果只装了命令行而后台没启动，kind 无法创建节点，这发生在安装 Envoy Gateway 之前。

检查命令：

```bash
docker info
kind version
kubectl version --client
helm version
rg --version
jq --version
```

脚本还需要 curl 和 Bash。ripgrep 的命令名是 rg，用于筛选预期输出；jq 用于核对正确父对象、控制器与当前 generation 的状态条件。本书不自动安装这些主机工具，工具安装方式取决于操作系统。已有同名 kind 集群时，setup 会复用它；因此 `envoy-book` 应专门保留给本书实验，不要用它承载别的项目。

## 18.2 先分步运行，再使用自动检查

从仓库根目录执行：

```bash
bash labs/03-envoy-gateway/setup.sh
```

脚本先检查工具和容器运行时，按 [kind.yaml](../../labs/03-envoy-gateway/kind.yaml) 创建集群，然后执行与官方 quickstart 同一入口的固定版本安装：

```bash
helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
  --version v1.9.2 --kube-context kind-envoy-book \
  -n envoy-gateway-system --create-namespace --wait --timeout 10m
```

`eg` 是 Helm release 名，可以理解为这次安装的记录名；OCI 地址是 chart 仓库；version 固定软件版本；namespace 指定控制器位置；wait 要求等待 chart 管理的相应资源就绪。默认安装包含 Gateway API 与 Envoy Gateway CRD，因此不必再叠加一份来源不明的 Gateway API 清单。真实生产集群若已有平台管理的 CRD，须先确认版本与所有权，不能直接照搬本书新建集群流程。

之后脚本应用 base.yaml。该文件定义 Namespace、GatewayClass、Gateway、Deployment、Service 和 HTTPRoute。提交后会先看到控制器 Deployment，再由 Envoy Gateway 为 Gateway 建立 Envoy 数据面。控制器进程不代理业务请求，它观察 API 并产生配置。两个应用副本也不是两份 Envoy；它们是数据面的上游。

## 18.3 找到数据面，而不是猜它的名字

Gateway 生成的资源名可能带有派生后缀。用 controller 放上的归属 label 寻找 Envoy Service：

```bash
ENVOY_SERVICE=$(kubectl --context kind-envoy-book -n envoy-gateway-system \
  get service -l gateway.envoyproxy.io/owning-gateway-namespace=envoy-book,gateway.envoyproxy.io/owning-gateway-name=book \
  -o jsonpath='{.items[0].metadata.name}')
kubectl --context kind-envoy-book -n envoy-gateway-system \
  port-forward "service/$ENVOY_SERVICE" 18888:80
```

第一条提取资源名称；第二条保持前台运行，在本机18888端口建立通往 Service 80端口的调试通道。另开终端执行：

```bash
curl --noproxy '*' -v -H 'Host: book.example' http://127.0.0.1:18888/
```

Host 必须与 HTTPRoute 的 hostnames 匹配。连接地址127.0.0.1解决“连接到哪里”，Host 解决“请求哪个站点”，二者职责不同。`--noproxy '*'` 避免主机代理环境变量把本地测试送到外部代理。预期得到200与 echo JSON，其中包含 `backend-` 开头的 Pod 名称。它证明这次请求穿过 Envoy 并到达应用，不只证明安装命令成功。

kind 默认没有云 LoadBalancer。Service 的 EXTERNAL-IP 为 pending 时，port-forward 仍可提供本地测试入口。它适用于学习与调试，不模拟真实互联网入口的健康检查、源地址、负载均衡或高可用。Gateway listener 80映射到实际容器端口时，也可能由实现使用非特权端口，调试应以生成的 Service 与 Pod 配置为准。

## 18.4 用失败请求确认规则边界

把 Host 改成 unknown.example：

```bash
curl --noproxy '*' -i -H 'Host: unknown.example' http://127.0.0.1:18888/
```

预期404，因为没有匹配的虚拟主机/路由。此时连接、监听器和 Envoy 都可能正常，失败的是 HTTP 路由匹配。再把本机端口改成18889，若没有进程监听，应得到连接失败而非 HTTP 404。这两个实验能帮助你区分“还没进入HTTP处理”和“已处理HTTP但没找到路由”。

查看状态与端点：

```bash
kubectl --context kind-envoy-book -n envoy-book get gateway,httproute -o yaml
kubectl --context kind-envoy-book -n envoy-book get endpointslice \
  -l kubernetes.io/service-name=backend
```

HTTPRoute 的 parent 条目应包含 Accepted=True，引用正常时还应看到 ResolvedRefs=True。假如返回503，先确认两个后端是否 Ready、EndpointSlice 是否包含可用地址，再检查 Service port、容器监听与网络。不要因为“HTTPRoute 是绿色”就跳过后端。

## 18.5 自动 smoke 的范围

停掉手工 port-forward，运行：

```bash
bash labs/03-envoy-gateway/smoke.sh
```

脚本会复用 setup，应用路由与限流示例，检查基础转发、未知Host 404、请求头专用响应头、本地限流429，最后展示端点。它自动结束自己启动的 port-forward，保留集群便于继续阅读。默认端口被占用时可用 `ENVOY_BOOK_PORT=18889 bash labs/03-envoy-gateway/smoke.sh`。

smoke 输出 PASS 只证明脚本列出的检查在那次执行中通过，不代表 TLS、跨 Namespace、升级与容灾都已验证。TLS 是第20章独立步骤。仓库验证记录应区分静态校验、真实集群执行、受环境限制的未执行项；阅读命令和推测结果不能算执行成功。

## 18.6 把一次执行记录成可以复查的实验

实验不只保存最后一行PASS。建议在笔记里记录主机系统、kind与kubectl版本、节点镜像、Helm chart版本、执行时间，以及关键响应。这样同一份清单换到另一台电脑失败时，可以比较环境差异，而不是猜“是不是网关偶尔不稳定”。版本固定解决软件输入的一部分，DNS、代理、镜像仓库和容器运行时仍是外部条件。

如果安装中断，先读现有状态再重跑。setup使用upgrade --install与同名声明资源，目标是重复执行后接近相同期望；它不会因为重跑就故意创建第二套业务资源。但网络失败可能留下正在拉取镜像的Pod或失败的Helm操作，必要时先查看Helm release状态、控制器Pod事件与原始错误。保留错误比立即删集群更利于理解。

验证也要包含拒绝路径。本实验既检查正确Host转发，也检查错误Host得到404；后续限流既观察正常请求，也观察超额拒绝。若只测试“某个请求能返回200”，路由过宽、认证失效、限流未生效都可能被误判为成功。每个规则都应至少有一个应通过样例和一个应拒绝或走其他路径的样例。

最后，端口转发会改变你测试的外部环境。它便于验证代理本身，但绕过了公网DNS、云负载均衡、边缘防火墙和真实客户端网络。学会把结论写得准确：“本地转发入口的这些请求通过”，比直接宣布“线上入口已经可靠”更有用。这种描述不是保守措辞，而是把证据范围对应到实际请求路径。

## 18.7 清理、常见错误与练习

结束本部分后执行 `bash labs/03-envoy-gateway/cleanup.sh`，它只调用 `kind delete cluster --name envoy-book`。这会删除该独立集群全部实验数据，无法依靠集群恢复；清单在仓库中，可重新创建。不要用删除所有 Namespace、所有 CRD 或当前 context 全部资源的宽泛命令代替。

`no matches for kind` 常表示 CRD 尚未安装或连错集群；`ImagePullBackOff` 先看 describe Pod 事件；curl 连接拒绝先看 port-forward 是否仍运行；返回404先核对 Host、path、listener 与 Route 附着；返回503再查端点与上游连接。按层定位能避免在错误位置反复改配置。

练习：连续两次 apply base.yaml 是否应创建两倍资源？答案：不应，同一作用域内同名对象会被更新；声明式操作应趋向相同目标。但若手工更换名字，会产生新对象，因为资源身份变了。再问：删除后端一个 Pod 后，下一次请求一定落到另一个旧 Pod 吗？答案：不一定，存在状态传播与新Pod恢复过程；应观察 Ready 端点以及真实响应，不能用单次结果推断整个调度行为。

## 本章依据

- [v1.9.2 官方 quickstart](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/quickstart.md)：OCI 安装与无 LoadBalancer 的 port-forward 路径。
- [v1.9.2 Helm 安装文档](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/install/install-helm.md)：CRD 默认安装及已有 CRD 的注意事项。
- [v1.9.2 版本兼容矩阵](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/news/releases/matrix.md)。

[上一章](17-gateway-api.md) · [下一章](19-routing-and-policies.md)
