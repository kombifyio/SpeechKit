import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import { SettingsApp } from '@/components/settings-app'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <SettingsApp />
  </StrictMode>,
)
