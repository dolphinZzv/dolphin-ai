---
title: Chrome DevTools
weight: 3
---

Chrome DevTools MCP 让 Dolphin 能直接控制浏览器：打开页面、点击元素、截图、分析性能、检查网络请求等。

## 配置

在 `mcpServers` 中注册：

```yaml
mcpServers:
  chrome-devtools:
    command: npx
    args:
      - "-y"
      - chrome-devtools-mcp@latest
```

启动后通过 `/mcp` 确认工具已加载即可使用。

## 常用工具

### 页面操作

| 工具 | 说明 |
|------|------|
| `navigate_page` | 跳转到指定 URL |
| `click` | 点击某个元素 |
| `fill` / `fill_form` | 填写表单 |
| `screenshot` / `take_screenshot` | 截图 |
| `evaluate_script` | 在页面中执行 JS |

### 调试

| 工具 | 说明 |
|------|------|
| `list_console_messages` | 查看控制台日志 |
| `list_network_requests` | 查看网络请求 |
| `take_memory_snapshot` | 内存快照 |
| `lighthouse_audit` | Lighthouse 审计（性能/可访问性/SEO） |

### 多标签

| 工具 | 说明 |
|------|------|
| `list_pages` | 列出所有已打开的标签页 |
| `new_page` | 新建标签页 |
| `select_page` | 切换到指定标签页 |

## 使用示例

在对话中直接描述需求即可，Dolphin 会自动调用对应工具。

> 打开 https://example.com 并截图

> 搜索 chrome-devtools-mcp 的 GitHub 仓库，把第一页的控制台错误列出来

> 跑一次 Lighthouse 性能审计，给我优化建议

> 帮我填这个登录表单，用户 admin，密码 123456，然后截图

## 选项

通过 `args` 传入额外参数控制浏览器行为：

```yaml
mcpServers:
  chrome-devtools:
    command: npx
    args:
      - "-y"
      - chrome-devtools-mcp@latest
      - "--headless"            # 无头模式，不显示浏览器窗口
      - "--viewport=1280x720"   # 视口大小
      - "--categoryNetwork=false"  # 关闭网络工具组
```

完整选项参考 [chrome-devtools-mcp 文档](https://github.com/ChromeDevTools/chrome-devtools-mcp)。

## 说明

- 需要 Node.js v20.19+ 和 Chrome 已安装
- 首次运行 `npx` 会自动下载依赖，稍等几秒
- 截图等操作写入临时文件，Dolphin 会读取并展示
