import { useCallback, useEffect, useState } from 'react'
import { Layout, type NavItem } from '@/components/Layout'
import { useNodes } from '@/hooks/useNodes'
import { DashboardPage } from '@/pages/DashboardPage'
import { NodesPage } from '@/pages/NodesPage'
import { HTTPStreamPage } from '@/pages/HTTPStreamPage'
import { SQLStreamPage } from '@/pages/SQLStreamPage'
import { SessionsPage } from '@/pages/SessionsPage'
import { DataStudioPage } from '@/pages/DataStudioPage'

type PageID = 'dashboard' | 'nodes' | 'http' | 'sql' | 'sessions' | 'data-studio'

const PAGES: ReadonlyArray<{ id: PageID; label: string }> = [
  { id: 'dashboard', label: 'Dashboard' },
  { id: 'nodes', label: 'Nodes' },
  { id: 'http', label: 'HTTP requests' },
  { id: 'sql', label: 'SQL statements' },
  { id: 'sessions', label: 'Sessions' },
  { id: 'data-studio', label: 'Data Studio' },
]

function pageFromHash(): PageID {
  const raw = window.location.hash.replace(/^#\/?/, '')
  const known = PAGES.find((p) => p.id === raw)
  return known?.id ?? 'dashboard'
}

function App(): React.JSX.Element {
  const [page, setPage] = useState<PageID>(pageFromHash)
  const { nodes, isError } = useNodes()

  useEffect(() => {
    const onHash = (): void => setPage(pageFromHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const navigate = useCallback((id: string): void => {
    if (PAGES.some((p) => p.id === id)) {
      window.location.hash = `#/${id}`
    }
  }, [])

  const items: NavItem[] = PAGES.map((p) =>
    p.id === 'nodes'
      ? { id: p.id, label: p.label, badge: nodes.length }
      : { id: p.id, label: p.label },
  )

  return (
    <Layout current={page} items={items} onNavigate={navigate} serverHealthy={!isError}>
      {page === 'dashboard' && <DashboardPage />}
      {page === 'nodes' && <NodesPage />}
      {page === 'http' && <HTTPStreamPage />}
      {page === 'sql' && <SQLStreamPage />}
      {page === 'sessions' && <SessionsPage />}
      {page === 'data-studio' && <DataStudioPage />}
    </Layout>
  )
}

export default App
