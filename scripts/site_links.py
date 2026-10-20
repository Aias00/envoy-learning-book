"""MkDocs hook: route links outside docs_dir to the public source repository."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
BOOK = ROOT / "book"

def on_page_markdown(markdown, page, **kwargs):
    current = BOOK / page.file.src_path
    def rewrite(match):
        label, target = match.groups()
        if re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", target) or target.startswith("#"):
            return match.group(0)
        local = (current.parent / target.split("#", 1)[0]).resolve()
        if local.is_relative_to(ROOT) and not local.is_relative_to(BOOK):
            return f"[{label}](https://github.com/Aias00/envoy-learning-book/blob/main/{local.relative_to(ROOT).as_posix()})"
        return match.group(0)
    parts = re.split(r"(^```[^\n]*\n.*?^```[^\n]*$)", markdown, flags=re.M | re.S)
    for i in range(0, len(parts), 2):
        parts[i] = re.sub(r"\[([^\]\n]+)\]\(([^)\s]+)\)", rewrite, parts[i])
    return "".join(parts)
