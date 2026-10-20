# 实验 01：先让一个静态 Envoy 转发请求

这个实验只需要 Docker Engine（或 Docker Desktop / OrbStack）、Docker Compose v2 和宿主机上的 `curl`，不要求先安装 Go。首次运行需要联网下载镜像。先确认：

```sh
docker version
docker compose version
curl --version
```

本目录固定使用 `envoyproxy/envoy:v1.36.0` 与 `nginx:1.29.2-alpine`。Envoy 版本可以这样核对：

```sh
docker run --rm envoyproxy/envoy:v1.36.0 --version
```

预期包含 `/1.36.0/`。实验会占用宿主机 `127.0.0.1:18000` 与 `127.0.0.1:19900`；请先关闭占用它们的进程。端口与实验 02 独立，可以同时运行。

## 启动与验证

在仓库根目录执行：

```sh
cd labs/01-static-envoy
docker compose up --build -d
curl http://localhost:18000/
curl http://localhost:18000/green
curl http://localhost:19900/ready
```

前两个请求分别预期得到 `blue`、`green`，最后一个在 Envoy 就绪后得到 `LIVE`。刚启动时 Docker 已创建容器不代表服务已经就绪，可等待几秒重试，或运行带等待与清理的一键检查：

```sh
./smoke.sh
```

成功时打印 `PASS: / -> blue, /green -> green, Envoy ready`。脚本默认检查后执行 `docker compose down --remove-orphans`；`KEEP_LAB=1 ./smoke.sh` 会保留实验容器方便观察。

## 对照文件理解流量

`compose.yaml` 创建 `blue`、`green` 两个 HTTP 服务和一个 Envoy。Docker 提供内部 DNS，因此 Envoy 可以使用服务名 `blue`、`green` 连接后端。两个后端没有发布宿主机端口，curl 先到 Envoy 再到后端。

`envoy.yaml` 中 `static_resources.listeners` 在容器的 `0.0.0.0:8080` 接收请求；Compose 把宿主机 `18000` 映射到这个端口。listener 内的 `http_connection_manager` 解析 HTTP，`http_filters` 中的 router 执行路由转发。

`route_config` 有一个 `domains: ["*"]` 的虚拟主机。路由按顺序匹配：`/green` 前缀选择 `green_backend`，剩余 `/` 前缀选择 `blue_backend`。前缀匹配意味着 `/green/hello`、`/greenish` 也匹配第一条。更具体的规则必须放在兜底规则之前。此处不改写路径；后端配置对任何路径都返回自身颜色。

`static_resources.clusters` 声明上游连接目标。cluster 名称是路由引用的逻辑名字，`load_assignment.cluster_name` 与它一致；endpoint 的 `blue:8080`、`green:8080` 才是实际网络位置。`STRICT_DNS` 让 Envoy 解析并刷新 Docker 服务名，`connect_timeout: 1s` 是建立上游连接的超时。后端只监听 IPv4，因此示例用 `dns_lookup_family: V4_ONLY`；某些容器网络同时返回 AAAA 记录，若 Envoy 选择 IPv6 地址而后端不监听 IPv6，会出现 `503 UF`。

`admin` 是 Envoy 自身的诊断接口。它在容器内监听 `9901`，宿主机仅 `127.0.0.1:19900` 可访问。容器内监听 `0.0.0.0` 是为了端口映射，不能把这理解成适合把 admin 暴露到公网。

## 看见配置与运行状态

```sh
docker compose ps
docker compose logs --tail=80 envoy
curl http://localhost:19900/config_dump
curl http://localhost:19900/clusters
curl 'http://localhost:19900/stats?filter=upstream_rq'
```

访问日志写入容器标准输出；`config_dump` 显示已加载配置；`clusters` 可以看到后端端点与连接状态；`stats` 可以看到请求计数。修改 `envoy.yaml` 后用 `docker compose up -d --force-recreate envoy` 重新加载本实验的静态配置，再等待 `/ready` 返回 LIVE 并发请求验证。这里强制重建是为了兼容编辑器原子替换文件的保存方式，避免已有容器的单文件 bind mount 仍引用旧文件。下一实验用 xDS 演示运行时更新。

## 常见问题

| 现象 | 检查与处理 |
| --- | --- |
| `Cannot connect to the Docker daemon` | 启动 Docker Desktop / OrbStack，确认 `docker version` 能显示 Server。 |
| `port is already allocated` | 用 `lsof -i :18000`、`lsof -i :19900` 找到占用者，或修改 Compose 的宿主机端口并同步 curl 地址。 |
| 启动后短暂 `503` | 等待后端启动与 DNS 解析；用 `docker compose logs blue green envoy` 检查失败原因。 |
| 始终 `503` | 检查后端端口是 `8080`、cluster 名称与路由一致，并查看 admin `/clusters`。 |
| YAML 修改后 Envoy 退出 | 执行下面的 validate 命令，按日志中的字段路径修正。 |
| 镜像拉取超时或 keychain 报错 | 先检查网络和 Docker 凭据助手；这是本机镜像下载问题，配置验证不能绕过镜像未下载。 |

```sh
docker compose run --rm --no-deps envoy --mode validate -c /etc/envoy/envoy.yaml
```

`validate` 只验证配置能否解析和初始化，不证明后端网络一定可达。`./smoke.sh` 会验证真实请求链路。

## 清理

```sh
docker compose down --remove-orphans
```

这只删除此 Compose 项目的容器与默认网络，不删除镜像、教材源码或其他项目。实验没有持久化卷。示例使用 HTTP 明文和无鉴权 admin，只适合本机学习；生产部署需要单独设计身份认证、TLS、admin 隔离与监控。
