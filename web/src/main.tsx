import React from 'react'
import ReactDOM from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import './styles/shadcn.css'
import './index.css'
import './console.css'
import './styles/app.css'
import './styles/components.css'
import Appearance from './Appearance'
import { ThemePreferenceProvider } from './themePreference'
import { markStandalone, registerServiceWorker } from './pwa'

// Makes the panel installable and keeps the shell available offline. No-op in
// dev and on insecure origins.
registerServiceWorker()
markStandalone()

// A data router lets detail editors guard both link clicks and browser Back.
const router = createBrowserRouter([{ path: '*', element: <ThemePreferenceProvider><Appearance /></ThemePreferenceProvider> }])

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>,
)
