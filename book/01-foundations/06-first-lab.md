# 06　运行、观察与修改静态代理

本章目标：启动真实 Envoy、区分配置验证与请求验证、通过 Admin 观察生效配置，并进行一次可解释的修改。先修：第 05 章；工具：Docker 引擎和 Compose。所有命令都在仓库根目录开始。

## 先做环境检查

```sh
docker version
docker compose version
docker info
cd labs/01-static-envoy
```

`docker version` 可能只显示客户端，然后报告无法连接引擎；这表示命令存在，但容器运行环境尚未启动。macOS 上应先启动 Docker Desktop 或 OrbStack。不要在看到 Cannot connect to the Docker daemon 后继续猜 Envoy YAML 的问题。实验使用 18000 与 19900 两个本机端口，端口占用时先定位已有进程，再决定是否修改本实验映射。

完整文件和最新命令见[实验 README](../../labs/01-static-envoy/README.md)。这里强调每一步要回答的问题。启动通常使用：

```sh
docker compose up --build -d
docker compose ps
docker compose logs envoy
```

`--build` 构建实验需要的镜像，`-d` 让进程在后台运行。`ps` 查看容器是否处于 running，日志检查 Envoy 是否成功加载配置。镜像拉取超时与 Envoy 进程退出是两类问题；前者发生在启动代理之前。第一次启动需要访问镜像仓库，务必保留实际错误信息。

## 从请求走到配置

```sh
curl -v http://127.0.0.1:18000/
curl -fsS http://127.0.0.1:19900/server_info
curl -fsS http://127.0.0.1:19900/config_dump
curl -fsS http://127.0.0.1:19900/clusters
```

第一条验证业务路径，默认应看到后端标识 `blue`。第二条说明回答 Admin 的 Envoy 是什么版本、处于什么运行状态。第三条展示当前配置；请搜索 `blue_backend`，看看路由与 Cluster 是否都出现。第四条帮助观察上游主机状态；仅有 Cluster 名称而没有可用端点，需要继续调查发现与连接。

`-f` 让 curl 在 4xx/5xx 时以失败退出，`-sS` 隐藏进度但保留错误。想看故障响应体时，先去掉 `-f` 或加上明确状态输出，不要把“curl 没打印 body”误读成“没有 HTTP 响应”。

## 改一次你能解释的配置

先确认实验目录中的完整配置同时声明 blue 与 green 后端 Cluster。找到兜底路由的 `cluster: blue_backend`，改成 `green_backend`。这是修改路由选择，两个后端地址都不变。静态配置文件不会因为被挂载就自动重新加载，需要重建这个实验的 Envoy 容器：

```sh
docker compose up -d --force-recreate envoy
curl --retry 10 --retry-connrefused --retry-delay 1 -fsS http://127.0.0.1:18000/
```

再次查看 config_dump，确认 action 已指向 green，再验证响应 `green`。你现在同时拥有文件层、运行配置层和业务请求层的证据。如果只改了文件而没重新加载，config_dump 仍显示旧路由，就能解释为何响应没有变化。这里用 force-recreate 而非 restart，是因为一些编辑器会原子替换文件；重启已有容器可能仍保留单文件挂载的旧 inode，重建会重新挂载当前文件。

练习完成后把默认配置改回 blue，并再次用相同命令重建，方便后续自动化脚本从已知初态开始。编辑默认文件前可以先复制一份自己的笔记版本；不要把改后的文件与书中初始状态混淆。

## 三种有意的故障

第一种：把请求改为访问一个未发布的本机端口。如果出现连接拒绝，检查监听和端口映射。它尚未进入 HTTP 路由。

第二种：暂停一个被选中的后端，再发请求。Envoy 仍可接收连接，但上游请求可能失败。观察 Envoy 日志、Cluster 状态和响应码，感受“两段连接独立”这句话。恢复后端后再继续下一种，避免同时引入多个变量。

第三种：把 route.cluster 改成不存在的名字。有些错误会在加载阶段拒绝，有些会在路由阶段显示为上游/Cluster 问题，具体取决于配置和校验路径。请记录对应版本的真实行为，不要预先强行归类成某个状态码。恢复正确名字再重启，并重新发请求验证。

所有故障练习都只作用于这个独立 Compose 项目。结束后运行：

```sh
docker compose down
```

这会移除当前项目的容器与网络；书稿和配置文件还在，不需要删除镜像缓存。后续 xDS 实验使用不同项目目录与端口。

## 练习与参考答案

1. 容器显示 running，但请求失败，还需要哪些证据？
2. 配置文件已经改成 green，config_dump 仍是 blue，优先怀疑什么？
3. Admin 返回成功是否足以说明业务后端正常？

<details><summary>参考答案</summary>

1. Envoy 日志、监听与映射、实际 HTTP 响应、当前路由/Cluster/端点状态。容器活着不说明每条业务链路健康。
2. 静态配置尚未重新加载，或者编辑了另一份文件、启动的是另一项目。先核对挂载和运行配置，再检查业务层。
3. 不足以。Admin 与业务入口、上游连接是独立路径，应实际访问业务入口并检查后端响应。

</details>

继续：[控制平面](07-control-plane.md)。来源：[完整实验](../../labs/01-static-envoy/README.md)、[Envoy v1.36 运维](https://www.envoyproxy.io/docs/envoy/v1.36.0/operations/operations)、[本书验证记录](../../VALIDATION.md)。
