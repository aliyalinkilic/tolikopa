import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
// Applies the stored theme + language before the first render (side-effect import).
import '@/lib/prefs'
import { App } from './App'

const root = document.getElementById('root')
if (!root) throw new Error('#root not found')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
