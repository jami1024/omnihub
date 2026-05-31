import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './lib/auth'
import { LoginPage } from './pages/Login'
import { DashboardPage } from './pages/Dashboard'

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <Protected>
            <DashboardPage />
          </Protected>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

function Protected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center text-zinc-500">
        Loading…
      </div>
    )
  }
  if (!me) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
