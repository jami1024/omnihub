import { Suspense, lazy } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/auth'
import { PortalAuthProvider, usePortalAuth } from './lib/portalAuth'
import { LandingPage } from './pages/Landing'
import { UnifiedLoginPage } from './pages/UnifiedLogin'
import { AccountsPage } from './pages/Accounts'
import { GroupsPage } from './pages/Groups'
import { KeysPage } from './pages/Keys'
import { BlockedIPsPage } from './pages/BlockedIPs'
import { AlertChannelsPage } from './pages/AlertChannels'
import { RedemptionsPage } from './pages/Redemptions'
import { HealthPage } from './pages/Health'
import { PricesPage } from './pages/Prices'
import { UsersPage } from './pages/Users'
import { SettingsPage } from './pages/Settings'
import { AnnouncementsPage } from './pages/Announcements'
import { PlansPage } from './pages/Plans'
import { PortalOverviewPage } from './pages/portal/PortalOverview'
import { PortalKeysPage } from './pages/portal/PortalKeys'
import { PortalRequestsPage } from './pages/portal/PortalRequests'
import { PortalWalletPage } from './pages/portal/PortalWallet'
import { PortalPlansPage } from './pages/portal/PortalPlans'

// The admin Dashboard pulls in recharts (~180 kB gzip). Lazy-load it so
// the charting library lands in its own chunk.
const DashboardPage = lazy(() =>
  import('./pages/Dashboard').then((m) => ({ default: m.DashboardPage })),
)

// The app serves two surfaces from one bundle: the admin console under
// /admin/* and the end-user portal under /portal/*. Each tree has its
// own auth provider (separate tokens) so they never share a session.
export default function App() {
  return (
    <Routes>
      {/* Marketing landing at the bare domain. */}
      <Route path="/" element={<LandingPage />} />
      <Route
        path="/login"
        element={
          <PortalAuthProvider>
            <UnifiedLoginPage />
          </PortalAuthProvider>
        }
      />
      {/* Admin console */}
      <Route
        path="/admin/*"
        element={
          <AuthProvider>
            <AdminRoutes />
          </AuthProvider>
        }
      />
      {/* End-user portal */}
      <Route
        path="/portal/*"
        element={
          <PortalAuthProvider>
            <PortalRoutes />
          </PortalAuthProvider>
        }
      />
      {/* Any other unknown path → the user-facing portal. */}
      <Route path="*" element={<Navigate to="/portal" replace />} />
    </Routes>
  )
}

function AdminRoutes() {
  return (
    <Routes>
      <Route path="login" element={<Navigate to="/login?next=/admin" replace />} />
      <Route path="" element={<AdminProtected><Suspense fallback={<PageLoading />}><DashboardPage /></Suspense></AdminProtected>} />
      <Route path="accounts" element={<AdminProtected><AccountsPage /></AdminProtected>} />
      <Route path="groups" element={<AdminProtected><GroupsPage /></AdminProtected>} />
      <Route path="keys" element={<AdminProtected><KeysPage /></AdminProtected>} />
      <Route path="blocked-ips" element={<AdminProtected><BlockedIPsPage /></AdminProtected>} />
      <Route path="alert-channels" element={<AdminProtected><AlertChannelsPage /></AdminProtected>} />
      <Route path="health" element={<AdminProtected><HealthPage /></AdminProtected>} />
      <Route path="prices" element={<AdminProtected><PricesPage /></AdminProtected>} />
      <Route path="users" element={<AdminProtected><UsersPage /></AdminProtected>} />
      <Route path="redemptions" element={<AdminProtected><RedemptionsPage /></AdminProtected>} />
      <Route path="announcements" element={<AdminProtected><AnnouncementsPage /></AdminProtected>} />
      <Route path="plans" element={<AdminProtected><PlansPage /></AdminProtected>} />
      <Route path="settings" element={<AdminProtected><SettingsPage /></AdminProtected>} />
      <Route path="*" element={<Navigate to="/admin" replace />} />
    </Routes>
  )
}

function PortalRoutes() {
  return (
    <Routes>
      <Route path="login" element={<Navigate to="/login?next=/portal" replace />} />
      <Route path="signup" element={<Navigate to="/login?mode=signup" replace />} />
      <Route path="" element={<PortalProtected><PortalOverviewPage /></PortalProtected>} />
      <Route path="keys" element={<PortalProtected><PortalKeysPage /></PortalProtected>} />
      <Route path="requests" element={<PortalProtected><PortalRequestsPage /></PortalProtected>} />
      <Route path="wallet" element={<PortalProtected><PortalWalletPage /></PortalProtected>} />
      <Route path="plans" element={<PortalProtected><PortalPlansPage /></PortalProtected>} />
      <Route path="*" element={<Navigate to="/portal" replace />} />
    </Routes>
  )
}

function PageLoading() {
  return <div className="flex h-screen items-center justify-center text-muted">Loading…</div>
}

function AdminProtected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) return <PageLoading />
  if (!me) return <Navigate to="/admin/login" replace />
  return <>{children}</>
}

function PortalProtected({ children }: { children: React.ReactNode }) {
  const { me, loading } = usePortalAuth()
  if (loading) return <PageLoading />
  if (!me) return <Navigate to="/portal/login" replace />
  return <>{children}</>
}
