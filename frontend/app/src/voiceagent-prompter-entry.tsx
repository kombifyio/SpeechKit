import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/index.css'
import '@/lib/control-plane-fetch'
import { VoiceAgentPrompter } from '@/components/voiceagent-prompter'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <VoiceAgentPrompter />
  </StrictMode>,
)
