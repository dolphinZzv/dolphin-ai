#!/usr/bin/env python3
"""
真人参与狼人杀（1 真人 + 8 AI）

你是 1 号玩家，身份随机。主持人 agent 当裁判并扮演其余 8 个 AI 玩家。
你通过终端与主持人对话：收夜晚/白天提示，输入你的行动、发言、投票。

用法:
    python3 play_werewolf_human.py
    python3 play_werewolf_human.py --name 你名字 --seat 3

前提: dolphin 二进制已编译（go build -o dolphin ./cmd/dolphin）
"""
import argparse
import json
import os
import random
import shutil
import signal
import subprocess
import sys
import time
import urllib.request

ROOT = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(ROOT)
DOLPHIN = os.path.join(REPO, "dolphin")
RUNTIME = os.path.join(ROOT, "runtime_human")
MOD_PORT = 8300

API_KEY = "ark-06f7312c-590a-4617-8a5f-e153e5c43b50-a1e85"
BASE_URL = "https://ark.cn-beijing.volces.com/api/plan/v3"
# 可用模型池（逗号分割），从 delivery/.env 的 MOD_MODEL 读取
MODELS = ["deepseek-v4-flash"]


def load_env():
    """从 delivery/.env 读取配置，覆盖默认值。格式：KEY = "value" 或 KEY=value。
    MOD_MODEL 支持逗号分隔的多个模型，作为可用模型池。"""
    global API_KEY, BASE_URL, MODELS
    env_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".env")
    if not os.path.exists(env_path):
        return
    models = None
    with open(env_path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, val = line.partition("=")
            key = key.strip()
            val = val.strip().strip('"').strip("'")
            if key == "API_KEY":
                API_KEY = val
            elif key == "BASE_URL":
                BASE_URL = val
            elif key == "MOD_MODEL":
                models = val
    if models:
        MODELS = [m.strip() for m in models.split(",") if m.strip()]


load_env()

PROCS = []


def log(m): print(f"[game] {m}", flush=True)


def gen_mod_cfg(model, human_name, human_seat, n):
    """主持人 config: 裁判 + 扮演 8 个 AI 玩家。"""
    wd = os.path.join(RUNTIME, "moderator")
    return f"""agent:
  name: moderator
  workspace: {wd}
  max_rounds: 80
  pool_size: 1
  workmode: yolo
a2a:
  enabled: true
  addr: "127.0.0.1:{MOD_PORT}"
  name: moderator
agents:
  enabled: true
  name: moderator
  listen_addr: "127.0.0.1:{MOD_PORT}"
  capabilities: ["orchestrate"]
  task_timeout: "600s"
  max_delegation_depth: 2
llm:
  use: volcengine_agent/{model}
  max_retries: 5
  timeout: 120s
  volcengine_agent:
    provider: volcengine
    api_type: openai
    api_key: "{API_KEY}"
    base_url: "{BASE_URL}"
    models:
      - name: "{model}"
        temperature: 0.6
memory:
  dir: {wd}/memory
session:
  dir: {wd}/sessions
log:
  level: info
  file: {wd}/moderator.log
  compress: false
tui:
  enabled: false
dream:
  enabled: false
system_prompt: |
  你是狼人杀主持人（上帝），同时扮演 {n} 人局中除 {human_seat} 号玩家（真人 {human_name}）外的所有 AI 玩家。
  【绝对禁止】不要调用任何工具！不要用 brain_list/brain_read/commands_list/scripts_list/delegate_to_agent 等任何工具。你只需用纯文字对话推进游戏，所有 AI 玩家的发言和行动都由你直接用文字扮演。
  你的职责：
  1. 开局随机分配身份（狼人/预言家/女巫/守卫/猎人/村民），仅你知晓全部身份。
  2. 夜晚：依次让狼人、预言家、女巫、守卫行动。若 {human_seat} 号是夜晚角色，你必须暂停并询问真人；其余 AI 角色你直接用文字决定并叙述。
  3. 白天：宣布昨夜死亡，所有存活玩家依次发言（AI 玩家由你扮演，{human_seat} 号由真人输入），然后投票。
  4. 投票后宣布出局者，判断游戏是否结束。
  规则：狼人杀光好人或好人投光狼人即胜。
  重要：每次需要真人 {human_seat} 号行动时，你必须以一行 ">>> 等待 {human_seat} 号玩家行动: <提示>" 结束你的回复，然后停下。不要替真人做决定，不要继续生成后续内容。回复要简洁，不要冗长。
"""


def write(path, content):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)


def start(cfg):
    logf = open(os.path.join(os.path.dirname(cfg), "stdout.log"), "ab")
    p = subprocess.Popen([DOLPHIN, "--config", cfg], stdout=logf, stderr=subprocess.STDOUT,
                         cwd=os.path.dirname(cfg))
    PROCS.append(p)
    return p


def wait_ready(addr, timeout=60):
    url = f"http://{addr}/jsonrpc"
    end = time.time() + timeout
    while time.time() < end:
        try:
            body = json.dumps({"jsonrpc": "2.0", "id": "1", "method": "agents/ping", "params": {}}).encode()
            req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=5) as r:
                if "result" in json.loads(r.read()):
                    return True
        except Exception:
            pass
        time.sleep(1)
    return False


# 会话内多轮对话：用同一 sessionId 让主持人保留记忆
SESSION_ID = "human-game-1"


def send_to_mod(text, timeout=300):
    """发 tasks/send 给主持人，返回回复文本。"""
    url = f"http://127.0.0.1:{MOD_PORT}/jsonrpc"
    body = json.dumps({
        "jsonrpc": "2.0", "id": "h-" + str(int(time.time() * 1000) % 100000),
        "method": "tasks/send",
        "params": {"id": "turn", "sessionId": SESSION_ID,
                   "message": {"role": "user", "parts": [{"text": text}]}},
    }).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.loads(resp.read())
    if "error" in data:
        return f"[错误] {data['error']}"
    out = ""
    for a in data.get("result", {}).get("artifacts", []):
        for p in a.get("parts", []):
            out += p.get("text", "")
    return out


def cleanup():
    for p in PROCS:
        try: p.send_signal(signal.SIGTERM)
        except: pass
    time.sleep(2)
    for p in PROCS:
        try:
            if p.poll() is None: p.kill()
        except: pass


def main():
    ap = argparse.ArgumentParser(description="真人参与狼人杀（1 真人 + 8 AI）")
    ap.add_argument("--name", default="玩家", help="你的名字")
    ap.add_argument("--seat", type=int, default=1, help="你的座位号（默认 1）")
    ap.add_argument("--players", type=int, default=9, help="总人数（默认 9）")
    args = ap.parse_args()

    log(f"🎲 狼人杀开局：{args.players} 人局，你是 {args.seat} 号 ({args.name})，其余 {args.players-1} 人由 AI 扮演")
    log("身份将由主持人随机分配，只有你自己的身份会告诉你。\n")

    if os.path.exists(RUNTIME):
        shutil.rmtree(RUNTIME)
    os.makedirs(RUNTIME)

    model = random.choice(MODELS)
    write(os.path.join(RUNTIME, "moderator", "config.yaml"),
          gen_mod_cfg(model, args.name, args.seat, args.players))

    log(f"启动主持人 ({model})...")
    start(os.path.join(RUNTIME, "moderator", "config.yaml"))
    if not wait_ready(f"127.0.0.1:{MOD_PORT}"):
        log("✗ 主持人启动失败，看 " + os.path.join(RUNTIME, "moderator", "stdout.log"))
        cleanup(); sys.exit(1)
    log("主持人就绪\n")

    # 开局：让主持人发牌并告诉玩家身份
    opener = (f"开始一局 {args.players} 人狼人杀。我是 {args.seat} 号玩家 {args.name}（真人）。"
              f"请随机分配所有身份，然后私下告诉我我的身份，再开始第一夜。"
              f"如果第一夜需要我行动（我是夜晚角色），用 '>>> 等待 {args.seat} 号玩家行动:' 提示我。")
    log("=" * 60)
    log("游戏开始（输入你的行动/发言/投票，回车提交。输入 quit 退出）")
    log("=" * 60)

    log("主持人思考中...（真实 LLM 调用，约 1~2 分钟，请耐心等待）")
    reply = send_to_mod(opener, timeout=240)
    print("\n主持人:\n" + reply)
    if reply.startswith("[错误]") or "llm 失败" in reply or "429" in reply:
        log("⚠ 主持人 LLM 调用失败（可能限流）。等 30 秒后可重跑，或换个时间。")
        log("日志: " + os.path.join(RUNTIME, "moderator", "moderator.log"))
        cleanup(); sys.exit(1)

    # 交互循环
    while True:
        try:
            user = input("\n你 > ").strip()
        except (EOFError, KeyboardInterrupt):
            break
        if user.lower() in ("quit", "exit", "q"):
            break
        if not user:
            continue
        log("主持人思考中...（约 30~90 秒）")
        reply = send_to_mod(user, timeout=300)
        print("\n主持人:\n" + reply)
        if "429" in reply or "llm 失败" in reply:
            log("⚠ LLM 限流/失败，等 10 秒后可重新输入同样内容重试...")
            time.sleep(10)
            continue
        if "游戏结束" in reply or "胜利" in reply:
            break

    # 对局复盘：让主持人公布所有玩家身份 + 每夜行动 + 胜负原因
    log("\n" + "=" * 60)
    log("📋 请求主持人复盘...")
    log("=" * 60)
    recap = send_to_mod(
        "游戏结束。请做完整复盘：1) 列出所有 9 名玩家的真实身份；"
        "2) 回顾每个夜晚谁被刀、谁被救、谁被毒；3) 每天投票出局谁；"
        "4) 最终哪方胜利、为什么。要简洁清晰。", timeout=180)
    print("\n主持人复盘:\n" + recap)

    log("\n对局结束。日志: " + os.path.join(RUNTIME, "moderator", "moderator.log"))
    cleanup()
    log("进程已清理。")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        cleanup(); sys.exit(130)
