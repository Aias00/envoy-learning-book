# 附录 D　命令、端口与实验速查

所有命令默认从仓库根目录开始。先读对应 README；下面只是回忆用的索引。

| 实验 | 启动 / 自动验证 | 本机入口 | 清理 |
| --- | --- | --- | --- |
| [01 静态 Envoy](../../labs/01-static-envoy/README.md) | `bash labs/01-static-envoy/smoke.sh` | 业务 18000、Admin 19900 | 在实验目录 `docker compose down` |
| [02 Go xDS](../../labs/02-xds/README.md) | `bash labs/02-xds/smoke.sh` | 业务 18080、管理 19000、Admin 19901 | 在实验目录 `docker compose down` |
| [03 EG](../../labs/03-envoy-gateway/README.md) | `bash labs/03-envoy-gateway/smoke.sh` | 临时转发 18888 | `bash labs/03-envoy-gateway/cleanup.sh` |
| [03 TLS](../../labs/03-envoy-gateway/README.md) | `bash labs/03-envoy-gateway/tls.sh` | 临时转发 18443 | 随独立集群清理；临时私钥另行处理 |

本机端口与容器内部端口可能不同。静态实验后端通过 Compose DNS 服务名访问；动态实验为了直接演示 EDS 的数值地址，让两个玩具后端共享 Envoy 网络命名空间，因此 EDS 里的 127.0.0.1 指向可达的同一环境。不要在普通独立后端容器中照搬这一地址。

## 常用观察

```sh
curl -fsS http://127.0.0.1:19901/server_info
curl -fsS http://127.0.0.1:19901/config_dump
curl -fsS http://127.0.0.1:19901/clusters
curl -fsS -X POST 'http://127.0.0.1:19000/switch?backend=green'
kubectl --context kind-envoy-book -n envoy-book get gateway,httproute
kubectl --context kind-envoy-book -n envoy-book get service,endpointslice
```

## 环境错误与业务错误

如果 Docker 提示 keychain locked，是宿主凭据助手无法读取登录信息，发生在镜像拉取阶段；不要改 Envoy 配置来修复。可在你的桌面会话正常解锁钥匙串后重试。仅拉取公开镜像时，也可临时使用一个不含 credsStore 的独立 Docker 配置，并显式指定当前 Docker socket；保持原用户配置不变。确切做法应按你的运行环境设置，实验脚本不会修改钥匙串或现有凭证。

如果镜像或 Go 模块下载因 DNS、代理、IPv6 或网络断开失败，先用同一个目标验证网络路径，再重试该下载。改用镜像源时应说明来源和版本；不要因为网络超时就随意升级依赖，升级可能引入新的兼容问题。

如果 Gateway API YAML 报 unknown field，检查实际安装 CRD 与正文版本是否一致。脚本可重复执行，但版本升级和 CRD 生命周期仍应按第 23 章处理，不能把教学 setup 脚本当成生产升级工具。
