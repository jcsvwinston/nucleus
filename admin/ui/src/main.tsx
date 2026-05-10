import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from '@/App'
import '@/index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Real-time observability: don't refetch idle queries automatically.
      // Streaming RPCs handle their own cadence; cached snapshots are short-
      // lived and revalidated on focus.
      refetchOnWindowFocus: true,
      staleTime: 5_000,
      retry: 1,
    },
  },
})

const rootEl = document.getElementById('root')
if (!rootEl) {
  throw new Error('Could not find #root element to mount the admin UI.')
}

createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
