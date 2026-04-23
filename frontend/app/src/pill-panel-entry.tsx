import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import '@/lib/control-plane-fetch'
import { PillPanelOverlay } from '@/components/overlay-surfaces'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PillPanelOverlay />
  </StrictMode>,
)
