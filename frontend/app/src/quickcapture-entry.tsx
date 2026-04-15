import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QuickCaptureApp } from '@/components/quickcapture-app'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QuickCaptureApp />
  </StrictMode>,
)
