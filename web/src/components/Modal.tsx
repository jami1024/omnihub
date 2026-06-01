import { useEffect } from 'react'

// Modal is the shared centered dialog used by the management pages: a
// dimmed, lightly blurred backdrop that closes on click or Esc, with the
// panel stopping propagation so clicks inside don't dismiss it.
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string
  onClose: () => void
  children: React.ReactNode
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      className="animate-overlay-in fixed inset-0 z-30 flex items-start justify-center overflow-y-auto bg-black/40 p-6 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div
        className="animate-modal-in mt-[8vh] w-full max-w-2xl rounded-xl border border-line bg-surface p-6 shadow-panel"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-4 text-lg font-semibold tracking-tight">{title}</h3>
        {children}
      </div>
    </div>
  )
}
