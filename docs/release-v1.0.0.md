# 《从零读懂 Envoy》首版

面向 Envoy 零基础读者，26 章正文约 4.7 万汉字，按请求与代理基础、Go xDS 控制平面、Envoy Gateway、排障与综合实践展开。包含练习答案、四周学习计划、术语表、82 个固定版本源码路径与三个完整实验。

在线阅读：https://aias00.github.io/envoy-learning-book/

下载 PDF（83 页）、EPUB、单文件 HTML；Markdown ZIP 包含配套 SVG 图片。所有图表随文件提供，不依赖在线 Mermaid 脚本。运行实验还需下载仓库、依赖与镜像。

基线：Envoy Gateway v1.9.2、go-control-plane 根模块 v0.14.0；Go 实验的 Envoy API 子模块与代理为 v1.36.0。EG 路径使用其官方兼容组合，不与 Go 实验版本混用。

验证：静态代理、Go race/vet、真实 Envoy ADS 蓝绿更新、Gateway API 转发/404/header/429、TLS 证书和主机名、日志/指标均有真实执行记录。GitHub CI 同时验证书稿、Go/Compose、Kubernetes 与 TLS。

详细证据与边界见仓库 VALIDATION.md；kind 使用 port-forward 验证，不代表公网负载均衡或生产高可用已验证。这是独立中文学习资料，不是 Envoy 官方出版物。
