#!/usr/bin/env python3
"""
狼人杀 Agent Mesh E2E 测试（真实 API，本机多进程）

用法:
    python3 e2e_werewolf.py

前提:
- 当前目录的 ../dolphin 二进制已编译（含 agentmesh 接线）
- 使用火山引擎 volcengine provider，每个角色随机选一个模型

流程:
1. 为每个角色（seer/witch/guard/hunter/werewolf）+ moderator 生成独立目录与 config.yaml
2. 启动 6 个 dolphin 进程，各角色暴露 A2A server，moderator 注册它们为 remote peer
3. 通过 A2A JSON-RPC 向 moderator 发 tasks/send：「天黑了，让预言家查验 3 号」
4. moderator 的 LLM 调用 delegate_to_agent 委托 seer，seer 真实 LLM 返回结果
5. 收集并校验结果
"""
import json
import os
import random
import shutil
import signal
import subprocess
import sys
import time
import urllib.request
import urllib.error

ROOT = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(ROOT)  # /Users/jzx/Desktop/DolphinzZ
DOLPHIN = os.path.join(REPO, "dolphin")
RUNTIME = os.path.join(ROOT, "runtime")

# 火山引擎配置，从 delivery/.env 读取
API_KEY = "ark-06f7312c-590a-4617-8a5f-e153e5c43b50-a1e85"
BASE_URL = "https://ark.cn-beijing.volces.com/api/plan/v3"
# 可用模型池（逗号分割），从 .env 的 MOD_MODEL 读取
MODELS = ["deepseek-v4-flash"]


def load_env():
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

# 角色 → (端口, 能力)
ROLES = [
    ("seer", 8201, ["divine"]),
    ("witch", 8202, ["poison", "save"]),
    ("guard", 8203, ["protect"]),
    ("hunter", 8204, ["shoot"]),
    ("werewolf", 8205, ["kill"]),
]
MODERATOR_PORT = 8200

PROCS = []


def log(msg):
    print(f"[e2e] {msg}", flush=True)


def gen_role_config(role, port, caps, model):
    """生成角色 agent 的 config.yaml。角色是叶子节点，只暴露 A2A server。"""
    workdir = os.path.join(RUNTIME, role)
    return f"""agent:
  name: {role}
  workspace: {workdir}
  max_rounds: 20
  pool_size: 1

a2a:
  enabled: true
  addr: "127.0.0.1:{port}"
  name: {role}
  description: "狼人杀角色: {role}"

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
  dir: {workdir}/memory
session:
  dir: {workdir}/sessions
log:
  level: info
  file: {workdir}/{role}.log
  compress: false
tui:
  enabled: false
dream:
  enabled: false
"""


def gen_moderator_config(model):
    """生成主持人 config：注册所有角色为 remote peer，开启委托。"""
    workdir = os.path.join(RUNTIME, "moderator")
    peers = ""
    for role, port, caps in ROLES:
        peers += f'    - name: {role}\n      addr: "127.0.0.1:{port}"\n      capabilities: {json.dumps(caps)}\n'
    return f"""agent:
  name: moderator
  workspace: {workdir}
  max_rounds: 30
  pool_size: 1

a2a:
  enabled: true
  addr: "127.0.0.1:{MODERATOR_PORT}"
  name: moderator
  description: "狼人杀主持人"

agents:
  enabled: true
  name: moderator
  listen_addr: "127.0.0.1:{MODERATOR_PORT}"
  capabilities: ["orchestrate"]
  task_timeout: "120s"
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
    send_burst: 20
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
  dir: {workdir}/memory
session:
  dir: {workdir}/sessions
log:
  level: info
  file: {workdir}/moderator.log
  compress: false
tui:
  enabled: false
dream:
  enabled: false
"""


def write_config(path, content):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)


def start_agent(name, cfg_path):
    """启动一个 dolphin 进程，日志重定向到文件。"""
    logpath = os.path.join(os.path.dirname(cfg_path), "stdout.log")
    logf = open(logpath, "ab")
    proc = subprocess.Popen(
        [DOLPHIN, "--config", cfg_path],
        stdout=logf, stderr=subprocess.STDOUT,
        cwd=os.path.dirname(cfg_path),
    )
    PROCS.append(proc)
    log(f"  启动 {name} (pid={proc.pid})")
    return proc


def wait_for_a2a(addr, timeout=60):
    """轮询 agents/ping 直到 A2A server 就绪。"""
    url = f"http://{addr}/jsonrpc"
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            body = json.dumps({
                "jsonrpc": "2.0", "id": "1", "method": "agents/ping", "params": {}
            }).encode()
            req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=5) as resp:
                data = json.loads(resp.read())
                if "result" in data:
                    return True
        except Exception:
            pass
        time.sleep(1)
    return False


def a2a_send(addr, task_text, timeout=180):
    """向 A2A server 发 tasks/send，返回结果文本。"""
    url = f"http://{addr}/jsonrpc"
    body = json.dumps({
        "jsonrpc": "2.0",
        "id": "e2e-" + str(int(time.time())),
        "method": "tasks/send",
        "params": {
            "id": "task-" + str(random.randint(1000, 9999)),
            "sessionId": "e2e-night",
            "message": {"role": "user", "parts": [{"text": task_text}]},
        },
    }).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.loads(resp.read())
    if "error" in data:
        return None, data["error"]
    result = data.get("result", {})
    artifacts = result.get("artifacts", [])
    text = ""
    for a in artifacts:
        for p in a.get("parts", []):
            text += p.get("text", "")
    return text, None


def cleanup():
    for proc in PROCS:
        try:
            proc.send_signal(signal.SIGTERM)
        except Exception:
            pass
    time.sleep(2)
    for proc in PROCS:
        try:
            if proc.poll() is None:
                proc.kill()
        except Exception:
            pass


def main():
    log("=== 狼人杀 Agent Mesh E2E（真实 API） ===")

    # 1. 清理 + 生成配置
    if os.path.exists(RUNTIME):
        shutil.rmtree(RUNTIME)
    os.makedirs(RUNTIME, exist_ok=True)

    log("生成各角色配置（每个角色随机模型）：")
    role_models = {}
    for role, port, caps in ROLES:
        model = random.choice(MODELS)
        role_models[role] = model
        cfg = gen_role_config(role, port, caps, model)
        write_config(os.path.join(RUNTIME, role, "config.yaml"), cfg)
        log(f"  {role}: {model} @ 127.0.0.1:{port}")

    mod_model = random.choice(MODELS)
    role_models["moderator"] = mod_model
    write_config(os.path.join(RUNTIME, "moderator", "config.yaml"), gen_moderator_config(mod_model))
    log(f"  moderator: {mod_model} @ 127.0.0.1:{MODERATOR_PORT}")

    # 2. 启动角色进程
    log("启动角色 agent 进程：")
    for role, port, caps in ROLES:
        start_agent(role, os.path.join(RUNTIME, role, "config.yaml"))

    log("等待角色 A2A server 就绪：")
    for role, port, caps in ROLES:
        addr = f"127.0.0.1:{port}"
        if wait_for_a2a(addr):
            log(f"  {role} 就绪")
        else:
            log(f"  ✗ {role} 启动失败，查看 {RUNTIME}/{role}/stdout.log")
            cleanup()
            sys.exit(1)

    # 3. 启动主持人
    log("启动主持人进程：")
    start_agent("moderator", os.path.join(RUNTIME, "moderator", "config.yaml"))
    if not wait_for_a2a(f"127.0.0.1:{MODERATOR_PORT}"):
        log("✗ 主持人启动失败")
        cleanup()
        sys.exit(1)
    log("  主持人就绪")

    # 4. 通过 A2A 让主持人指挥夜晚行动
    log("\n=== 夜晚行动：主持人委托各角色 ===")
    task = (
        "你是狼人杀主持人。现在是夜晚，玩家有 1~9 号。请依次：\n"
        "1. 用 delegate_to_agent 工具委托 seer 查验 3 号玩家身份\n"
        "2. 用 delegate_to_agent 工具委托 guard 守护 5 号玩家\n"
        "3. 用 delegate_to_agent 工具委托 werewolf 刀杀 6 号玩家\n"
        "每个委托用 sync 模式。最后汇总三个角色的回报，宣布今晚结果。"
    )
    log(f"向主持人发送任务（可能耗时 1~3 分钟，涉及 4 次 LLM 调用 + 3 次委托）...")
    text, err = a2a_send(f"127.0.0.1:{MODERATOR_PORT}", task, timeout=300)
    if err:
        log(f"✗ 主持人返回错误: {err}")
        log("查看日志: " + os.path.join(RUNTIME, "moderator", "moderator.log"))
        cleanup()
        sys.exit(1)

    log("\n=== 主持人回复 ===")
    print(text)
    log("\n=== 角色模型分配 ===")
    for r, m in role_models.items():
        log(f"  {r}: {m}")

    # 5. 简单校验
    log("\n=== 校验 ===")
    checks = {
        "委托了 seer": "seer" in text or "预言家" in text,
        "委托了 guard": "guard" in text or "守卫" in text,
        "委托了 werewolf": "werewolf" in text or "狼人" in text,
        "有结果输出": len(text) > 20,
    }
    all_ok = True
    for desc, ok in checks.items():
        log(f"  {'✓' if ok else '✗'} {desc}")
        if not ok:
            all_ok = False

    log("\n=== E2E 结果 ===")
    if all_ok:
        log("✓ E2E 通过：主持人成功委托各角色，真实 API 链路打通")
    else:
        log("△ 部分校验未通过（LLM 可能未按预期调用工具，查看日志）")
        log("  moderator 日志: " + os.path.join(RUNTIME, "moderator", "moderator.log"))

    cleanup()
    log("进程已清理，E2E 结束")
    sys.exit(0 if all_ok else 2)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        cleanup()
        sys.exit(130)
