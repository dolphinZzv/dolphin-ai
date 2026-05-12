import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { LayoutDashboard, Bot, FolderKanban } from "lucide-react";

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "首页" },
  { to: "/projects", icon: FolderKanban, label: "项目" },
  { to: "/agents", icon: Bot, label: "Agent" },
];

export function MobileNav() {
  return (
    <nav
      className="fixed bottom-0 left-0 right-0 z-50 flex border-t bg-card lg:hidden"
      aria-label="底部导航"
    >
      {navItems.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.to === "/"}
          className={({ isActive }) =>
            cn(
              "flex flex-1 flex-col items-center gap-1 py-2 text-xs transition-colors",
              isActive
                ? "text-primary"
                : "text-muted-foreground"
            )
          }
        >
          <item.icon className="h-5 w-5" />
          {item.label}
        </NavLink>
      ))}
    </nav>
  );
}
