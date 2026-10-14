import { useEffect } from 'react'

const APP = 'ToliKOPA'

/** `<title>` = "Page · ToliKOPA". Pass the already-translated page name. */
export function useDocumentTitle(page?: string | null): void {
  useEffect(() => {
    document.title = page ? `${page} · ${APP}` : APP
  }, [page])
}
