package fleet

// baseCSS is shared by the fleet report and the action pages: color tokens
// for light and dark mode (text and links at WCAG AA contrast or better on
// their background), type, links, code and tables.
const baseCSS = `
:root{color-scheme:light dark;
--bg:#ffffff;--fg:#1f2328;--muted:#59636e;--line:#d1d9e0;--surface:#f6f8fa;--hover:#eaeef2;--code:#eff2f5;
--link:#0550ae;--link-visited:#6639ba;--focus:#0969da;--ok:#1a7f37;--warn:#9a6700;--err:#cf222e}
@media (prefers-color-scheme:dark){:root{
--bg:#0d1117;--fg:#e6edf3;--muted:#9198a1;--line:#3d444d;--surface:#151b23;--hover:#212830;--code:#1f2630;
--link:#79c0ff;--link-visited:#d2a8ff;--focus:#58a6ff;--ok:#3fb950;--warn:#d29922;--err:#ff7b72}}
*{box-sizing:border-box}
body{font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif;background:var(--bg);color:var(--fg);margin:0}
h1{font-size:22px;line-height:1.3;margin:0 0 6px}h2{font-size:17px;margin:32px 0 8px}
p{max-width:72rem}.muted{color:var(--muted)}
a{color:var(--link);text-decoration:underline;text-decoration-thickness:1px;text-underline-offset:2px}
a:visited{color:var(--link-visited)}a:hover{text-decoration-thickness:2px}
a:focus-visible,input:focus-visible,th:focus-visible{outline:2px solid var(--focus);outline-offset:2px}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
code{font-size:.88em;background:var(--code);color:var(--fg);padding:.1em .35em;border-radius:4px}
pre{background:var(--code);color:var(--fg);padding:.75rem 1rem;border-radius:6px;overflow:auto;font-size:13px;line-height:1.5}
pre code{padding:0;background:none}
table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums;font-size:14px}
th,td{border:1px solid var(--line);padding:5px 9px;text-align:left;white-space:nowrap}
th{position:sticky;top:0;background:var(--surface);font-weight:600}
tbody tr:nth-child(even){background:var(--surface)}tbody tr:hover{background:var(--hover)}
`

// reportCSS is what only the fleet report needs.
const reportCSS = `
body{padding:20px 16px 40px;max-width:1600px}
td.c{text-align:center}
table.sort th{cursor:pointer;user-select:none}table.sort th:hover{color:var(--link)}
table.sort th[data-dir=asc]::after{content:" ▲";font-size:10px}table.sort th[data-dir=desc]::after{content:" ▼";font-size:10px}
.ok{color:var(--ok)}.warn{color:var(--warn)}.err{color:var(--err);font-weight:600}.na{color:var(--muted)}
.bar{display:inline-block;height:10px;border-radius:2px;vertical-align:middle;margin-right:6px}
.b-ok{background:var(--ok)}.b-warn{background:var(--warn)}.b-err{background:var(--err)}
.wrap{overflow-x:auto;border-radius:6px}abbr{text-decoration:none;cursor:help}
.filter{margin:0 0 8px;display:flex;gap:16px;flex-wrap:wrap;align-items:center}
.filter input[type=search]{font:inherit;padding:4px 8px;min-width:16rem;background:var(--bg);color:var(--fg);border:1px solid var(--line);border-radius:6px}
th.rot{height:170px;vertical-align:bottom;padding:4px 2px}
th.rot div{writing-mode:vertical-rl;transform:rotate(180deg);font-weight:normal;font-size:12px}
`

// pageCSS is what only the action pages need.
const pageCSS = `
body{max-width:60rem;margin:0 auto;padding:2rem 1rem 3rem}
h2{margin-top:2.2rem;font-size:18px}
`
