# 验证记录

验证日期：2026-10-20。状态以本文件各项范围为准；正文的预期输出不是额外测试证据。

## 已执行

- 阅读两个上游固定版本，确认源码链接目标、go-control-plane 根/子模块依赖和 EG 兼容矩阵。
- 静态实验：实际拉取 Envoy v1.36.0 与 nginx:1.29.2-alpine；Envoy `--mode validate`；smoke 验证默认 blue、/green 为 green、Admin LIVE。
- 动态实验：Go 1.25.6；`go test -race ./...`、`go vet ./...`；Envoy bootstrap validate；实际构建容器并完成 blue → green → blue、非法 purple 返回 400、Admin LIVE。观察到四类资源 v2/v3 的真实 Envoy ACK、update_rejected 计数为 0。
- 独立审阅基础/综合篇和两个 Go/Compose 实验：修复单文件 bind mount 原子保存后 restart 可能读旧文件的问题，统一用 force-recreate；补充综合蓝绿项目独立 selector/label，说明动态实验共享网络命名空间。
- EG 八章与实验：实际安装官方 v1.9.2 Helm chart，HTTP 200、未知 Host 404、请求头路由、本地限流 429、HTTPS 证书和主机名验证、JSON 访问日志、Prometheus 指标、12 资源服务器 dry-run 通过；修复后的最终 smoke 与 TLS 脚本重复执行均退出 0。详细环境、chart 与镜像 digest、TLS 反例、未测范围见 [实验三记录](labs/03-envoy-gateway/VALIDATION.md)。
- MkDocs strict 构建和内部链接检查成功；浏览器确认中文正文、SVG 图、折叠答案列表与中英文搜索结果可用。完整 Markdown、离线 HTML、EPUB 和 PDF 已导出；PDF 83 页，文字提取可找到正文与全部四附录，抽查页面中文与代码正常。EPUB ZIP 完整性通过。

## 远端与发布

- 首次书稿提交已推送到公开仓库 Aias00/envoy-learning-book。本机 Git HTTPS 网络失败后，在任务专用容器内完成同一个本地 commit 的正常 push，未改写历史。
- 首轮远端 CI：book 与 go-and-compose 成功；gateway 在运行前因 runner 未安装 ripgrep 失败，已为该 job 显式安装 ripgrep/jq。主编独立重跑修订后的本地 EG smoke 与 TLS，两项退出 0。
- 修订版本 `77c11c86ab12db4cbc08f44666fac5d160a95d2e` 的 [CI 39396463317](https://github.com/Aias00/envoy-learning-book/actions/runs/39396463317)：book、go-and-compose、gateway 三个 job 全部 success，gateway 包含真实 kind、Helm、HTTP 和 TLS。
- [Pages 部署 39396463311](https://github.com/Aias00/envoy-learning-book/actions/runs/39396463311) success；浏览器实际打开 https://aias00.github.io/envoy-learning-book/ ，核对标题、正文与图片加载。
- 首版电子书发布目标为 [v1.0.0](https://github.com/Aias00/envoy-learning-book/releases/tag/v1.0.0)，附件为83页PDF、EPUB、离线HTML、含图片的Markdown ZIP和SHA256校验文件。构建与内容完整性证据见上文；远端附件以该发布页面为准。
- 发布版本 `1a2517e6771670e9138e2a0ea2e5619df5d3df77` 的 [CI 39399216174](https://github.com/Aias00/envoy-learning-book/actions/runs/39399216174) 与 [Pages 39399216148](https://github.com/Aias00/envoy-learning-book/actions/runs/39399216148) 均 success。v1.0.0 为非草稿公开发布，实际重新下载全部5附件；远端SHA256SUMS与本地产物一致，四个书籍文件逐个校验OK。

## 清理

两个 Compose 实验容器与网络已移除；2026-10-20 主编按 cleanup.sh 删除本次创建的唯一独立 kind 集群 envoy-book，随后 `kind get clusters` 返回 No kind clusters found。书稿、声明文件与镜像缓存保留，实验可从源码重新创建。电子书构建产物位于本地 dist/（不纳入 Git，由 Release 分发）。

kind 没有 LoadBalancer 地址，Gateway Programmed=False / AddressNotAssigned；port-forward 的真实请求通过。这不代表公网入口、DNS、云负载均衡、源地址或高可用已验证。

## 环境问题与处理

Docker 最初未运行，已启动本机 OrbStack。公开镜像拉取遇到 macOS keychain 锁定时，用临时独立 Docker 配置与显式 socket 运行，不修改原有凭据配置。Go 默认模块代理曾遇到网络超时，宿主下载临时使用 GOPROXY=goproxy.cn；容器构建默认代理成功。源码 git clone 受连接重置影响，改为下载同一 tag 的官方 codeload tarball，commit 通过 GitHub API 核对。

静态实验首次真实请求发现双栈 DNS 与 IPv4-only 后端不匹配，修复为 V4_ONLY 后复测通过。Shell 脚本发现变量紧邻中文标点可能被 Bash 解析为变量名的一部分，改用大括号并核对所有脚本。

## 验证边界

内存 gRPC 测试验证本书客户端与服务端交互，不等于 Envoy 语义测试，因此另有真实容器 smoke。`Consistent()` 和 YAML/CRD 结构检查不能证明所有业务正确。只验证本书所列场景，不表示对两个上游项目执行过完整测试、性能测试、故障注入、多租户安全审计或生产部署验证。
