import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import { DashboardApp } from '@/components/dashboard-app'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <DashboardApp />
  </StrictMode>,
)
