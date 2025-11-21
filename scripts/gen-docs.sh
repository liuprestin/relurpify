#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCS_DIR="${DOCS_DIR:-$ROOT_DIR/docs}"
CACHE_DIR="${GOCACHE:-$ROOT_DIR/.gocache}"
MODCACHE_DIR="${GOMODCACHE:-$ROOT_DIR/.gomodcache}"
GOLDS_BIN="${GOLDS_BIN:-$(go env GOPATH)/bin/golds}"
ARCHITECTURE_MD="$ROOT_DIR/ARCHITECTURE.md"
ARCHITECTURE_HTML="$DOCS_DIR/architecture.html"

if [ ! -x "$GOLDS_BIN" ]; then
  echo "golds binary not found at $GOLDS_BIN" >&2
  echo "Install via: go install go101.org/golds@latest" >&2
  exit 1
fi

mkdir -p "$DOCS_DIR" "$CACHE_DIR" "$MODCACHE_DIR"

(
  cd "$ROOT_DIR"
  GOCACHE="$CACHE_DIR" GOMODCACHE="$MODCACHE_DIR" "$GOLDS_BIN" -gen -dir="$DOCS_DIR" ./...
)

if [ -f "$ARCHITECTURE_MD" ]; then
  if command -v python3 >/dev/null 2>&1; then
    ARCHITECTURE_MD="$ARCHITECTURE_MD" ARCHITECTURE_HTML="$ARCHITECTURE_HTML" python3 <<'PY'
import html
import os
from pathlib import Path

source = Path(os.environ["ARCHITECTURE_MD"])
target = Path(os.environ["ARCHITECTURE_HTML"])
lines = source.read_text(encoding="utf-8").splitlines()

body = []
in_list = False
in_code = False

def close_list():
    global in_list
    if in_list:
        body.append("</ul>")
        in_list = False

for raw in lines:
    line = raw.rstrip("\n")
    if line.strip().startswith("```"):
        close_list()
        if in_code:
            body.append("</pre>")
            in_code = False
        else:
            body.append("<pre>")
            in_code = True
        continue
    if in_code:
        body.append(html.escape(line))
        continue
    stripped = line.strip()
    if not stripped:
        close_list()
        continue
    if stripped.startswith("#"):
        close_list()
        level = min(len(stripped) - len(stripped.lstrip("#")), 6)
        content = stripped[level:].strip()
        body.append(f"<h{level}>{html.escape(content)}</h{level}>")
        continue
    if stripped[:1] in {"-", "*"}:
        if not in_list:
            in_list = True
            body.append("<ul>")
        body.append(f"<li>{html.escape(stripped[1:].strip())}</li>")
        continue
    close_list()
    body.append(f"<p>{html.escape(stripped)}</p>")

close_list()
if in_code:
    body.append("</pre>")

html_doc = """<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8"/>
    <title>Architecture Outline</title>
    <style>
      body {{ font-family: system-ui, -apple-system, "Segoe UI", sans-serif; margin: 2rem; line-height: 1.6; }}
      pre {{ background: #111; color: #f5f5f5; padding: 1rem; overflow-x: auto; }}
      ul {{ padding-left: 1.3rem; }}
      .nav {{ margin-bottom: 1.5rem; }}
    </style>
  </head>
  <body>
    <div class="nav"><a href="index.html">&larr; Package Index</a></div>
    {body}
  </body>
</html>
""".format(body="\n    ".join(body))
target.write_text(html_doc, encoding="utf-8")
PY
  else
    {
      echo "<!doctype html>"
      echo "<html lang=\"en\"><head><meta charset=\"utf-8\"/>"
      echo "<title>Architecture Outline</title></head><body>"
      echo "<div class=\"nav\"><a href=\"index.html\">&larr; Package Index</a></div>"
      echo "<pre>"
      sed 's/&/\&amp;/g; s/</\&lt;/g' "$ARCHITECTURE_MD"
      echo "</pre></body></html>"
    } >"$ARCHITECTURE_HTML"
    echo "python3 not found; wrote preformatted architecture outline." >&2
  fi
fi

echo "Documentation generated at $DOCS_DIR"
