# UI 设计风格

## 设计原则

- **清晰优先** — 每个页面只有一个主要操作，信息层级分明
- **一致性** — 复用 shadcn 组件，不自定义视觉样式
- **响应式** — 桌面/平板/手机三档适配，不割裂体验
- **暗色模式** — 默认跟随系统，支持手动切换
- **中文优先** — 全界面中文，符合中文用户阅读和操作习惯

## 中文适配

### 字体

中文场景下字体栈调整，确保中文字符渲染细腻：

```
字体栈: 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', Inter, sans-serif
等宽:   'JetBrains Mono', 'Noto Sans Mono SC', monospace
```

- Noto Sans SC 优先（Google Fonts，无版权问题）
- PingFang SC 备选（macOS 内置）
- Microsoft YaHei 备选（Windows 内置）
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

### 布局习惯

- 列表页默认分页 20 条/页（中文用户习惯 20/50/100 选项）
- 表格/列表顶部分页控件（中文用户习惯上方分页）
- 表单标签右对齐（中文阅读视线从右到左扫到输入框更自然，但考虑到现代 Web 习惯，统一左对齐 + 顶部标签）
- 确认弹窗按"取消 → 确认"顺序排列（符合平台一致性，不特地为中文反转）

### 搜索

- 支持中文分词搜索（后端已有全文搜索）
- 搜索框 placeholder 提示："搜索项目、Issue、Agent..."

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
字体栈: 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', Inter, sans-serif
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
页面 Padding: p-6 (桌面), p-4 (平板), p-3 (手机)
卡片间距: gap-4
列表项间距: space-y-2
内容与标题间距: mt-6
```

## 布局

### 侧边栏

```
桌面 (≥1024px):  固定 240px，左侧放置
平板 (768-1023): 收缩为 64px 图标栏
手机 (<768px):   隐藏，从左侧滑出浮层
```

每个导航项包含图标 + 文字标签（平板仅图标）。

### 看板 (Issue Board)

```
桌面 4 列: open / in_progress / blocked / review + closed 折叠在底部
平板 2 列: 左右滚动
手机 1 列: 上下滚动，State 切换用 Dropdown
```

每列宽度固定 280px，横向滚动，不压缩卡片。

### 详情页 (Issue Detail)

```
桌面: 双栏布局 — 主内容区 2/3 + 元信息侧栏 1/3
平板: 侧栏移到下方
手机: 单列堆叠，元信息可折叠
```

## 组件设计规范

### Issue Card

```
┌──────────────────────────┐
│ ◯ Fix login timeout bug  │  ← 标题(1行截断)
│ #42 · open               │  ← Issue 号 + State Badge
│ 🔴 critical              │  ← Priority Badge
│ 👤 alice · 🏷 bug · 🏷 auth│  ← Assignee + Label 药丸
└──────────────────────────┘
- 圆角: rounded-lg
- 悬浮: shadow-sm → shadow-md
- 左侧彩色边框标识 State
- 拖拽中: opacity-50 + rotate-2
```

### Button

```
primary:   bg-primary text-primary-foreground
secondary: bg-secondary text-secondary-foreground (灰色)
outline:   border + bg-transparent
ghost:     bg-transparent (hover 高亮)
danger:    bg-destructive text-destructive-foreground

尺寸: default (h-9) / sm (h-8) / lg (h-10) / icon (h-9 w-9)
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
  关键(critical): bg-red-100 text-red-800 + 🔴
  高(high):       bg-orange-100 text-orange-800
  中(medium):     bg-blue-100 text-blue-800
  低(low):        bg-sky-100 text-sky-800

Label Badge:
  bg-gray-100 text-gray-700 text-xs 圆角药丸
```

### Dialog

```
- 居中弹窗
- 点击遮罩关闭
- 标题: font-semibold
- 底部: Cancel + Confirm 按钮
- 宽度: sm:max-w-lg (默认), sm:max-w-xl (创建 Issue)
```

### Comment Thread

```
┌─ 头像 ─ 作者名 ─ 时间 ─┐
│                         │
│ Markdown 渲染内容        │
│                         │
│ Reply · Edit · Delete   │  ← 操作按钮(ghost, text-xs)
├─────────────────────────┤
│ Replies...              │  ← 缩进 8px 嵌套
└─────────────────────────┘

输入框:
  - textarea 自动增高
  - 支持 Markdown (预览切换)
  - Cmd+Enter 提交
```

### Timeline

```
● issue_created       — Alice created this issue        2h ago
│
● assigned            — Bob assigned to this issue      1h ago
│
● state_changed       → in_progress                     30m ago
│
● comment_added       — Alice commented                 5m ago

时间线按时间倒序排列，每个事件包含：
  - 图标/圆点 + 事件类型标签
  - 描述文字（可点击 Actor）
  - 相对时间
```

## 动画

```
- 页面切换: 无动画 (即时渲染)
- Dialog:  fade-in + scale-in (150ms)
- Dropdown: fade-in (100ms)
- Toast:   slide-in-from-right (200ms)
- Issue Card 拖拽: transition-transform (150ms)
- Sidebar 展开/收起: transition-width (200ms)
- 暗色模式切换: transition-colors (200ms)
```

## 响应式适配细则

### 桌面 ≥1024px
- Sidebar 固定 240px
- 看板横向滚动 4 列
- 详情页双栏布局
- Dialog 居中

### 平板 768–1023px
- Sidebar 收缩为 64px 图标栏（文本隐藏）
- 看板 2 列 + 横向滚动
- 详情页单栏（侧栏移到下方）
- Dialog 宽度自适应

### 手机 <768px
- Sidebar 隐藏，左上角汉堡按钮展开浮层
- 底部固定导航栏 (Home · Projects · Agents · Settings)
- 看板单列，State 切换用 Dropdown 替代拖拽
- 详情页全单栏 + 可折叠 Meta 区
- 所有卡片圆角减小 (rounded-lg → rounded-md)
- 页面 Padding 缩小 (p-6 → p-3)

## 暗色模式

- 使用 shadcn `next-themes` 的 `ThemeProvider`（class 策略）
- 默认跟随系统 (`prefers-color-scheme`)
- TopBar 右侧手动切换按钮 (Sun/Moon 图标)
- 所有 CSS 变量通过 `:root` / `.dark` 切换
- 不额外写 `dark:` Tailwind class（依赖 CSS 变量自动适配）

## 图标

全部使用 Lucide React，shadcn 默认图标集：
- 导航: LayoutDashboard, FolderKanban, Bot, Settings
- Issue: CircleDot, MessageSquare, Clock, ArrowRight
- 操作: Plus, Trash2, Pencil, X, Check, ChevronDown
- 状态: Circle (绿/琥珀/红/灰)
- 用户: User, LogOut, Sun, Moon
