/*
Copyright (C) 2023-2026 QuantumNous
This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { redeemSubscriptionCode } from '@/features/subscriptions/api'
import { handleServerError } from '@/lib/handle-server-error'

export function SubscriptionRedeemCard({
  onSuccess,
}: {
  onSuccess?: () => void
}) {
  const { t } = useTranslation()
  const [code, setCode] = useState('')
  const [redeeming, setRedeeming] = useState(false)

  const redeem = async () => {
    if (!code.trim() || redeeming) return
    setRedeeming(true)
    try {
      const response = await redeemSubscriptionCode(code.trim())
      if (response.success && response.data?.subscription) {
        setCode('')
        toast.success(t('Subscription activated successfully'))
        onSuccess?.()
      } else {
        handleServerError(response, t('Subscription redemption failed'))
      }
    } catch (error) {
      handleServerError(error, t('Subscription redemption failed'))
    } finally {
      setRedeeming(false)
    }
  }

  return (
    <Card className='gap-0'>
      <CardHeader className='pb-3'>
        <CardTitle className='text-base'>
          {t('Redeem a subscription code')}
        </CardTitle>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Enter the code you received from the shop. It activates a subscription and does not add wallet balance.'
          )}
        </p>
      </CardHeader>
      <CardContent className='flex flex-col gap-2 sm:flex-row'>
        <Input
          value={code}
          onChange={(event) => setCode(event.target.value)}
          onKeyDown={(event) => event.key === 'Enter' && void redeem()}
          placeholder={t('Subscription code')}
          autoComplete='off'
          maxLength={64}
        />
        <Button
          onClick={() => void redeem()}
          disabled={redeeming || !code.trim()}
        >
          {redeeming ? t('Activating...') : t('Redeem subscription')}
        </Button>
      </CardContent>
    </Card>
  )
}
