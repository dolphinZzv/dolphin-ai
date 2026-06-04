# MCP Browser 工具改进提案

> 日期: 2026-06-03
> 提出人: AI 助理（基于今日 X/Twitter + Hacker News 实操反馈）

---

## 一、当前状态评估

| 维度 | 评分 | 说明 |
|---|---|---|
| 导航能力 | ⭐⭐⭐⭐⭐ | `browser_navigate` 稳、快 |
| 多标签管理 | ⭐⭐⭐⭐⭐ | `browser_list_tabs/create/activate/close` 完整 |
| 截图能力 | ⭐⭐⭐⭐ | `browser_screenshot` 实用 |
| 交互/点击能力 | ⭐⭐ | 全靠 `browser_evaluate` 手写 JS 模拟 |
| 数据提取能力 | ⭐⭐ | 同样全靠手写 JS 遍历 DOM |
| 同步/等待能力 | ⭐ | 几乎没有，竞态问题频发 |

**核心问题：** 三个高频操作没有原生工具，导致每次交互都要手写大量 JavaScript。

---

## 二、改进提案

### 提案 #1：`browser_click` — 原生点击交互

**当前痛点：** 想点击任何元素，必须手写 JS 模拟事件，且 SPA 框架（React/Vue）经常不响应普通 `.click()`，需要原生事件分发。

**今日实操示例（X/Twitter 关注按钮）：**
```javascript
// 现在的写法
const btn = document.querySelector('[data-testid$="-follow"]');
if (btn) {
  const event = new MouseEvent('click', { bubbles: true, cancelable: true });
  btn.dispatchEvent(event);
}
```

**期望接口：**
```typescript
browser_click({
  selector: string,           // CSS 选择器
  method?: "dispatch" | "native" | "js",  // 触发方式，默认自适应
  wait?: boolean,             // 点击后等待页面稳定，默认 true
  retry?: number              // 失败重试次数，默认 3
})
```

**使用示例：**
```
browser_click("[data-testid$='-follow']")
browser_click("text='关注'")          // 按文本匹配
browser_click("xpath=//button[text()='Follow']")  // 按 XPath
```

---

### 提案 #2：`browser_extract` — 结构化数据提取

**当前痛点：** 提取页面数据需要手写 DOM 遍历、数据清洗、格式转换，每个场景重复造轮子。

**今日实操示例（Hacker News 热榜）：**
```javascript
// 现在的写法
document.querySelectorAll('.athing').map(el => ({
  title: el.querySelector('.titleline a')?.innerText,
  link: el.querySelector('.titleline a')?.href,
  score: el.nextElementSibling?.querySelector('.score')?.innerText
}))
```

**期望接口：**
```typescript
browser_extract({
  selector: string,                    // CSS 选择器
  attribute?: string | string[],       // 提取属性: "text" | "href" | "src" | "innerHTML" | ["text", "href"]
  multiple?: boolean,                  // true = 返回所有匹配, false = 只返回第一个
  format?: "text" | "json" | "table",  // 返回格式
  transform?: "trim" | "number" | "none"  // 后处理
})
```

**使用示例：**
```
browser_extract(selector=".storylink a", attribute=["text", "href"], multiple=true)
// 返回: [{text: "Google 发布 Gemma 4", href: "..."}, ...]

browser_extract(selector="table tr", format="table")
// 返回: [[col1, col2, ...], ...]

browser_extract(selector="h1", attribute="text")
// 返回: "页面标题"
```

---

### 提案 #3：`browser_wait` — 页面就绪等待

**当前痛点：** 导航后元素还未渲染就执行操作，报错；没有等待机制只能硬编码 setTimeout 或手写自旋循环。

**今日实操示例（等待搜索结果加载）：**
```javascript
// 现在的写法 — 手写自旋循环
async function waitForElement(selector, timeout=10) {
  for(let i=0; i<timeout*10; i++) {
    if(document.querySelector(selector)) return true;
    await new Promise(r => setTimeout(r, 100));
  }
  return false;
}
```

**期望接口：**
```typescript
browser_wait({
  selector: string,              // CSS 选择器
  state: "exists" | "visible" | "stable" | "gone",  // 等待状态
  timeout?: number,              // 超时秒数，默认 10
  interval?: number,             // 轮询间隔 ms，默认 100
  stable_duration?: number       // state=stable 时，页面需稳定多久(ms)
})
```

**状态说明：**

| state | 含义 | 典型场景 |
|---|---|---|
| `exists` | 元素出现在 DOM 中 | 等待搜索结果出现 |
| `visible` | 元素可见（非 hidden/display:none） | 等待弹窗出现 |
| `stable` | 页面 DOM 连续 N 毫秒无变化 | 等待 SPA 完全渲染 |
| `gone` | 元素从 DOM 中消失 | 等待 loading spinner 消失 |

**使用示例：**
```
browser_wait(selector="[data-testid='tweet']", state="visible", timeout=10)
browser_wait(selector=".loading-spinner", state="gone", timeout=15)
browser_wait(selector="body", state="stable", timeout=5)
```

---

## 三、三件套协作流程

```
browser_navigate("https://x.com/search?q=OpenAI")
  → browser_wait("[data-testid='tweet']", state="visible")
    → browser_extract(".tweet-text", attribute="text", multiple=true)
      → browser_click("[data-testid='first-result']")
        → browser_wait("[data-testid$=-follow]", state="exists")
          → browser_click("[data-testid$=-follow]")
```

**效果：** 无需任何手写 JS，纯工具链完成完整的搜索→提取→点击操作。

---

## 四、优先级建议

| 优先级 | 提案 | 预期收益 |
|---|---|---|
| 🔴 **P0 必须** | `browser_click` | 消除 90% 的手写 JS 交互代码 |
| 🔴 **P0 必须** | `browser_wait` | 解决竞态问题，提升稳定性 3x |
| 🟡 **P1 强烈建议** | `browser_extract` | 数据提取从 20 行 JS 降到 1 行参数 |
| 🟢 **P2 锦上添花** | `browser_search(engine, query)` | 搜索操作一站式完成 |

---

## 五、影响范围

- **用户端：** 使用 MCP Browser 的开发者/Agent 体验大幅提升
- **实现复杂度：** 三个工具均可基于现有 `browser_evaluate` 能力实现，无需改动浏览器底层
- **兼容性：** 向后兼容，不影响现有 `browser_evaluate/navigate/screenshot` 等已有工具
