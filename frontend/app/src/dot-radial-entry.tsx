import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import '@/lib/control-plane-fetch'
import { DotRadialOverlay } from '@/components/overlay-surfaces'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <DotRadialOverlay />
  </StrictMode>,
)
