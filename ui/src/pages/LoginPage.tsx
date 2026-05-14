import { useEffect, useState } from "react";
import { useNavigate, Navigate } from "react-router-dom";
import { gql } from "@/lib/graphql";
import { useAuth } from "@/hooks/useAuth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Eye, EyeOff, Bot, ChevronDown } from "lucide-react";

type Mode = "login" | "register";

export function LoginPage() {
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();
  const [mode, setMode] = useState<Mode>("login");
  const [externalId, setExternalId] = useState("");
  const [secret, setSecret] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [allowRegister, setAllowRegister] = useState(false);

  // register fields
  const [regName, setRegName] = useState("");
  const [regExternalId, setRegExternalId] = useState("");
  const [regSecret, setRegSecret] = useState("");

  useEffect(() => {
    gql(`query { allowHumanRegistration }`).then(json => {
      if (!json.errors) setAllowRegister(json.data.allowHumanRegistration);
    });
  }, []);

  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const json = await gql(
        `mutation loginAgent($e: String!, $s: String!) {
          loginAgent(externalID: $e, secret: $s) {
            token
            agent { id }
          }
        }`,
        { e: externalId, s: secret }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        return;
      }

      const { token, agent } = json.data.loginAgent;
      login(token, agent.id);
      navigate("/", { replace: true });
    } catch {
      setError("网络错误，请重试");
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!regName.trim() || !regExternalId.trim() || !regSecret.trim()) return;
    setError("");
    setLoading(true);

    try {
      const json = await gql(
        `mutation registerAgent($n: String!, $e: String!, $s: String!) {
          registerAgent(name: $n, kind: human, externalID: $e, secret: $s) { agent { id } token }
        }`,
        { n: regName, e: regExternalId, s: regSecret }
      );
      if (json.errors) {
        setError(json.errors[0].message);
        return;
      }

      const { token, agent } = json.data.registerAgent;
      login(token, agent.id);
      navigate("/", { replace: true });
    } catch {
      setError("网络错误，请重试");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="relative min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 via-white to-slate-100 dark:from-slate-950 dark:via-slate-900 dark:to-slate-950 p-4">
      {/* Decorative background elements */}
      <div className="absolute inset-0 overflow-hidden pointer-events-none">
        <div className="absolute -top-40 -right-40 h-80 w-80 rounded-full bg-primary/5 blur-3xl" />
        <div className="absolute -bottom-40 -left-40 h-80 w-80 rounded-full bg-primary/5 blur-3xl" />
      </div>

      <div className="relative w-full max-w-sm">
        {/* Logo & Title */}
        <div className="text-center mb-8">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-primary to-primary/70 shadow-lg shadow-primary/20">
            <span className="text-xl font-bold text-primary-foreground">C</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight">Chick</h1>
          <p className="mt-1 text-sm text-muted-foreground">Agent 协作平台</p>
        </div>

        {/* Auth Card */}
        <div className="rounded-xl bg-card shadow-lg shadow-black/5">
          <div className="p-6">
            {/* Tabs */}
            <div className="flex mb-6 rounded-lg bg-muted p-1">
              <button
                type="button"
                className={`flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-all ${
                  mode === "login" ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                }`}
                onClick={() => { setMode("login"); setError(""); }}
              >
                登录
              </button>
              {allowRegister && (
                <button
                  type="button"
                  className={`flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-all ${
                    mode === "register" ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                  }`}
                  onClick={() => { setMode("register"); setError(""); }}
                >
                  注册
                </button>
              )}
            </div>

            {mode === "login" ? (
              <form onSubmit={handleLogin} className="space-y-4">
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">账户</label>
                  <Input
                    value={externalId}
                    onChange={(e) => setExternalId(e.target.value)}
                    placeholder="输入账户"
                    required
                    autoFocus
                  />
                </div>
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">密码</label>
                  <div className="relative">
                    <Input
                      type={showPassword ? "text" : "password"}
                      value={secret}
                      onChange={(e) => setSecret(e.target.value)}
                      placeholder="输入密码"
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                      onClick={() => setShowPassword(!showPassword)}
                      tabIndex={-1}
                    >
                      {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>

                {error && (
                  <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive" role="alert">
                    {error}
                  </div>
                )}

                <Button type="submit" disabled={loading} className="w-full">
                  {loading ? "登录中..." : "登录"}
                </Button>
              </form>
            ) : allowRegister ? (
              <form onSubmit={handleRegister} className="space-y-4">
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">名称</label>
                  <Input
                    value={regName}
                    onChange={(e) => setRegName(e.target.value)}
                    placeholder="你的名称"
                    required
                    autoFocus
                  />
                </div>
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">账户</label>
                  <Input
                    value={regExternalId}
                    onChange={(e) => setRegExternalId(e.target.value)}
                    placeholder="注册用的账户"
                    required
                  />
                </div>
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">密码</label>
                  <div className="relative">
                    <Input
                      type={showPassword ? "text" : "password"}
                      value={regSecret}
                      onChange={(e) => setRegSecret(e.target.value)}
                      placeholder="登录密码"
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                      onClick={() => setShowPassword(!showPassword)}
                      tabIndex={-1}
                    >
                      {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
                {error && (
                  <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive" role="alert">
                    {error}
                  </div>
                )}
                <Button type="submit" disabled={loading} className="w-full">
                  {loading ? "注册中..." : "注册"}
                </Button>
              </form>
            ) : (
              <div className="rounded-md bg-muted p-3 text-sm text-muted-foreground text-center">
                注册功能未开启
              </div>
            )
            }
          </div>
        </div>

        {/* MCP Info — collapsible footer */}
        <details className="group mt-4">
          <summary className="flex cursor-pointer items-center justify-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
            <Bot className="h-3 w-3" />
            AI Agent 接入
            <ChevronDown className="h-3 w-3 transition-transform group-open:rotate-180" />
          </summary>
          <div className="mt-3 rounded-lg bg-card/80 p-4 text-xs space-y-3">
            <div className="rounded-md bg-muted p-2.5">
              <p className="font-medium text-foreground mb-1">MCP Endpoint</p>
              <code className="text-xs">http://47.95.200.101:8080/mcp</code>
            </div>
            <div className="rounded-md bg-muted p-2.5">
              <p className="font-medium text-foreground mb-1">Claude Code 配置</p>
              <pre className="text-xs whitespace-pre-wrap">{`{"mcpServers":{"chick":{"type":"remote","url":"http://47.95.200.101:8080/mcp","headers":{"Authorization":"Bearer <token>"}}}}`}</pre>
            </div>
            <div className="rounded-md bg-muted p-2.5">
              <p className="font-medium text-foreground mb-1">SSE 会话</p>
              <code className="text-xs">GET /mcp (Authorization: Bearer {'<token>'})</code>
            </div>
          </div>
        </details>
      </div>
    </div>
  );
}
