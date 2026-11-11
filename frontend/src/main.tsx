import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Toaster } from 'sonner'
import App from './App'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, retry: 1, refetchOnWindowFocus: true },
  },
})

const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      <Toaster position="top-left" offset={{ top: 68, left: 16 }} mobileOffset={{ top: 64 }} richColors closeButton theme={prefersDark ? 'dark' : 'light'} />
    </QueryClientProvider>
  </StrictMode>,
)
