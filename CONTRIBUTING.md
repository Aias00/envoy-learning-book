# 维护与复现

权威正文在 book/，完整实验在 labs/。修改概念时同步检查实验、源码索引与版本说明。先固定目标版本，再引用源码；不要把上游 main 的行为写成固定 tag 已支持的功能。

## 内容检查与网页

```sh
uv venv .venv
uv pip install --python .venv/bin/python -r requirements.txt
python3 scripts/check_book.py
.venv/bin/mkdocs build --strict
```

结构检查覆盖章节数量、基本练习与答案入口、相对链接和代码围栏；它不能替代技术审校。网页中指向 labs/ 或根目录文件的链接由构建钩子改为 GitHub 链接，源码树中的相对链接保持可用。

## 离线电子书

需要 Node.js 22+、npm 和 Pandoc 3+；首次安装 Mermaid CLI 会下载用于绘图的 Chromium。图表从正文 Mermaid 代码渲染成 SVG，嵌入离线 HTML 和 EPUB，不依赖在线脚本。

```sh
npm ci
python3 scripts/render_diagrams.py
python3 scripts/export_book.py
node scripts/export_pdf.mjs
```

输出位于 dist/：完整 Markdown、单文件 HTML、EPUB，以及可选的 PDF。PDF 使用本机字体生成；若在 Linux 构建，先安装支持中文的字体，如 Noto Sans CJK。正文间的链接改为书内锚点，实验/源码树链接指向公开仓库；离线运行实验仍需提前下载仓库及镜像/依赖。EPUB 的可折叠答案展开显示，适配不支持 details 的阅读器。

网页也使用提交在 book/assets/diagrams 的 SVG，不依赖外部 Mermaid CDN。改动图表后先重新渲染并提交新资产，再构建网页。dist 中的 Markdown 与 diagrams/ 应一起保存；HTML 与 EPUB 已内嵌图片，可单独分发。

## 实验验证

```sh
cd labs/02-xds
go test -race ./...
go vet ./...
```

容器与 Kubernetes 端到端入口见各实验 smoke.sh；TLS 另用 labs/03-envoy-gateway/tls.sh。修改后把真实结果记录到 VALIDATION.md，包含版本、执行范围、失败原因及限制。不要把“预期输出”当作“已执行结果”。

清理只针对对应 Compose 项目和 kind-envoy-book；不要在系统或工作区根目录做广泛删除。私钥只在临时目录与独立实验 Secret 中生成，不提交 Git。
