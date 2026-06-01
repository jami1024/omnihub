import { Suspense, lazy } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './lib/auth'
import { LoginPage } from './pages/Login'
import { AccountsPage } from './pages/Accounts'
import { KeysPage } from './pages/Keys'
import { BlockedIPsPage } from './pages/BlockedIPs'
import { HealthPage } from './pages/Health'
import { PricesPage } from './pages/Prices'

// The Dashboard pulls in recharts (~180 kB gzip). Lazy-load it so the
// charting library lands in its own chunk and never weighs down login
// or the management pages.
const DashboardPage = lazy(() =>
  import('./pages/Dashboard').then((m) => ({ default: m.DashboardPage })),
)

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <Protected>
            <Suspense fallback={<PageLoading />}>
              <DashboardPage />
            </Suspense>
          </Protected>
        }
      />
      <Route
        path="/accounts"
        element={
          <Protected>
            <AccountsPage />
          </Protected>
        }
      />
      <Route
        path="/keys"
        element={
          <Protected>
            <KeysPage />
          </Protected>
        }
      />
      <Route
        path="/blocked-ips"
        element={
          <Protected>
            <BlockedIPsPage />
          </Protected>
        }
      />
      <Route
        path="/health"
        element={
          <Protected>
            <HealthPage />
          </Protected>
        }
      />
      <Route
        path="/prices"
        element={
          <Protected>
            <PricesPage />
          </Protected>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

function PageLoading() {
  return (
    <div className="flex h-screen items-center justify-center text-muted">Loading…</div>
  )
}

function Protected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center text-muted">
        Loading…
      </div>
    )
  }
  if (!me) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
