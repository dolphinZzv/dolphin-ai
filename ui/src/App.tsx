import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Provider as UrqlProvider } from "urql";
import { ThemeProvider } from "next-themes";
import { Toaster } from "@/components/ui/sonner";
import { urqlClient } from "@/lib/urql";
import { AuthProvider } from "@/hooks/useAuth";
import { ErrorBoundary } from "@/components/shared/ErrorBoundary";
import { Layout } from "@/components/layout/Layout";
import { LoginPage } from "@/pages/LoginPage";
import { ProjectDetailPage } from "@/pages/ProjectDetailPage";
import { IssueDetailPage } from "@/pages/IssueDetailPage";
import { ProposalDetailPage } from "@/pages/ProposalDetailPage";
import { TaskDetailPage } from "@/pages/TaskDetailPage";
import { AgentDetailPage } from "@/pages/AgentDetailPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { ProjectSettingsPage } from "@/pages/ProjectSettingsPage";
import { NotFoundPage } from "@/pages/NotFoundPage";

function PageBoundary({ children }: { children: React.ReactNode }) {
  return (
    <ErrorBoundary>
      {children}
    </ErrorBoundary>
  );
}

export default function App() {
  return (
    <ErrorBoundary>
      <UrqlProvider value={urqlClient}>
        <AuthProvider>
          <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
            <BrowserRouter>
              <Routes>
                <Route path="/login" element={<PageBoundary><LoginPage /></PageBoundary>} />
                <Route element={<Layout />}>
                  <Route path="/" element={<Navigate to="/projects" replace />} />
                  <Route path="/projects" element={<PageBoundary><ProjectsPage /></PageBoundary>} />
                  <Route path="/projects/:id" element={<PageBoundary><ProjectDetailPage /></PageBoundary>} />
                  <Route path="/projects/:id/settings" element={<PageBoundary><ProjectSettingsPage /></PageBoundary>} />
                  <Route path="/issues/:id" element={<PageBoundary><IssueDetailPage /></PageBoundary>} />
                  <Route path="/proposals/:id" element={<PageBoundary><ProposalDetailPage /></PageBoundary>} />
                  <Route path="/tasks/:id" element={<PageBoundary><TaskDetailPage /></PageBoundary>} />
                  <Route path="/agents/:id" element={<PageBoundary><AgentDetailPage /></PageBoundary>} />
                </Route>
                <Route path="/404" element={<PageBoundary><NotFoundPage /></PageBoundary>} />
                <Route path="*" element={<Navigate to="/404" replace />} />
              </Routes>
            </BrowserRouter>
            <Toaster />
          </ThemeProvider>
        </AuthProvider>
      </UrqlProvider>
    </ErrorBoundary>
  );
}
