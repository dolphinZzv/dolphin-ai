import { useTheme } from "next-themes";
import { useAuth } from "@/hooks/useAuth";
import { LogOut, Sun, Moon, Bell } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";

export function TopBar() {
  const { logout } = useAuth();
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  return (
    <header className="flex h-14 items-center gap-2 border-b bg-card px-4">
      <div className="flex-1" />

      <div className="flex items-center gap-1 ml-auto">
        {/* Notification bell */}
        <Button variant="ghost" size="icon" className="h-9 w-9" aria-label="通知" onClick={() => toast.message("通知功能开发中")}>
          <Bell className="h-4 w-4" />
        </Button>

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
