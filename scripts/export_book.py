#!/usr/bin/env python3
"""Export one complete manuscript, with rendered diagrams and usable internal links."""
from pathlib import Path
import hashlib
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BOOK = ROOT / "book"
OUT = ROOT / "dist"
CACHE = ROOT / ".diagram-cache"
OUT.mkdir(exist_ok=True)
CACHE.mkdir(exist_ok=True)
files = [BOOK / "index.md", *sorted(BOOK.glob("[0-9][0-9]-*/*.md")), *sorted((BOOK / "appendices").glob("*.md"))]
anchors = {p.resolve(): "section-" + str(i) for i, p in enumerate(files)}
chunks = ["---\ntitle: 从零读懂 Envoy\nsubtitle: 代理基础、Go 控制平面与 Envoy Gateway\nlang: zh-CN\nauthor: Envoy 中文学习资料项目\nrights: Apache-2.0\n---\n"]

for path in files:
    body = path.read_text()
    body = re.sub(r"^# (.+)$", lambda m: f"# {m[1]} {{#{anchors[path.resolve()]}}}", body, count=1, flags=re.M)
    def link(match):
        label, target = match.group(1), match.group(2)
        if re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", target) or target.startswith("#"):
            return match.group(0)
        local = (path.parent / target.split("#", 1)[0]).resolve()
        if local in anchors:
            return f"[{label}](#{anchors[local]})"
        if local.is_relative_to(ROOT):
            remote = "https://github.com/Aias00/envoy-learning-book/blob/main/" + local.relative_to(ROOT).as_posix()
            return f"[{label}]({remote})"
        return match.group(0)
    pieces = re.split(r"(^```[^\n]*\n.*?^```[^\n]*$)", body, flags=re.M | re.S)
    for i in range(0, len(pieces), 2):
        pieces[i] = re.sub(r"\[([^\]\n]+)\]\(([^)\s]+)\)", link, pieces[i])
    body = "".join(pieces)
    def diagram(match):
        source = match.group(1)
        digest = hashlib.sha256(source.encode()).hexdigest()[:16]
        src, svg = CACHE / f"{digest}.mmd", CACHE / f"{digest}.svg"
        if not svg.exists():
            src.write_text(source)
            subprocess.run([str(ROOT / "node_modules/.bin/mmdc"), "-i", str(src), "-o", str(svg), "-b", "transparent"], check=True)
        return f"\n![本节关系图]({svg.as_posix()})\n"
    body = re.sub(r"```mermaid\n(.*?)\n```", diagram, body, flags=re.S)
    body = re.sub(r"</?details[^>]*>", "", body)
    body = re.sub(r"<summary>(.*?)</summary>", r"\n**\1**\n", body)
    chunks.append(body)

manuscript = OUT / "envoy-learning-book.md"
manuscript.write_text("\n\n".join(chunks))
common = ["pandoc", str(manuscript), "--from=markdown+implicit_figures", "--standalone", "--toc", "--toc-depth=2", "--resource-path=" + str(CACHE)]
subprocess.run([*common, "--embed-resources", "--css=" + str(ROOT / "scripts/book.css"), "-o", str(OUT / "envoy-learning-book.html")], check=True)
subprocess.run([*common, "--split-level=1", "--css=" + str(ROOT / "scripts/book.css"), "-o", str(OUT / "envoy-learning-book.epub")], check=True)
print("Exported complete Markdown, standalone HTML and EPUB with embedded SVG diagrams to dist/.")
