import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
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
  const rowCount = rows?.length ?? 0
  const hardBlockCount = rows?.filter((r) => r.blocked).length ?? 0
  const rateCapCount = rows?.filter((r) => !r.blocked).length ?? 0
  const concurrentCount = rows?.filter((r) => r.concurrent_limit != null).length ?? 0

  function openNew() {
    setFormErr(null)
    setEditing('new')
  }

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
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow="EDGE"
          context="Pre-auth traffic policy"
          title="Blocked IPs"
          description="Apply hard blocks and per-IP rate caps before authentication, so noisy traffic is stopped at the gateway edge."
          action={
            <button onClick={openNew} className="btn btn-primary h-10">
              Block an IP
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: 'Total', value: rowCount },
            { label: 'Hard blocks', value: hardBlockCount, tone: hardBlockCount > 0 ? 'danger' : undefined },
            { label: 'Rate caps', value: rateCapCount },
            { label: 'Concurrent caps', value: concurrentCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : 'Could not load blocked IPs.'}
          </ErrorNotice>
        )}

        {rows && rows.length === 0 && (
          <EmptyState
            eyebrow="No IP policy rows"
            title="No addresses are currently blocked or capped."
            description="Add a hard block for abusive traffic, or set request, token, and concurrency caps for noisy clients."
            action={
              <button onClick={openNew} className="btn btn-primary h-10">
                Block an IP
              </button>
            }
          />
        )}

        {rows && rows.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
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
              <tbody className="divide-y divide-line">
                {rows.map((row) => (
                  <tr key={row.ip} className="transition-colors hover:bg-surface-2">
                    <Td className="font-mono text-xs">{row.ip}</Td>
                    <Td>
                      <PolicyBadge blocked={row.blocked} />
                    </Td>
                    <Td className="text-right tabular-nums">{row.rpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.tpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.concurrent_limit ?? '—'}</Td>
                    <Td className="text-muted">{row.reason || '—'}</Td>
                    <Td className="text-muted">{row.created_by || '—'}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => {
                          setFormErr(null)
                          setEditing(row)
                        }}
                        className="mr-3 text-muted underline-offset-4 hover:text-ink hover:underline"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(row)}
                        disabled={del.isPending}
                        className="btn-danger hover:underline disabled:opacity-50"
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
          <div className="mt-3">
            <ErrorNotice>
              {del.error instanceof ApiError ? del.error.message : 'Unblock failed.'}
            </ErrorNotice>
          </div>
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
    <span className="badge badge-danger">
      Hard block
    </span>
  ) : (
    <span className="badge badge-warning">
      Rate cap
    </span>
  )
}
