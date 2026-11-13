import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import 'bootstrap-icons/font/bootstrap-icons.css'
import './styles/app.scss'
import { followSystemTheme } from './lib/theme'

followSystemTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
