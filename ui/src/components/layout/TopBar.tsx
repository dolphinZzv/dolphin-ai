import { useTheme } from "next-themes";
import { useAuth } from "@/hooks/useAuth";
import { useSubscription } from "@/hooks/useSubscription";
import { LogOut, Sun, Moon, Bell, CheckCheck, MessageSquare, UserPlus, ArrowRightCircle, AlertCircle, FileText, GitPullRequest, CheckSquare, UserCheck, RefreshCw, Radio, Star } from "lucide-react";
import { useEffect, useState, useCallback, useRef } from "react";
import { Button } from "@/components/ui/button";
import { gql } from "@/lib/graphql";
import { cn } from "@/lib/utils";

interface Notification {
  id: string;
  number: number;
  agentID: string;
  notificationType: string;
  issueID?: string | null;
  proposalID?: string | null;
  taskID?: string | null;
  projectID?: string | null;
  message: string;
  read: boolean;
  createdAt: string;
}

const notifIconMap: Record<string, React.ElementType> = {
  issue_assigned: UserPlus,
  comment_mention: MessageSquare,
  issue_state_changed: ArrowRightCircle,
  status_change_request: AlertCircle,
  proposal_created: FileText,
  proposal_state_changed: GitPullRequest,
  task_created: CheckSquare,
  task_assigned: UserCheck,
  task_state_changed: RefreshCw,
  agent_status_changed: Radio,
  feedback_received: Star,
};

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return "刚刚";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}小时前`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}天前`;
  return new Date(dateStr).toLocaleDateString("zh-CN");
}

export function TopBar() {
  const { logout, agent } = useAuth();
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  const [notifs, setNotifs] = useState<Notification[]>([]);
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const agentId = agent?.agentId;

  const fetchNotifs = useCallback(async () => {
    if (!agentId) return;
    try {
      const json = await gql<{ notifications: Notification[] }>(
        `query notifications($aid: ID!) { notifications(agentID: $aid) { id number agentID notificationType issueID proposalID taskID projectID message read createdAt } }`,
        { aid: agentId }
      );
      if (json.data?.notifications) {
        setNotifs(json.data.notifications);
      }
    } catch { /* ignore */ }
  }, [agentId]);

  useEffect(() => {
    fetchNotifs();
    const interval = setInterval(fetchNotifs, 30000);
    return () => clearInterval(interval);
  }, [fetchNotifs]);

  // Real-time subscription for new notifications
  useSubscription(
    `subscription agentNotifications($aid: ID!) { agentNotifications(agentID: $aid) { id number agentID notificationType issueID message read createdAt } }`,
    agentId ? { aid: agentId } : undefined,
    (data: any) => {
      if (data?.agentNotifications) {
        setNotifs(prev => [data.agentNotifications, ...prev]);
      }
    }
  );

  // Close dropdown on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const unreadCount = notifs.filter(n => !n.read).length;

  const handleMarkRead = useCallback(async (id: string) => {
    try {
      await gql(
        `mutation markRead($id: ID!) { markNotificationRead(id: $id) { id read } }`,
        { id }
      );
      setNotifs(prev => prev.map(n => n.id === id ? { ...n, read: true } : n));
    } catch { /* ignore */ }
  }, []);

  const handleMarkAllRead = useCallback(async () => {
    if (!agentId) return;
    try {
      await gql(
        `mutation markAllRead($aid: ID!) { markAllNotificationsRead(agentID: $aid) }`,
        { aid: agentId }
      );
      setNotifs(prev => prev.map(n => ({ ...n, read: true })));
    } catch { /* ignore */ }
  }, [agentId]);

  useEffect(() => { setMounted(true); }, []);

  return (
    <header className="flex h-14 items-center gap-2 border-b bg-card px-4">
      <div className="flex-1" />

      <div className="flex items-center gap-1 ml-auto relative" ref={dropdownRef}>
        {/* Notification bell */}
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9 relative"
          aria-label="通知"
          onClick={() => setOpen(v => !v)}
        >
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-[16px] items-center justify-center rounded-full bg-red-500 text-[10px] font-medium text-white px-1">
              {unreadCount > 99 ? "99+" : unreadCount}
            </span>
          )}
        </Button>

        {/* Notification dropdown */}
        {open && (
          <div className="absolute top-full right-0 mt-2 w-80 sm:w-96 rounded-lg border bg-card shadow-lg z-50 overflow-hidden">
            {/* Header */}
            <div className="flex items-center justify-between px-4 py-3 border-b">
              <h3 className="text-sm font-semibold">通知</h3>
              {unreadCount > 0 && (
                <Button variant="ghost" size="sm" className="h-7 text-xs gap-1" onClick={handleMarkAllRead}>
                  <CheckCheck className="h-3.5 w-3.5" />
                  全部已读
                </Button>
              )}
            </div>

            {/* List */}
            <div className="max-h-96 overflow-y-auto">
              {notifs.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
                  <Bell className="h-8 w-8 mb-2" />
                  <p className="text-sm">暂无通知</p>
                </div>
              ) : (
                <div className="divide-y">
                  {notifs.map(n => {
                    const Icon = notifIconMap[n.notificationType] || Bell;
                    return (
                      <button
                        key={n.id}
                        className={cn(
                          "w-full text-left px-4 py-3 hover:bg-accent transition-colors flex items-start gap-3",
                          !n.read && "bg-accent/30"
                        )}
                        onClick={() => { if (!n.read) handleMarkRead(n.id); }}
                      >
                        <Icon className={cn(
                          "h-4 w-4 mt-0.5 shrink-0",
                          n.read ? "text-muted-foreground" : "text-primary"
                        )} />
                        <div className="flex-1 min-w-0">
                          <p className={cn("text-sm leading-snug", !n.read && "font-medium")}>
                            {n.message}
                          </p>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            {relativeTime(n.createdAt)}
                          </p>
                        </div>
                        {!n.read && (
                          <span className="h-2 w-2 rounded-full bg-primary shrink-0 mt-1.5" />
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Theme toggle */}
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          aria-label="切换主题"
        >
          {mounted && theme === "dark" ? (
            <Sun className="h-4 w-4" />
          ) : (
            <Moon className="h-4 w-4" />
          )}
        </Button>

        {/* Logout */}
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9"
          onClick={logout}
          aria-label="退出登录"
        >
          <LogOut className="h-4 w-4" />
        </Button>
      </div>
    </header>
  );
}
