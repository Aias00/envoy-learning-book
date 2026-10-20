#!/usr/bin/env python3
"""Check the authored deliverables and local Markdown links, without network access."""
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
errors = []
chapters = sorted((ROOT / "book").glob("[0-9][0-9]-*/*.md"))
numbers = [int(p.name.split("-", 1)[0]) for p in chapters]
if numbers != list(range(1, 27)):
    errors.append(f"Expected exactly chapters 01–26, found {numbers}")
for chapter in chapters:
    body = chapter.read_text()
    chinese = len(re.findall(r"[\u4e00-\u9fff]", body))
    if chinese < 900:
        errors.append(f"Chapter suspiciously short ({chinese} Chinese chars): {chapter.relative_to(ROOT)}")
    if "练习" not in body and "自测" not in body and "验收" not in body:
        errors.append(f"Missing learner exercise: {chapter.relative_to(ROOT)}")
    if "答案" not in body:
        errors.append(f"Missing answer guidance: {chapter.relative_to(ROOT)}")
for path in ROOT.rglob("*.md"):
    if any(p in {".sources", ".venv", "node_modules", "site", "dist", ".git"} for p in path.parts):
        continue
    body = path.read_text()
    if len(re.findall(r"^```", body, flags=re.M)) % 2:
        errors.append(f"Unbalanced fenced code blocks: {path.relative_to(ROOT)}")
    prose = re.sub(r"```[^\n]*\n.*?^```[^\n]*$", "", body, flags=re.M | re.S)
    for target in re.findall(r"!?\[[^\]\n]*\]\(([^)\s]+)(?:\s+[^)]*)?\)", prose):
        target = target.strip("<>")
        parts = urlsplit(target)
        if parts.scheme or parts.netloc or not parts.path:
            continue
        resolved = (path.parent / unquote(parts.path)).resolve()
        if not resolved.exists():
            errors.append(f"Broken local link {path.relative_to(ROOT)} -> {target}")
for lab in ["01-static-envoy", "02-xds", "03-envoy-gateway"]:
    for filename in ["README.md", "smoke.sh"]:
        if not (ROOT / "labs" / lab / filename).is_file():
            errors.append(f"Missing labs/{lab}/{filename}")
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
total = sum(len(re.findall(r"[\u4e00-\u9fff]", p.read_text())) for p in chapters)
print(f"PASS: 26 chapters, {total:,} Chinese characters; local links, fences and lab entrypoints checked.")
print("This structural check does not prove technical accuracy or end-to-end runtime behavior.")
