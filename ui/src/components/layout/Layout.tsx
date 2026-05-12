import { Outlet, Navigate } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { Sidebar } from "./Sidebar";
import { TopBar } from "./TopBar";
import { MobileNav } from "./MobileNav";

export function Layout() {
  const { isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="flex min-h-screen flex-col lg:flex-row">
      {/* Desktop sidebar */}
      <Sidebar />

      {/* Main area */}
      <div className="flex flex-1 flex-col">
        <TopBar />

        {/* Page content */}
        <main className="flex-1 p-3 lg:p-6">
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom nav */}
      <MobileNav />
    </div>
  );
}
