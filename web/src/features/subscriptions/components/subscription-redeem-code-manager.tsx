/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { Download, KeyRound, RefreshCw, ShieldAlert } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  generateSubscriptionRedeemCodes,
  getAdminPlans,
  getSubscriptionRedeemCodes,
  revokeSubscriptionRedeemCode,
  type IssuedSubscriptionCode,
  type SubscriptionRedeemCodeRecord,
} from '../api'
import type { PlanRecord } from '../types'

function downloadCodes(codes: IssuedSubscriptionCode[], batchId: string) {
  const csv = ['code', ...codes.map(({ code }) => `"${code}"`)].join('\r\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
  const href = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = href
  link.download = `apic-subscription-codes-${batchId}.csv`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(href), 0)
}

export function SubscriptionRedeemCodeManager() {
  const { t } = useTranslation()
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [records, setRecords] = useState<SubscriptionRedeemCodeRecord[]>([])
  const [planId, setPlanId] = useState('')
  const [count, setCount] = useState(10)
  const [busy, setBusy] = useState(false)
  const [revoking, setRevoking] = useState(false)
  const [codeToRevoke, setCodeToRevoke] = useState<number | null>(null)
  const [newBatch, setNewBatch] = useState<{
    batchId: string
    codes: IssuedSubscriptionCode[]
  } | null>(null)

  const refresh = useCallback(async () => {
    const [planResponse, codeResponse] = await Promise.all([
      getAdminPlans(),
      getSubscriptionRedeemCodes(),
    ])
    setPlans(requireServerSuccess(planResponse).data || [])
    setRecords(requireServerSuccess(codeResponse).data?.items || [])
  }, [])

  useEffect(() => {
    void refresh().catch(() =>
      toast.error(t('Could not load subscription codes'))
    )
  }, [refresh, t])

  const generate = async () => {
    if (!planId || count < 1 || count > 1000 || busy) return
    setBusy(true)
    try {
      const response = requireServerSuccess(
        await generateSubscriptionRedeemCodes({
          plan_id: Number(planId),
          count,
        })
      )
      const codes = response.data?.codes || []
      const batchId = response.data?.batch_id || ''
      setNewBatch({ batchId, codes })
      await refresh()
      toast.success(t('Subscription codes generated'))
    } catch {
      toast.error(t('Could not generate subscription codes'))
    } finally {
      setBusy(false)
    }
  }

  const revoke = async (id: number) => {
    if (revoking) return
    setRevoking(true)
    try {
      requireServerSuccess(await revokeSubscriptionRedeemCode(id))
      setCodeToRevoke(null)
      await refresh()
      toast.success(t('Subscription code revoked'))
    } catch {
      toast.error(t('Could not revoke subscription code'))
    } finally {
      setRevoking(false)
    }
  }

  return (
    <Card className='shrink-0'>
      <CardHeader className='pb-3'>
        <CardTitle className='flex items-center gap-2 text-base'>
          <KeyRound className='h-4 w-4' />
          {t('Subscription redeem codes')}
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <Alert variant='default'>
          <ShieldAlert className='h-4 w-4' />
          <AlertDescription>
            {t(
              'Codes are shown only once. Save or download them now; APIC stores only code digests.'
            )}
          </AlertDescription>
        </Alert>
        <div className='flex flex-col gap-2 sm:flex-row'>
          <select
            aria-label={t('Subscription plan')}
            className='border-input bg-background h-10 rounded-md border px-3 text-sm sm:min-w-56'
            value={planId}
            onChange={(event) => setPlanId(event.target.value)}
          >
            <option value=''>{t('Select a subscription plan')}</option>
            {plans
              .filter(({ plan }) => plan.enabled)
              .map(({ plan }) => (
                <option key={plan.id} value={plan.id}>
                  {plan.title}
                </option>
              ))}
          </select>
          <Input
            type='number'
            min={1}
            max={1000}
            value={count}
            onChange={(event) => setCount(Number(event.target.value))}
            aria-label={t('Number of codes')}
            className='sm:max-w-36'
          />
          <Button
            onClick={() => void generate()}
            disabled={busy || !planId || count < 1 || count > 1000}
          >
            {busy ? (
              <RefreshCw className='h-4 w-4 animate-spin' />
            ) : (
              <KeyRound className='h-4 w-4' />
            )}
            {t('Generate codes')}
          </Button>
        </div>
        {newBatch ? (
          <div className='space-y-2 rounded-md border p-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <p className='text-sm font-medium'>
                {t('New batch: {{count}} codes', {
                  count: newBatch.codes.length,
                })}
              </p>
              <Button
                variant='outline'
                size='sm'
                onClick={() => downloadCodes(newBatch.codes, newBatch.batchId)}
              >
                <Download className='h-4 w-4' />
                {t('Download CSV')}
              </Button>
            </div>
            <textarea
              readOnly
              value={newBatch.codes.map(({ code }) => code).join('\n')}
              className='bg-muted min-h-32 w-full rounded-md p-2 font-mono text-xs'
            />
          </div>
        ) : null}
        <StaticDataTable>
          <TableHeader>
            <TableRow>
              <TableHead>{t('ID')}</TableHead>
              <TableHead>{t('Plan')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Redeemed user')}</TableHead>
              <TableHead>{t('Subscription')}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <TableRow key={record.id}>
                <TableCell>{record.id}</TableCell>
                <TableCell>
                  {plans.find(({ plan }) => plan.id === record.plan_id)?.plan
                    .title || record.plan_id}
                </TableCell>
                <TableCell>{record.status}</TableCell>
                <TableCell>{record.redeemed_by || '?'}</TableCell>
                <TableCell>{record.subscription_id || '?'}</TableCell>
                <TableCell className='text-right'>
                  {record.status === 'issued' ? (
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setCodeToRevoke(record.id)}
                    >
                      {t('Revoke')}
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
            {records.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground p-4 text-center'
                >
                  {t('No subscription codes yet')}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </StaticDataTable>
      </CardContent>
      <ConfirmDialog
        open={codeToRevoke !== null}
        onOpenChange={(open) => {
          if (!open && !revoking) setCodeToRevoke(null)
        }}
        title={t('Revoke subscription code?')}
        desc={t('This unused code will no longer be redeemable.')}
        confirmText={t('Revoke')}
        destructive
        isLoading={revoking}
        handleConfirm={() => {
          if (codeToRevoke !== null) void revoke(codeToRevoke)
        }}
      />
    </Card>
  )
}
