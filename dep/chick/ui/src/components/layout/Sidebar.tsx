import { useState } from "react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { FolderKanban, PanelLeftClose, PanelLeftOpen } from "lucide-react";

const navItems = [
  { to: "/projects", icon: FolderKanban, label: "项目" },
];

export function Sidebar() {
  const [collapsed, setCollapsed] = useState(true);

  return (
    <aside
      className={cn(
        "hidden flex-col border-r bg-card transition-all duration-200 lg:flex",
        collapsed ? "w-16" : "w-60"
      )}
    >
      {/* Logo */}
      <div
        className={cn(
          "flex h-14 items-center border-b transition-all",
          collapsed ? "justify-center px-0" : "gap-2 px-4"
        )}
      >
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-sm font-bold text-primary-foreground">
          C
        </div>
        <span className={cn("font-semibold", collapsed && "hidden")}>Chick</span>
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
                "flex items-center rounded-lg px-3 py-2 text-sm transition-colors",
                collapsed ? "justify-center" : "gap-3",
                isActive
                  ? "bg-primary/10 text-primary font-medium"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon className="h-4 w-4 shrink-0" />
            <span className={cn(collapsed && "hidden")}>{item.label}</span>
          </NavLink>
        ))}
      </nav>

      {/* User area */}
      <div className="border-t p-3">
        <div
          className={cn(
            "flex items-center rounded-lg px-3 py-2 text-sm text-muted-foreground",
            collapsed ? "justify-center" : "gap-3"
          )}
        >
          <div className="h-2 w-2 shrink-0 rounded-full bg-green-500" />
          <span className={cn("flex-1", collapsed && "hidden")}>在线</span>
        </div>
      </div>

      {/* Toggle button */}
      <div className="border-t p-2">
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="flex w-full items-center justify-center rounded-lg px-3 py-2 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          title={collapsed ? "展开侧栏" : "收起侧栏"}
        >
          {collapsed ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
        </button>
      </div>
    </aside>
  );
}
