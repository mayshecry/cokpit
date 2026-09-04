#!/usr/bin/env python3

from pathlib import Path

HERE = Path(__file__).resolve().parent


def main() -> None:
    html = (HERE / "index.html").read_text(encoding="utf-8")
    css = (HERE / "styles.css").read_text(encoding="utf-8")
    js_parts = []
    for name in ("core", "notifications", "auth", "orders", "detail",
                  "checklists", "manuals", "users", "config", "app"):
        js_parts.append((HERE / "js" / (name + ".js")).read_text(encoding="utf-8"))
    js = "\n\n".join(js_parts)

    html = html.replace(
        '<link rel="stylesheet" href="styles.css">',
        "<style>\n" + css + "\n</style>",
    )
    html = html.replace(
        '<script src="https://cdnjs.cloudflare.com/ajax/libs/xlsx/0.18.5/xlsx.full.min.js"></script>\n',
        "",
    )
    html = html.replace(
        '  <script src="js/core.js"></script>\n'
        '  <script src="js/notifications.js"></script>\n'
        '  <script src="js/auth.js"></script>\n'
        '  <script src="js/orders.js"></script>\n'
        '  <script src="js/detail.js"></script>\n'
        '  <script src="js/checklists.js"></script>\n'
        '  <script src="js/manuals.js"></script>\n'
        '  <script src="js/users.js"></script>\n'
        '  <script src="js/config.js"></script>\n'
        '  <script src="js/app.js"></script>',
        "<script>\n" + js + "\n</script>",
    )

    out = HERE / "cockpit-standalone.html"
    out.write_text(html, encoding="utf-8")
    print(f"wrote {out} ({len(html):,} bytes)")


if __name__ == "__main__":
    main()
