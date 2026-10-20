# 实验 02：用 Go ADS 控制平面切换蓝绿后端

目标是在 Envoy 不重启的情况下，把 `curl localhost:18080` 从 `blue` 切到 `green`。先准备 Docker Engine / Docker Desktop / OrbStack、Docker Compose v2、宿主机 `curl` 和可下载依赖的网络。用容器运行实验不要求宿主机安装 Go；阅读、修改和测试 Go 代码时安装 Go 1.25 或更新版本。本实验以 Go 1.25.6 为验证基线。

固定版本：Envoy 镜像 `envoyproxy/envoy:v1.36.0`、Go 构建镜像 `golang:1.25.6-alpine`、根模块 `github.com/envoyproxy/go-control-plane v0.14.0`、独立生成代码模块 `github.com/envoyproxy/go-control-plane/envoy v1.36.0`。不要把两个 Go 模块的版本号互换；`go.sum` 校验依赖下载内容。

## 启动、切换与预期结果

在仓库根目录执行：

```sh
cd labs/02-xds
docker compose up --build -d
curl http://localhost:19000/healthz
curl http://localhost:18080/
curl -X POST 'http://localhost:19000/switch?backend=green'
curl http://localhost:18080/
```

全新启动时依次预期 `ok`、`blue`、`{"backend":"green","version":"v2"}`、`green`。xDS 是异步推送，切换后可能需要稍等再请求；HTTP 发布成功不代表 Envoy 已经 ACK。容器可能已经在运行，此时版本号不一定是 `v2`。控制平面重启会恢复初始 `blue` / `v1`。

切回与非法输入：

```sh
curl -X POST 'http://localhost:19000/switch?backend=blue'
curl -i -X POST 'http://localhost:19000/switch?backend=purple'
curl -i 'http://localhost:19000/switch?backend=green'
```

合法值只有精确的 `blue`、`green`；非法值或缺少参数返回 `400`，非 POST 返回 `405`，不会发布快照或递增版本。每次合法 POST（包括重复颜色）都会发布一个新版本。

完整集成检查会等待服务、重设 blue、切换 green、检查非法输入与 Envoy 就绪：

```sh
./smoke.sh
```

成功时打印 `PASS: blue -> green, invalid backend rejected, Envoy ready`；默认结束后删除本项目容器与网络。`KEEP_LAB=1 ./smoke.sh` 保留环境。

## 端口与进程

| 宿主机地址 | 用途 |
| --- | --- |
| `127.0.0.1:18080` | Envoy 数据面 HTTP，映射容器 `8080`。 |
| `127.0.0.1:19000` | Go 教学管理 HTTP：`POST /switch`、`GET /healthz`。 |
| `127.0.0.1:19901` | Envoy admin，映射容器 `9901`。 |

ADS gRPC 只在 Compose 网络的 `control-plane:18000` 提供，未发布到宿主机。`blue`、`green` 容器运行同一个小 Go 程序的后端模式，分别监听 `8081`、`8082` 并返回颜色。

它们通过 Compose 的 `network_mode: service:envoy` 与 Envoy 共享网络命名空间。因此 EDS 可以使用数值地址 `127.0.0.1:8081`、`127.0.0.1:8082`，不依赖把 DNS 名字塞进 EDS 地址后让 Envoy解析。这里的 loopback 属于容器网络命名空间；真实控制平面必须发布 Envoy 可达的端点 IP。本机端口映射都限制在 loopback，但同一 Compose 网络仍能访问管理服务，这是教学隔离边界。

## 文件与资源关系

`envoy.yaml` 是 bootstrap：声明 `node.id: envoy-book`，建立到 `xds_cluster` 的 HTTP/2 gRPC 连接，并让 CDS、LDS 使用 ADS。EDS 与 RDS 的订阅入口在控制平面下发的 Cluster / Listener 资源内。

`controlplane.go` 构造四类 v3 资源并管理快照发布；`main.go` 启动 gRPC、HTTP 与日志；`controlplane_test.go` 用实际 protobuf、缓存和内存 gRPC 连接验证行为。`Dockerfile` 先编译静态 Go 程序，再复制到无 shell 的 scratch 运行镜像。

资源引用固定如下：

```text
LDS listener_0 (:8080)
  └─ HTTP connection manager -> RDS book_routes
       └─ route / -> blue_backend 或 green_backend
CDS blue_backend  -> EDS blue_backend  -> 127.0.0.1:8081
CDS green_backend -> EDS green_backend -> 127.0.0.1:8082
```

两套 CDS/EDS 始终存在，切换只改变 RDS 选择的 cluster。为了让流程直观，每次重新生成整个 SotW 快照，四种类型都使用同一个新的版本字符串（`v1`、`v2`……）；这不表示所有资源内容都改变。SotW 返回订阅范围内该资源类型的完整状态，并不是字段级补丁。

发布顺序是：校验颜色 → 生成 protobuf 资源 → `NewSnapshot` → 显式 `snapshot.Consistent()` → `SetSnapshot(ctx, "envoy-book", snapshot)`。`NewSnapshot` 不会自动调用一致性检查。`Consistent` 检查 CDS→EDS、LDS→RDS 等引用完整性，不能代替 Envoy 的语义校验，也不会全面检查每条路由引用的 cluster；测试额外验证了本实验的路由目标。

`NewSnapshotCache(true, cache.IDHash{}, nil)` 选择 ADS 模式，以 Node.ID 为分组键，必须与 bootstrap 的 `envoy-book` 完全一致。`registerADS` 把 `server.NewServer` 注册为 gRPC 的 `AggregatedDiscoveryService`。仅创建一个 Go server 对象并不会监听网络。

管理发布使用互斥锁串行执行，避免并发请求让旧版本覆盖新版本。发布的 protobuf 对象随后保持不变。上游 `SetSnapshot` 会先写入缓存再通知 watch，因此错误不意味着可自动回滚；示例保留版本递增，并在错误时报告缓存可能已更新，便于排查。快照版本属于应用数据；nonce 属于某次流上的响应，不能混用。

## 观察推送与 ACK

```sh
docker compose logs --tail=100 control-plane
docker compose logs --tail=100 envoy
curl http://localhost:19901/ready
curl http://localhost:19901/config_dump
curl http://localhost:19901/clusters
curl 'http://localhost:19901/stats?filter=update_rejected'
```

控制平面日志的 `send` 包含 type URL、version 和 nonce；收到带 `response_nonce` 且没有 `error_detail` 的请求时记录 `ACK`，有错误时记录 `NACK`。这是教学观察日志，不是完整的响应关联或可靠发布状态系统。ACK 表示 Envoy 接受了该响应，不表示所有业务请求都成功。

`/healthz` 只表示控制平面 HTTP 进程可服务；Envoy admin `/ready` 和实际数据面请求才帮助判断代理与流量情况。`config_dump` 中的动态 listener、route、cluster 能确认 Envoy 当前接收了什么，不能只看管理端 HTTP 200。

## 本机测试与配置验证

```sh
go test -race ./...
go vet ./...
docker compose config --quiet
docker compose run --rm --no-deps envoy --mode validate -c /etc/envoy/envoy.yaml
```

单元/协议测试验证资源 protobuf 与引用、Node.ID 分组、HTTP 更新版本、非法输入不发布、真实 gRPC ADS 收到 v1/v2 并发送 ACK，以及后端响应。内存 gRPC 测试没有启动 Envoy，因此还需要 `./smoke.sh` 验证代理接受动态资源与真实流量切换。

若 Go 的默认模块代理在本地网络不可达，可以临时为单条命令选择自己信任的模块代理，例如 `GOPROXY=https://goproxy.cn,direct go mod download`。无需改用户全局 Go 配置；依赖仍由 `go.sum` 校验。

## 排错与清理

| 现象 | 检查与处理 |
| --- | --- |
| `18080` 连接被拒绝 | 动态 listener 尚未就绪；检查控制平面日志和 bootstrap 的 ADS cluster 地址。 |
| 没有任何资源下发 | 比较 `node.id` 与缓存发布的 Node.ID，检查 HTTP/2 与 ADS gRPC 注册。 |
| `NACK` / `update_rejected` | 阅读 Envoy 拒绝的字段路径，查看控制平面日志中的 `error_detail`。 |
| `503 no healthy upstream` | 用 admin `/clusters` 检查端点；确认 blue/green 容器运行且共享 Envoy 网络。 |
| POST 成功但请求仍旧颜色 | 等待异步传播，检查 ACK、`config_dump` 与实际目标；发布成功不是流量已切换承诺。 |
| 版本重新从 `v1` 开始 | 控制平面进程重启；本例没有持久化。 |
| 宿主端口冲突 | 检查 `18080`、`19000`、`19901`，修改 Compose 左侧映射后同步 curl 地址。 |
| Docker 凭据或镜像下载失败 | 检查 Docker 登录/凭据助手与镜像网络；不属于 xDS 协议错误。 |

```sh
docker compose down --remove-orphans
```

只清理本项目容器与网络，不删除镜像或源码。本例省略 TLS/mTLS、管理鉴权、租户隔离、配置持久化、ACK/NACK 状态存储、回滚策略与高可用；不得直接用于生产。管理 HTTP、admin 和 ADS 都属于需要隔离的控制接口。
