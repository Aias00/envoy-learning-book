# 验证记录

验证日期：2026-10-20。状态以本文件各项范围为准；正文的预期输出不是额外测试证据。

## 已执行

- 阅读两个上游固定版本，确认源码链接目标、go-control-plane 根/子模块依赖和 EG 兼容矩阵。
- 静态实验：实际拉取 Envoy v1.36.0 与 nginx:1.29.2-alpine；Envoy `--mode validate`；smoke 验证默认 blue、/green 为 green、Admin LIVE。
- 动态实验：Go 1.25.6；`go test -race ./...`、`go vet ./...`；Envoy bootstrap validate；实际构建容器并完成 blue → green → blue、非法 purple 返回 400、Admin LIVE。观察到四类资源 v2/v3 的真实 Envoy ACK、update_rejected 计数为 0。
- 独立审阅基础/综合篇和两个 Go/Compose 实验：修复单文件 bind mount 原子保存后 restart 可能读旧文件的问题，统一用 force-recreate；补充综合蓝绿项目独立 selector/label，说明动态实验共享网络命名空间。
- EG 八章与实验：固定 tag 源码链接、YAML 结构和 CRD 字段校验；实验脚本 bash 语法检查。真实运行仍在验证中，见下节。
- MkDocs strict 构建成功；完整 Markdown、离线 HTML 与 EPUB 已首次导出，图表以嵌入 SVG 呈现。后续修改后还会重新验证最终产物。

## 当前仍在验证

- EG：独立 kind-envoy-book 已创建，Kubernetes v1.34.0 节点 Ready；Helm 拉取 Docker Hub OCI chart 被网络连接重置阻断，正在使用不改动用户全局配置的替代下载路径排查。此时尚不能声称 EG 业务链路或 TLS 实验运行通过。
- 最终电子书完整性、浏览器呈现、CI 与 GitHub 提交/Release 尚待最终审计。

## 环境问题与处理

Docker 最初未运行，已启动本机 OrbStack。公开镜像拉取遇到 macOS keychain 锁定时，用临时独立 Docker 配置与显式 socket 运行，不修改原有凭据配置。Go 默认模块代理曾遇到网络超时，宿主下载临时使用 GOPROXY=goproxy.cn；容器构建默认代理成功。源码 git clone 受连接重置影响，改为下载同一 tag 的官方 codeload tarball，commit 通过 GitHub API 核对。

静态实验首次真实请求发现双栈 DNS 与 IPv4-only 后端不匹配，修复为 V4_ONLY 后复测通过。Shell 脚本发现变量紧邻中文标点可能被 Bash 解析为变量名的一部分，改用大括号并核对所有脚本。

## 验证边界

内存 gRPC 测试验证本书客户端与服务端交互，不等于 Envoy 语义测试，因此另有真实容器 smoke。`Consistent()` 和 YAML/CRD 结构检查不能证明所有业务正确。只验证本书所列场景，不表示对两个上游项目执行过完整测试、性能测试、故障注入、多租户安全审计或生产部署验证。
