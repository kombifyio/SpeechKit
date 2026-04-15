import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@/index.css'
import { QuickNoteApp } from '@/components/quicknote-app'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QuickNoteApp />
  </StrictMode>,
)
