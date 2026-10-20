# Envoy 中文学习书实施计划

> For agentic workers: use parallel, disjoint chapter ownership, followed by independent technical and beginner-readability review. 用户已授权完成教材并提交 GitHub，且明确选择公开仓库。

**Goal:** 将 envoyproxy/gateway 和 envoyproxy/go-control-plane 整理为面向零基础读者、详细可读、配有可运行实验与源码依据的中文书籍，提交到 Aias00/envoy-learning-book。

**Architecture:** Markdown 为权威书稿；MkDocs 提供可搜索网页；Pandoc 导出 EPUB 和单文件 HTML。基础概念先行，然后通过可运行的最小 xDS 控制平面解释 Envoy，再进入 Kubernetes Gateway API 和 Envoy Gateway；每章包含目标、先修、解释、实例、常见错误、练习与答案、固定版本来源。

**Tech Stack:** Markdown、Mermaid、MkDocs Material、Pandoc、Go、Docker Compose、Kubernetes Gateway API。

## 内容与文件分工

- 主编：README、book/index.md、book/01-foundations/（第 01–07 章）、book/04-practice/（第 24–26 章）、附录、构建与发布。
- go-control-plane 作者：book/02-go-control-plane/（第 08–15 章），固定 v0.14.0 源码分析，解释 Envoy proto 子模块独立版本。
- Envoy Gateway 作者：book/03-envoy-gateway/（第 16–23 章），固定 v1.9.2 源码分析和 Kubernetes 实验。
- 实验作者：labs/01-static-envoy、labs/02-xds，Go 自动化测试和容器集成验证入口。

## 工作与验收

- [x] 获取上游固定版本、记录 commit、依赖版本、许可证和追溯链接。
- [x] 编写 26 章中文正文，包含学习目标、先修概念、逐步推导、命令解释、练习和参考答案；内容能脱离源代码独立阅读。
- [x] 提供静态 Envoy、Go xDS 动态更新和 Envoy Gateway Kubernetes 实验；每个实验有前置条件、命令、预期输出、失败排查、清理。
- [x] 验证 Go 构建与资源/更新行为测试，实际执行容器及 Kubernetes/TLS 端到端实验并记录范围。
- [x] 增加术语表、四周学习计划、源码索引、版本维护和许可说明。
- [x] 构建可搜索静态站、单文件 HTML、EPUB、83页PDF；检查内部链接、导航、代码格式和构建结果。
- [x] 独立审校源码准确性、零基础教学连贯性、实验一致性，修复发现的问题。
- [x] 创建公开 GitHub 仓库，提交并推送书稿；远端书稿/Go/Compose/Kubernetes CI均通过，Pages已部署。
- [ ] 发布并下载核验首版电子书附件，确认最终远端 commit。

## 完成证据

在 VALIDATION.md 记录真实执行命令、结果和未执行项。检查公开仓库、默认分支和 Release 附件的可访问性；不能用构建通过代替教学内容的逐项审校。
