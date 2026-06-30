#!/usr/bin/env python3
"""
一句话开局狼人杀：随机分配角色 + 模型，自动开打。

用法:
    python3 play_werewolf.py                 # 默认 9 人局
    python3 play_werewolf.py --players 12    # 12 人局
    python3 play_werewolf.py --rounds 3      # 玩 3 个夜晚

它会:
1. 随机给每个玩家分配身份（狼人/预言家/女巫/守卫/猎人/村民）
2. 为每个角色 agent 随机选一个火山模型
3. 启动各角色进程 + 主持人
4. 让主持人按夜晚流程委托各角色行动
5. 宣布每夜结果
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
RUNTIME = os.path.join(ROOT, "runtime_play")

API_KEY = "ark-06f7312c-590a-4617-8a5f-e153e5c43b50-a1e85"
BASE_URL = "https://ark.cn-beijing.volces.com/api/plan/v3"
# 可用模型池（逗号分割），从 delivery/.env 的 MOD_MODEL 读取
MODELS = ["deepseek-v4-flash"]


def load_env():
    """从 delivery/.env 读取配置。MOD_MODEL 逗号分隔 → 可用模型池。"""
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

# 角色 agent 配置：name → (port, capabilities, system_prompt)
ROLE_AGENTS = {
    "seer":      (8201, ["divine"],  "你是狼人杀预言家。夜晚查验一名玩家身份，只能回答好人/狼人。"),
    "witch":     (8202, ["poison", "save"], "你是狼人杀女巫。有一瓶解药一瓶毒药，夜晚可救人或毒人。"),
    "guard":     (8203, ["protect"], "你是狼人杀守卫。每晚守护一名玩家，不能连续守同一人。"),
    "hunter":    (8204, ["shoot"],   "你是狼人杀猎人。死亡时可开枪带走一人。"),
    "werewolf":  (8205, ["kill"],    "你是狼人杀狼人。夜晚与同伴集体刀杀一名玩家。"),
}
MODERATOR_PORT = 8200
PROCS = []


def log(m): print(f"[play] {m}", flush=True)


def assign_roles(n):
    """随机分配 n 个玩家的身份，返回 {seat: role}。"""
    # 标准配置：狼人数 ≈ n/4，1 预言家 1 女巫 1 守卫 1 猎人，余村民
    wolves = max(1, n // 4)
    pool = (["werewolf"] * wolves +
            ["seer", "witch", "guard", "hunter"] +
            ["villager"] * (n - wolves - 4))
    random.shuffle(pool)
    return {i + 1: pool[i] for i in range(n)}


def gen_role_cfg(role, port, caps, sys_prompt, model):
    wd = os.path.join(RUNTIME, role)
    return f"""agent:
  name: {role}
  workspace: {wd}
  max_rounds: 15
  pool_size: 1
a2a:
  enabled: true
  addr: "127.0.0.1:{port}"
  name: {role}
agents:
  enabled: true
  name: {role}
  listen_addr: "127.0.0.1:{port}"
  capabilities: {json.dumps(caps)}
  max_delegation_depth: 1
llm:
  use: volcengine_agent/{model}
  max_retries: 2
  timeout: 60s
  volcengine_agent:
    provider: volcengine
    api_type: openai
    api_key: "{API_KEY}"
    base_url: "{BASE_URL}"
    models:
      - name: "{model}"
        temperature: 0.7
memory:
  dir: {wd}/memory
session:
  dir: {wd}/sessions
log:
  level: info
  file: {wd}/{role}.log
  compress: false
tui:
  enabled: false
dream:
  enabled: false
system_prompt: |
  {sys_prompt}
"""


def gen_mod_cfg(model, seats):
    wd = os.path.join(RUNTIME, "moderator")
    peers = ""
    for role, (port, caps, _) in ROLE_AGENTS.items():
        peers += f'    - name: {role}\n      addr: "127.0.0.1:{port}"\n      capabilities: {json.dumps(caps)}\n'
    seats_str = ", ".join(f"{s}号={r}" for s, r in seats.items())
    return f"""agent:
  name: moderator
  workspace: {wd}
  max_rounds: 40
  pool_size: 1
a2a:
  enabled: true
  addr: "127.0.0.1:{MODERATOR_PORT}"
  name: moderator
agents:
  enabled: true
  name: moderator
  listen_addr: "127.0.0.1:{MODERATOR_PORT}"
  capabilities: ["orchestrate"]
  task_timeout: "300s"
  max_delegation_depth: 3
  retry:
    max_retries: 1
    backoff: 1s
  fallback:
    enabled: true
  circuit_breaker:
    failure_threshold: 5
    cooldown_period: 30s
  rate_limit:
    send_burst: 30
  remote:
{peers}
llm:
  use: volcengine_agent/{model}
  max_retries: 2
  timeout: 120s
  volcengine_agent:
    provider: volcengine
    api_type: openai
    api_key: "{API_KEY}"
    base_url: "{BASE_URL}"
    models:
      - name: "{model}"
        temperature: 0.5
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
  你是狼人杀主持人（上帝视角），负责调度夜晚各角色行动并宣布结果。
  本局玩家座位与身份（仅你可见，不可泄露给玩家）：
  {seats_str}
  你必须用 delegate_to_agent 工具委托对应角色 agent 执行行动，不要自己编造结果。
"""


def write(path, content):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)


def start(name, cfg):
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


def send(addr, text, timeout=300):
    url = f"http://{addr}/jsonrpc"
    body = json.dumps({
        "jsonrpc": "2.0", "id": "play-" + str(int(time.time())),
        "method": "tasks/send",
        "params": {"id": "t" + str(random.randint(1000, 9999)), "sessionId": "play",
                   "message": {"role": "user", "parts": [{"text": text}]}},
    }).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.loads(resp.read())
    if "error" in data:
        return None, data["error"]
    text = ""
    for a in data.get("result", {}).get("artifacts", []):
        for p in a.get("parts", []):
            text += p.get("text", "")
    return text, None


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
    ap = argparse.ArgumentParser(description="一句话开局狼人杀")
    ap.add_argument("--players", type=int, default=9, help="玩家数（默认 9）")
    ap.add_argument("--rounds", type=int, default=1, help="夜晚回合数（默认 1）")
    ap.add_argument("--verbose", "-v", action="store_true",
                    help="实时显示各角色 + 主持人的中间对话（LLM 回复、委托过程）")
    args = ap.parse_args()

    log("🎲 开一局狼人杀！随机分配身份与模型...")
    seats = assign_roles(args.players)
    log("座位分配：")
    for s, r in seats.items():
        m = random.choice(MODELS)
        log(f"  {s} 号 → {r}  (若为角色 agent，模型: {m})")

    if os.path.exists(RUNTIME):
        shutil.rmtree(RUNTIME)
    os.makedirs(RUNTIME)

    # 生成角色 config（每个角色随机模型）
    role_model = {}
    for role, (port, caps, sp) in ROLE_AGENTS.items():
        m = random.choice(MODELS)
        role_model[role] = m
        write(os.path.join(RUNTIME, role, "config.yaml"), gen_role_cfg(role, port, caps, sp, m))

    mod_model = random.choice(MODELS)
    role_model["moderator"] = mod_model
    write(os.path.join(RUNTIME, "moderator", "config.yaml"), gen_mod_cfg(mod_model, seats))

    log("\n启动角色进程：")
    for role in ROLE_AGENTS:
        start(role, os.path.join(RUNTIME, role, "config.yaml"))
    log("等待角色就绪：")
    for role, (port, _, _) in ROLE_AGENTS.items():
        if not wait_ready(f"127.0.0.1:{port}"):
            log(f"✗ {role} 启动失败"); cleanup(); sys.exit(1)
        log(f"  {role} ✓ ({role_model[role]})")

    log("\n启动主持人：")
    start("moderator", os.path.join(RUNTIME, "moderator", "config.yaml"))
    if not wait_ready(f"127.0.0.1:{MODERATOR_PORT}"):
        log("✗ 主持人启动失败"); cleanup(); sys.exit(1)
    log(f"  主持人 ✓ ({mod_model})")

    # verbose 模式：后台 tail 各角色 + 主持人日志，实时打印中间对话
    tail_proc = None
    if args.verbose:
        import glob
        log_files = []
        for role in list(ROLE_AGENTS.keys()) + ["moderator"]:
            lf = os.path.join(RUNTIME, role, f"{role}.log")
            if os.path.exists(lf):
                log_files.append(lf)
        # 清空旧日志内容，从空开始 tail
        for lf in log_files:
            open(lf, "w").close()
        log("=" * 50)
        log("🔊 verbose 模式：实时显示中间对话（LLM 回复/委托）")
        log("   日志是 JSON 行，关键字段：msg=llm.complete/tool.complete/agent.delegate.*")
        log("=" * 50)
        # 用 tail -F 跟踪多文件，python 解析每行提取关键内容
        cmd = ["tail", "-F"] + log_files
        tail_proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        import threading
        def pump():
            for raw in tail_proc.stdout:
                line = raw.decode("utf-8", "replace").strip()
                if not line:
                    continue
                # 提取关键字段：哪个角色 + 哪个事件 + 摘要
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                path = obj.get("path", "")
                role = os.path.basename(os.path.dirname(path)) if path else "?"
                msg = obj.get("msg", "")
                if msg == "llm.complete":
                    # LLM 回复内容不在日志里（只有 token 数），跳过
                    pass
                elif msg == "tool.start":
                    tool = obj.get("tool", "")
                    inp = obj.get("input", "")[:200]
                    print(f"\n  [{role}] 🔧 调用工具 {tool}: {inp}", flush=True)
                elif msg == "tool.complete":
                    tool = obj.get("tool", "")
                    out = obj.get("output", "")[:300]
                    err = obj.get("is_error", False)
                    mark = "❌" if err else "✓"
                    print(f"\n  [{role}] {mark} {tool} 返回: {out}", flush=True)
                elif msg == "agent.delegate.sent":
                    to = obj.get("to", "")
                    print(f"\n  [{role}] ➡ 委托发给 {to}", flush=True)
                elif msg == "agent.delegate.received":
                    frm = obj.get("from", "")
                    st = obj.get("status", "")
                    print(f"\n  [{role}] ⬅ {frm} 回报: {st}", flush=True)
                elif msg == "turn.complete":
                    print(f"\n  [{role}] —— 轮次结束 ——", flush=True)
        threading.Thread(target=pump, daemon=True).start()

    for night in range(1, args.rounds + 1):
        log(f"\n{'='*50}\n🌙 第 {night} 夜\n{'='*50}")
        task = (
            f"现在是第 {night} 夜。请按狼人杀规则依次：\n"
            f"1. delegate_to_agent(agent='werewolf', task='今夜刀杀目标，从存活玩家中选一个非狼人')\n"
            f"2. delegate_to_agent(agent='seer', task='查验一名玩家身份')\n"
            f"3. delegate_to_agent(agent='guard', task='守护一名玩家')\n"
            f"4. delegate_to_agent(agent='witch', task='根据今夜被刀者决定是否用解药')\n"
            f"每个用 sync 模式。最后宣布：谁死了、谁存活，进入白天。"
        )
        log("主持人指挥夜晚行动（真实 LLM 调用，约 2~4 分钟）...")
        text, err = send(f"127.0.0.1:{MODERATOR_PORT}", task, timeout=400)
        if err:
            log(f"✗ 错误: {err}")
            log("日志: " + os.path.join(RUNTIME, "moderator", "moderator.log"))
            cleanup(); sys.exit(1)
        print("\n" + text + "\n")

    # 复盘
    log("=" * 50)
    log("📋 请求主持人复盘...")
    log("=" * 50)
    recap, rerr = send(f"127.0.0.1:{MODERATOR_PORT}",
                       "游戏结束。请做完整复盘：1) 列出所有玩家真实身份；"
                       "2) 回顾每个夜晚谁被刀/被救/被毒；3) 每天投票出局谁；"
                       "4) 最终哪方胜利、为什么。要简洁。", timeout=180)
    if rerr:
        log(f"复盘失败: {rerr}")
    else:
        print("\n主持人复盘:\n" + recap + "\n")

    log("\n✓ 对局结束。模型分配：")
    for r, m in role_model.items():
        log(f"  {r}: {m}")
    if tail_proc:
        try: tail_proc.terminate()
        except: pass
    cleanup()
    log("进程已清理。")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        cleanup(); sys.exit(130)
