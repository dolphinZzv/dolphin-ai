# Browser MCP Examples

Test HTML pages for the DolphinzZ Browser MCP.

## Pages

| File | Test Scenario |
|------|---------------|
| `01_basic.html` | Basic page load, JS execution |
| `02_navigation.html` | Hash navigation, link clicks, new tab |
| `03_forms.html` | Form fill, submit, JS evaluation |
| `04_dynamic.html` | Dynamic DOM — test `browser_wait` (exists/visible/gone/stable) |
| `05_alerts.html` | Alert/confirm/prompt intercept |
| `06_scroll.html` | Page scrolling |
| `07_screenshot.html` | Visual elements for screenshot testing |

## Usage

Serve with any HTTP server, e.g.:

```bash
python3 -m http.server 8080
```

Then use `browser_navigate` to `http://localhost:8080/04_dynamic.html`.
