#!/usr/bin/env python3
"""测试 OpenAI API 兼容性"""

import argparse
import json
import os
import sys
import time
import ssl
import urllib.request
import urllib.error


def main():
    parser = argparse.ArgumentParser(description="测试 OpenAI API 兼容性")
    parser.add_argument("url", help="API 端点地址，例如 https://api.openai.com/v1")
    parser.add_argument("model", help="模型名称，例如 gpt-4o")
    parser.add_argument("--api-key", "-k", help="API Key（默认从 OPENAI_API_KEY 环境变量读取）")
    parser.add_argument("--prompt", "-p", default="Hello, say hi in one word.", help="测试提示词")
    parser.add_argument("--insecure", "-i", action="store_true", help="跳过 SSL 证书验证")
    args = parser.parse_args()

    api_key = args.api_key or os.environ.get("OPENAI_API_KEY", "")

    url = args.url.rstrip("/")
    model = args.model
    prompt = args.prompt

    print(f"🔍 测试 OpenAI API 兼容...")
    print(f"  端点: {url}")
    print(f"  模型: {model}")

    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 50,
    }).encode("utf-8")

    req = urllib.request.Request(
        f"{url}/chat/completions",
        data=payload,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {api_key}",
        },
        method="POST",
    )

    ctx = ssl.create_default_context()
    if args.insecure:
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE

    start = time.time()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as resp:
            body = json.loads(resp.read())
        elapsed = (time.time() - start) * 1000
        code = resp.status

        content = body["choices"][0]["message"]["content"]

        print()
        print(f"✅ OpenAI API 兼容测试通过")
        print(f"├─ 端点: {url}")
        print(f"├─ 模型: {model}")
        print(f"├─ 状态码: {code}")
        print(f"├─ 响应: {content}")
        print(f"└─ 耗时: {elapsed:.0f}ms")

    except urllib.error.HTTPError as e:
        elapsed = (time.time() - start) * 1000
        try:
            err_body = json.loads(e.read())
        except Exception:
            err_body = str(e)
        print()
        print(f"❌ OpenAI API 兼容测试失败")
        print(f"├─ 端点: {url}")
        print(f"├─ 模型: {model}")
        print(f"├─ 状态码: {e.code}")
        print(f"└─ 错误: {json.dumps(err_body, ensure_ascii=False)}")
        sys.exit(1)

    except Exception as e:
        elapsed = (time.time() - start) * 1000
        print()
        print(f"❌ OpenAI API 兼容测试失败")
        print(f"├─ 端点: {url}")
        print(f"├─ 模型: {model}")
        print(f"├─ 状态码: N/A")
        print(f"└─ 错误: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
