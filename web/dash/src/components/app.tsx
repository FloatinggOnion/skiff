import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate, Outlet, useParams } from 'react-router'
import { AuthProvider, useAuthContext } from '@/context/auth-context'
import Setup from '@/components/setup'
import Login from '@/components/login'
import AcceptInvite from '@/components/accept-invite'
import Shell from '@/components/shell'
import ProjectsPage from '@/pages/projects'
import ProjectDetailPage from '@/pages/project-detail'
import DeploymentsPage from '@/pages/deployments'
import ActivityPage from '@/pages/activity'
import LogsPage from '@/pages/logs'
import ServerPage from '@/pages/server'
import DomainsPage from '@/pages/domains'
import EmailPage from '@/pages/email'
import DatabasesPage from '@/pages/databases'
import StoragePage from '@/pages/storage'
import BucketBrowserPage from '@/pages/bucket-browser'
import EnvPage from '@/pages/env'
import SettingsPage from '@/pages/settings'
import CliLoginPage from '@/pages/cli-login'
import { AnalyticsSkeleton } from '@/components/skeletons'

const AnalyticsPage = lazy(() => import('@/pages/analytics'))

function InviteRoute() {
  const { token } = useParams()
  return <AcceptInvite token={token ?? ''} />
}

// AuthGate short-circuits the whole shell until the visitor is signed in.
function AuthGate() {
  const auth = useAuthContext()
  if (auth.state === 'checking')
    return <div className="text-muted-foreground flex h-screen items-center justify-center">…</div>
  if (auth.state === 'setup') return <Setup onSetup={auth.setup} />
  if (auth.state === 'out') return <Login onLogin={auth.login} onCode={auth.loginCode} />
  return <Outlet />
}

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/invite/:token" element={<InviteRoute />} />
          <Route element={<AuthGate />}>
            <Route element={<Shell />}>
              <Route index element={<ProjectsPage />} />
              <Route path="projects/:name" element={<ProjectDetailPage />} />
              <Route path="deployments" element={<DeploymentsPage />} />
              <Route path="activity" element={<ActivityPage />} />
              <Route path="logs" element={<LogsPage />} />
              <Route
                path="analytics"
                element={
                  <Suspense fallback={<AnalyticsSkeleton />}>
                    <AnalyticsPage />
                  </Suspense>
                }
              />
              <Route path="server" element={<ServerPage />} />
              <Route path="domains" element={<DomainsPage />} />
              <Route path="email" element={<EmailPage />} />
              <Route path="databases" element={<DatabasesPage />} />
              <Route path="storage" element={<StoragePage />} />
              <Route path="storage/:id" element={<BucketBrowserPage />} />
              <Route path="env" element={<EnvPage />} />
              <Route path="settings" element={<SettingsPage />} />
              <Route path="cli-login" element={<CliLoginPage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}
