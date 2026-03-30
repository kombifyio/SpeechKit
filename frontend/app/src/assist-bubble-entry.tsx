import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import { AssistBubble } from '@/components/assist-bubble'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AssistBubble />
  </StrictMode>,
)
