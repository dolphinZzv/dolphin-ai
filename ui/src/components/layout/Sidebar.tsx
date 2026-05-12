import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { LayoutDashboard, Bot, FolderKanban } from "lucide-react";

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "首页" },
  { to: "/projects", icon: FolderKanban, label: "项目" },
  { to: "/agents", icon: Bot, label: "Agent" },
];

export function Sidebar() {
  return (
    <aside className="hidden w-60 flex-col border-r bg-card lg:flex">
      {/* Logo */}
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-sm font-bold text-primary-foreground">
          C
        </div>
        <span className="font-semibold">Chick</span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 p-2">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-primary/10 text-primary font-medium"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon className="h-4 w-4" />
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* User area */}
      <div className="border-t p-3">
        <div className="flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted-foreground">
          <div className="h-2 w-2 rounded-full bg-green-500" />
          <span className="flex-1">在线</span>
        </div>
      </div>
    </aside>
  );
}
