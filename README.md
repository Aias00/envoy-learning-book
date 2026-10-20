# 从零读懂 Envoy：代理基础、Go 控制平面与 Envoy Gateway

一本面向 Envoy 初学者的中文学习书。围绕 [envoyproxy/go-control-plane](https://github.com/envoyproxy/go-control-plane) 和 [envoyproxy/gateway](https://github.com/envoyproxy/gateway)，从一次 HTTP 请求讲起，走到动态配置、Kubernetes 网关和源码阅读。正文、完整实验与练习答案一起维护。

**从这里开始：[在线阅读](https://aias00.github.io/envoy-learning-book/) · [GitHub 全书目录](book/index.md) · [下载电子书](https://github.com/Aias00/envoy-learning-book/releases/latest)。** 26 章正文约 4.7 万汉字，附四周学习计划、术语表和练习答案。不需要先读懂两个仓库；前七章会补上网络、代理与 Envoy 的基础。会用终端即可开始，Go 和 Kubernetes 分别在对应篇章引入。

## 你会得到什么

| 内容 | 阅读入口 |
| --- | --- |
| 第一篇，01–07 章：请求、代理、资源图、静态配置 | [从一次请求开始](book/01-foundations/01-one-request.md) |
| 第二篇，08–15 章：xDS、Go、SnapshotCache、服务端与源码 | [xDS 协议](book/02-go-control-plane/08-xds-protocol.md) |
| 第三篇，16–23 章：Kubernetes、Gateway API、EG、策略与源码 | [Kubernetes 基础](book/03-envoy-gateway/16-kubernetes-basics.md) |
| 第四篇，24–26 章：排障、综合项目与复习路线 | [分层排障](book/04-practice/24-troubleshooting.md) |
| 三个配套实验 | [静态代理](labs/01-static-envoy/README.md) · [动态 xDS](labs/02-xds/README.md) · [Kubernetes 网关](labs/03-envoy-gateway/README.md) |
| 附录 | [术语表](book/appendices/glossary.md) · [源码索引](book/appendices/source-index.md) · [版本说明](book/appendices/versions.md) |

## 阅读和构建

可以直接在 GitHub 阅读 Markdown。离线书籍文件位于 [Releases](https://github.com/Aias00/envoy-learning-book/releases)：PDF 适合打印，EPUB 适合电子书阅读器，单文件 HTML 适合浏览器离线搜索。网页版本提供全书导航、中英文全文搜索和代码复制。

本地可搜索网页：

```sh
uv venv .venv
uv pip install --python .venv/bin/python -r requirements.txt
.venv/bin/mkdocs serve
```

打开命令输出的 `http://127.0.0.1:8000`。不使用 uv 时，可用 `python3 -m venv .venv`，再运行 `.venv/bin/pip install -r requirements.txt`。构建与验证命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 版本与验证

正文固定 Envoy Gateway **v1.9.2**、go-control-plane 根模块 **v0.14.0**；后者依赖的 Envoy API Go 子模块为 **v1.36.0**。这是两条学习路径各自的基线，并不表示 EG v1.9.2 内部使用同一组依赖。EG 的实际兼容矩阵见[版本说明](book/appendices/versions.md)。

实验执行证据和限制记录在 [VALIDATION.md](VALIDATION.md)。请先按实验 README 检查工具和端口；教程里的“预期输出”是学习目标，是否实测以验证记录为准。

## 资料性质与许可

这是独立编写的学习资料，不是 Envoy 官方文档或官方出版物。原理解释、实验和源码导航围绕固定上游版本展开，生产部署应核对当前官方兼容矩阵与安全公告。正文和示例代码采用 [Apache-2.0](LICENSE)，上游引用与归属见 [NOTICE](NOTICE)。
