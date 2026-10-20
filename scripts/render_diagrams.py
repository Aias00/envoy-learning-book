#!/usr/bin/env python3
"""Render committed book SVGs so the website works without a Mermaid CDN."""
from pathlib import Path
import hashlib
import re
import subprocess
import tempfile
import sys

ROOT = Path(__file__).resolve().parents[1]
output = ROOT / "book/assets/diagrams"
output.mkdir(parents=True, exist_ok=True)
count = 0
with tempfile.TemporaryDirectory(prefix="envoy-book-diagrams-") as directory:
    for page in sorted((ROOT / "book").rglob("*.md")):
        for source in re.findall(r"```mermaid\n(.*?)\n```", page.read_text(), flags=re.S):
            digest = hashlib.sha256(source.encode()).hexdigest()[:16]
            target = output / f"{digest}.svg"
            if not target.exists() or "--force" in sys.argv:
                input_file = Path(directory) / f"{digest}.mmd"
                input_file.write_text(source)
                subprocess.run([str(ROOT / "node_modules/.bin/mmdc"), "-i", str(input_file), "-o", str(target), "-b", "transparent", "-c", str(ROOT / "scripts/mermaid-config.json")], check=True)
            count += 1
print(f"{count} diagram sources have generated SVG assets.")
