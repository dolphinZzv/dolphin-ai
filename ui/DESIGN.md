# UI 设计风格

## 设计原则

- **手机优先** — 默认手机布局，逐步增强到平板和桌面。不在手机上妥协
- **清晰优先** — 每个页面只有一个主要操作，信息层级分明
- **一致性** — 复用 shadcn 组件，不自定义视觉样式
- **暗色模式** — 默认跟随系统，支持手动切换
- **中文优先** — 全界面中文，符合中文用户阅读和操作习惯

## 手机优先策略

### 核心思路

```
默认 (手机)    →   平板 (≥640px)    →   桌面 (≥1024px)
───────────────────────────────────────────────────────
底部导航       →   侧栏图标         →   展开侧栏
单列堆叠       →   2 列网格         →   多列/双栏
全屏操作       →   弹窗/侧面板      →   弹窗
大字+大触控    →   适中             →   标准
```

手机不是"缩水的桌面"，而是**默认体验**。桌面版在手机版基础上增加信息密度和快捷操作。

### Touch 优先

```
交互元素最小尺寸: 44px (Apple HIG 标准)
按钮间距:        ≥ 8px（防止误触）
列表项:          整行可点击（不依赖小图标）
下拉菜单:        选项高度 ≥ 44px
```

### 性能基线

手机网络和硬件受限，纳入设计考量：

```
JS 产物:   ≤ 150KB (gzip)
首屏加载:  ≤ 2s (3G 模拟)
图片:      不加载非首屏图片
字体:      系统自带字体，零网络开销（Noto Sans SC 后期按需加载）
Lazy Load: 页面/弹窗按需加载
```

## 断点系统

手机优先的 min-width 断点：

```
默认        < 640px    手机 (竖屏)
sm:  640px  ~ 1023px   平板 (竖屏/横屏)
lg:  1024px +          桌面

Tailwind 写法:
  <div className="grid grid-cols-1 lg:grid-cols-4">
    默认手机 1 列 → 桌面 4 列
  </div>
```

不在手机上用 `hidden` 隐藏内容 —— 手机上默认显示所有核心内容，桌面用 `lg:block` 增强布局。

## 中文适配

### 字体

中文场景下字体栈调整，确保中文字符渲染细腻：

```
字体栈: 'PingFang SC', 'Microsoft YaHei', 'Noto Sans SC', Inter, sans-serif
等宽:   'JetBrains Mono', 'Noto Sans Mono SC', monospace
```

- PingFang SC 优先（macOS 内置，系统字体零开销）
- Microsoft YaHei 备选（Windows 内置）
- Noto Sans SC 备选（后期优化按需加载）
- 英文字体放在中文字体之后，确保英文/数字优先用 Inter

### 字号

中文阅读建议字号比英文略大：

```
body:     text-sm (14px) → 中文可读性良好
h1:       text-2xl (24px)
h2:       text-xl (20px)
small:    text-xs (12px) → 中文最小字号，不再缩小
```

不使用小于 12px 的中文文本。

### 日期时间

```
格式:     YYYY-MM-DD HH:mm（24小时制）
相对时间: "3 分钟前"、"昨天 14:30"、"2026-05-11"
时区:     Asia/Shanghai（北京时间）
```

- 不使用 MM/DD/YYYY（美式）或 DD/MM/YYYY（欧式）
- 相对时间只在 24 小时内显示，超过显示完整日期
- 工具提示显示完整绝对时间

### 本地化细节

- 数字分隔符不使用千位逗号（中文习惯 10000 > 10,000）
- 金额/百分比等按中国习惯呈现
- 空状态插图旁的说明文字用中文
- Toast 通知、错误提示、确认对话框全用中文
- 键盘快捷键提示标注中文按键名（"确认" 而非 "Submit"）

### 搜索

- 支持中文分词搜索（后端已有全文搜索）
- 搜索框 placeholder 提示："搜索项目、Issue、Agent..."

## 色彩系统

基于 shadcn CSS 变量，覆盖 Tailwind 主题色。

### 亮色模式

```
背景:       白色 (#fff)          hsl(0 0% 100%)
卡片:       暖灰-50              hsl(0 0% 98%)
边框:       暖灰-200             hsl(0 0% 90%)
主文本:     暖灰-900             hsl(0 0% 10%)
次要文本:   暖灰-500             hsl(0 0% 45%)

主要色:     蓝-600               hsl(221 83% 53%)     — 按钮/链接/激活态
主要色悬浮: 蓝-700               hsl(221 83% 45%)
成功:       绿-600               hsl(142 71% 45%)     — closed_completed
警告:       琥珀-500             hsl(38 92% 50%)      — blocked
危险:       红-600               hsl(0 84% 60%)       — 删除/错误
```

### 暗色模式

```
背景:       暖灰-950             hsl(0 0% 4%)
卡片:       暖灰-900             hsl(0 0% 10%)
边框:       暖灰-800             hsl(0 0% 15%)
主文本:     暖灰-100             hsl(0 0% 95%)
次要文本:   暖灰-400             hsl(0 0% 55%)

主要色:     蓝-400               hsl(221 83% 65%)
成功:       绿-400               hsl(142 71% 55%)
警告:       琥珀-400             hsl(38 92% 55%)
危险:       红-400               hsl(0 84% 65%)
```

### 语义色

| 用途 | 亮色 | 暗色 |
|---|---|---|
| Issue State Badge | 对应色 bg + 文字 | 对应色 bg + 文字 |
| Priority 关键 | hsl(0 84% 60%) | hsl(0 84% 65%) |
| Priority 高 | hsl(25 95% 53%) | hsl(25 95% 58%) |
| Priority 中 | hsl(221 83% 53%) | hsl(221 83% 65%) |
| Priority 低 | hsl(200 98% 39%) | hsl(200 98% 55%) |
| Agent Status 在线 | 绿 | 绿 |
| Agent Status 忙碌 | 琥珀 | 琥珀 |
| Agent Status 离线 | 灰 | 灰 |
| Agent Status 错误 | 红 | 红 |

## 字体

```
字体栈: 'PingFang SC', 'Microsoft YaHei', 'Noto Sans SC', Inter, sans-serif
等宽:   'JetBrains Mono', 'Noto Sans Mono SC', monospace
层级:
  h1: text-2xl font-semibold     — 页面标题
  h2: text-xl font-semibold      — 区块标题
  h3: text-base font-semibold    — 卡片标题
  body: text-sm                  — 正文
  small: text-xs                 — 辅助文字（不低于 12px）
  code: text-sm font-mono        — 代码/ID
```

## 间距

```
页面 Padding: p-3 (手机默认) → lg:p-6
卡片间距: gap-3 (手机) → lg:gap-4
列表项间距: space-y-2
内容与标题间距: mt-4 (手机) → lg:mt-6
```

## 布局

### 导航

```
手机 (<640px):        底部固定导航栏 (5 项: 首页/项目/Agent/设置/用户)
                      当前页高亮，图标 + 标签文字
                      无侧边栏

平板 (640–1023px):    底部导航 + 左侧图标栏 (64px)
                      图标栏只显示图标，文字隐藏

桌面 (≥1024px):       底部导航隐藏，左侧展开侧栏 (240px)
                      图标 + 文字标签
```

底部导航始终可见，不因桌面模式移除。桌面模式下 auto-hide。

### 看板 (Issue Board)

```
手机 (<640px):        单列列表，每条 Issue 占满宽度
                      State 切换用底部 Action Sheet 或 Dropdown（不拖拽）

平板 (640–1023px):    2 列网格，横向滚动
                      可以拖拽

桌面 (≥1024px):       4 列 (open / in_progress / blocked / review)
                      closed 折叠在底部，可展开
                      拖拽切换状态
```

### 详情页 (Issue Detail)

```
手机 (<640px):        单列堆叠
                      元信息（Assignee/Labels/Priority）可折叠区域
                      操作按钮固定在底部

平板 (640–1023px):    单列，元信息在内容下方

桌面 (≥1024px):       双栏 — 主内容 2/3 + 元信息侧栏 1/3
                      操作按钮在顶部
```

## 组件设计规范

### Issue Card

```
┌──────────────────────────────┐
│ ◯ Fix login timeout bug      │  ← 标题(1行截断)
│ #42 · open                   │  ← Issue 号 + State Badge
│ 🔴 critical                  │  ← Priority Badge
│ 👤 alice · 🏷 bug · 🏷 auth  │  ← Assignee + Label 药丸
└──────────────────────────────┘
- 圆角: rounded-lg
- 手机: 左右满宽，上下间距 8px
- 桌面: 有边距，阴影微妙
- 左侧 3px 彩色边框标识 State
- 整行可点击（手机触摸友好）
```

### Button

```
primary:   bg-primary text-primary-foreground
secondary: bg-secondary text-secondary-foreground (灰色)
outline:   border + bg-transparent
ghost:     bg-transparent (hover 高亮)
danger:    bg-destructive text-destructive-foreground

尺寸: default (h-10) — 手机默认更大
      sm (h-9)
      lg (h-11)
      icon (h-10 w-10)

手机: 默认按钮宽度 100%（撑满）
桌面: 默认按钮宽度 auto
```

### Badge

```
State Badge (圆角 pill):
  open:            bg-green-100 text-green-800
  in_progress:     bg-blue-100 text-blue-800
  blocked:         bg-amber-100 text-amber-800
  review:          bg-purple-100 text-purple-800
  closed_completed: bg-gray-100 text-gray-800 (strikethrough)
  closed_not_planned: bg-gray-100 text-gray-500 (strikethrough)

Priority Badge:
  关键(critical): bg-red-100 text-red-800
  高(high):       bg-orange-100 text-orange-800
  中(medium):     bg-blue-100 text-blue-800
  低(low):        bg-sky-100 text-sky-800

Label Badge:
  bg-gray-100 text-gray-700 text-xs 圆角药丸
```

### Dialog / Action Sheet

```
手机 (<640px):
  - Action Sheet 样式（从底部滑入）
  - 遮罩 + 底部面板，可下滑关闭
  - 取消按钮固定在底部

桌面 (≥640px):
  - 居中弹窗
  - 点击遮罩关闭
  - 宽度: sm:max-w-lg (默认)
  - Cancel + Confirm 在底部右对齐

创建 Issue:
  - 手机: 全屏新页面（不是弹窗）
  - 桌面: sm:max-w-xl 弹窗
```

### Comment Thread

```
┌─ 头像 ─ 作者名 ─ 时间 ─┐
│                         │
│ Markdown 渲染内容        │
│                         │
│ 回复 · 编辑 · 删除      │  ← 操作按钮(ghost, text-xs)
├─────────────────────────┤
│ 回复...                 │  ← 缩进 8px 嵌套
└─────────────────────────┘

输入框:
  - textarea 自动增高
  - 支持 Markdown (预览切换)
  - 手机: 输入框固定在底部（类似 IM 应用）
  - 桌面: 输入框在评论区底部
  - Cmd+Enter / 发送按钮 提交
```

### Timeline

```
● 创建 Issue    — 张三          2 小时前
│
● 分配          — 分配给 李四    1 小时前
│
● 状态变更      → 进行中         30 分钟前
│
● 评论          — 王五          5 分钟前

每个事件包含：
  - 圆点 + 事件类型标签（中文："创建 Issue" 而非 "issue_created"）
  - 参与者姓名（可点击）
  - 相对时间
  - 手机: 简洁版，只显示图标 + 一句话描述
  - 桌面: 完整版，显示详情
```

## 动画

```
- 页面切换: 无动画（即时渲染，手机感知更快）
- 底部导航切换: 无动画
- Action Sheet: slide-up (200ms) — 手机
- Dialog: fade-in + scale-in (150ms) — 桌面
- Dropdown: fade-in (100ms)
- Toast: slide-up-from-bottom (200ms) — 手机优先
- Sidebar 展开/收起: transition-width (200ms) — 桌面
- 暗色模式切换: transition-colors (200ms)
```

手机动画比桌面更少、更简单（降低低端设备负载）。

## 布局细则

### 手机 <640px（默认）

- 底部固定导航栏 (Home · 项目 · Agent · 设置)
- 当前页 tab 高亮
- 页面标题固定在顶部
- 操作按钮在底部（拇指热区）
- Action Sheet 替代 Dialog
- 单列布局，不隐藏关键信息
- 列表整行可点击

### 平板 640–1023px

- 底部导航 + 左侧图标栏
- 看板 2 列 + 横向滚动
- 详情页单栏，元信息在内容下方
- Dialog 弹窗（非 Action Sheet）

### 桌面 ≥1024px

- 展开侧栏，底部导航隐藏（可设置 auto-hide）
- 看板 4 列横向滚动
- 详情页双栏布局
- 快捷操作（快捷键提示）
- 信息密度更高（show more）

## 暗色模式

- 使用 shadcn `next-themes` 的 `ThemeProvider`（class 策略）
- 默认跟随系统 (`prefers-color-scheme`)
- TopBar/底部导航 右侧手动切换按钮 (Sun/Moon 图标)
- 所有 CSS 变量通过 `:root` / `.dark` 切换
- 不额外写 `dark:` Tailwind class（依赖 CSS 变量自动适配）

## 图标

全部使用 Lucide React，shadcn 默认图标集：
- 导航: LayoutDashboard, FolderKanban, Bot, Settings
- Issue: CircleDot, MessageSquare, Clock, ArrowRight
- 操作: Plus, Trash2, Pencil, X, Check, ChevronDown
- 状态: Circle (绿/琥珀/红/灰)
- 用户: User, LogOut, Sun, Moon

## 无障碍 (a11y)

基于 shadcn/Radix 原语，大部分无障碍能力开箱即得。以下清单覆盖需要人工关注的场景。

### 焦点管理

| 场景 | 要求 |
|------|------|
| Dialog / Action Sheet 打开 | 焦点锁定在弹窗内 (Radix 默认) |
| Dialog / Action Sheet 关闭 | 焦点恢复到触发按钮 |
| 页面路由跳转 | 页面 `<h1>` 获得焦点 (用 `useEffect` + `ref.focus()`) |
| IssueBoard 拖拽 | 拖拽结束后焦点回到卡片 |
| Toast 出现 | 不抢焦点（仅 aria-live 播报） |
| 错误状态 | ErrorBoundary fallback 中的重试按钮自动获焦 |

### 键盘导航

| 场景 | 要求 |
|------|------|
| Dialog | Escape 关闭 (Radix 默认) |
| Dropdown / Select | 方向键导航 (Radix 默认) |
| IssueBoard | `Tab` 在列间移动，Enter 打开详情 |
| Comment 输入 | `Cmd+Enter` / `Ctrl+Enter` 提交 |
| 列表页 | `Tab` 在卡片间导航 |
| 侧栏 | Escape 关闭（移动端 overlay） |

### ARIA

| 场景 | 要求 |
|------|------|
| 图标按钮 (icon-only) | `<Button aria-label="创建 Issue">` |
| Badge (State/Priority) | `<span role="status">` |
| Toast 通知 | `<div role="alert" aria-live="assertive">` |
| Loading Skeleton | `<div aria-busy="true">` |
| 错误提示 | `<div role="alert">` |
| 导航栏 | `<nav aria-label="主导航">` |
| 移动端底部导航 | `<nav aria-label="底部导航">` |
| 当前页标识 | `aria-current="page"` |

### 色彩与对比度

- 所有文本/背景组合满足 WCAG AA 标准 (对比度 ≥ 4.5:1)
- 状态/优先级不单独依赖颜色标识，同时使用文字标签（State Badge 含中文名，Priority Badge 含文本）
- 暗色模式色值已定义，不影响对比度

### 屏幕阅读器

| 场景 | 要求 |
|------|------|
| 头像 (Avatar) | `<img alt="张三的头像">` 或 `aria-label` |
| 图表/统计卡片 | `<span aria-label="3 个进行中的 Issue">` |
| 相对时间 | 同时包含机器可读时间 `<time datetime="...">` |
| 拖拽 (dnd-kit) | `aria-roledescription="sortable"` |
| 状态变更通知 | `aria-live="polite"` 播报 |

### Touch 目标

已在手机优先章节定义：最小 44px，间距 ≥ 8px。所有可交互元素必须满足此标准。
