import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { Td, Th } from '../components/Table'
import { BlockedIPForm } from '../components/BlockedIPForm'
import { ApiError } from '../lib/api'
import {
  useBlockedIPs,
  useCreateBlockedIP,
  useDeleteBlockedIP,
  useUpdateBlockedIP,
  type BlockedIP,
  type BlockedIPInput,
} from '../lib/blockedIps'

type Editing = 'new' | BlockedIP | null

export function BlockedIPsPage() {
  const { data: rows, isLoading, error } = useBlockedIPs()
  const create = useCreateBlockedIP()
  const update = useUpdateBlockedIP()
  const del = useDeleteBlockedIP()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)

  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: BlockedIPInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : 'Request failed.')
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing) {
      update.mutate({ ip: editing.ip, input }, { onSuccess: close, onError })
    }
  }

  function handleDelete(row: BlockedIP) {
    if (!confirm(`Unblock ${row.ip}?`)) return
    del.mutate(row.ip)
  }

  return (
    <Layout>
      <main className="mx-auto max-w-6xl px-6 py-10">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">Blocked IPs</h2>
            <p className="text-sm text-zinc-500">
              Hard blocks (403) and per-IP rate caps (429), enforced before auth.
            </p>
          </div>
          <button
            onClick={() => {
              setFormErr(null)
              setEditing('new')
            }}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            Block an IP
          </button>
        </div>

        {isLoading && <p className="text-sm text-zinc-500">Loading…</p>}
        {error && (
          <p className="text-sm text-red-600 dark:text-red-400">
            {error instanceof ApiError ? error.message : 'Could not load blocked IPs.'}
          </p>
        )}

        {rows && rows.length === 0 && (
          <div className="rounded-lg border border-dashed border-zinc-300 p-10 text-center text-sm text-zinc-500 dark:border-zinc-700">
            No blocked IPs. Traffic from every address is allowed.
          </div>
        )}

        {rows && rows.length > 0 && (
          <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-200 bg-zinc-50 text-xs uppercase tracking-wide text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900">
                <tr>
                  <Th>IP</Th>
                  <Th>Policy</Th>
                  <Th className="text-right">RPM</Th>
                  <Th className="text-right">TPM</Th>
                  <Th className="text-right">Concurrent</Th>
                  <Th>Reason</Th>
                  <Th>Added by</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {rows.map((row) => (
                  <tr key={row.ip} className="hover:bg-zinc-50 dark:hover:bg-zinc-900/50">
                    <Td className="font-mono text-xs">{row.ip}</Td>
                    <Td>
                      <PolicyBadge blocked={row.blocked} />
                    </Td>
                    <Td className="text-right tabular-nums">{row.rpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.tpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.concurrent_limit ?? '—'}</Td>
                    <Td className="text-zinc-500">{row.reason || '—'}</Td>
                    <Td className="text-zinc-500">{row.created_by || '—'}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => {
                          setFormErr(null)
                          setEditing(row)
                        }}
                        className="mr-3 text-zinc-600 hover:underline dark:text-zinc-300"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(row)}
                        disabled={del.isPending}
                        className="text-red-600 hover:underline disabled:opacity-50 dark:text-red-400"
                      >
                        Unblock
                      </button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {del.error && (
          <p className="mt-3 text-sm text-red-600 dark:text-red-400">
            {del.error instanceof ApiError ? del.error.message : 'Unblock failed.'}
          </p>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? 'Block an IP' : `Edit ${editing.ip}`} onClose={close}>
          <BlockedIPForm
            entry={editing === 'new' ? undefined : editing}
            submitting={create.isPending || update.isPending}
            error={formErr}
            onCancel={close}
            onSubmit={handleSubmit}
          />
        </Modal>
      )}
    </Layout>
  )
}

function PolicyBadge({ blocked }: { blocked: boolean }) {
  return blocked ? (
    <span className="inline-flex items-center rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/40 dark:text-red-400">
      Hard block
    </span>
  ) : (
    <span className="inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-400">
      Rate cap
    </span>
  )
}
