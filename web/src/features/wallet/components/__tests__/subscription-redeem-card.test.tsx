import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { redeemSubscriptionCode } from '@/features/subscriptions/api'

import { SubscriptionRedeemCard } from '../subscription-redeem-card'

vi.mock('@/features/subscriptions/api', () => ({
  redeemSubscriptionCode: vi.fn(),
}))

describe('SubscriptionRedeemCard', () => {
  beforeEach(() => {
    vi.mocked(redeemSubscriptionCode).mockReset()
  })

  it('redeems a trimmed shop code and refreshes subscriptions after success', async () => {
    const user = userEvent.setup()
    const onSuccess = vi.fn()
    vi.mocked(redeemSubscriptionCode).mockResolvedValue({
      success: true,
      data: { subscription: {} as never },
    })

    render(<SubscriptionRedeemCard onSuccess={onSuccess} />)

    const redeemButton = screen.getByRole('button', {
      name: 'Redeem subscription',
    })
    expect(redeemButton).toBeDisabled()

    await user.type(
      screen.getByPlaceholderText('Subscription code'),
      ' APIC-TEST-CODE '
    )
    expect(redeemButton).toBeEnabled()
    await user.click(redeemButton)

    await waitFor(() => {
      expect(redeemSubscriptionCode).toHaveBeenCalledWith('APIC-TEST-CODE')
      expect(onSuccess).toHaveBeenCalledOnce()
    })
    expect(screen.getByPlaceholderText('Subscription code')).toHaveValue('')
  })

  it('submits from the keyboard', async () => {
    vi.mocked(redeemSubscriptionCode).mockResolvedValue({
      success: false,
      message: 'invalid code',
    })
    render(<SubscriptionRedeemCard />)

    const input = screen.getByPlaceholderText('Subscription code')
    fireEvent.change(input, { target: { value: 'APIC-TEST-CODE' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => {
      expect(redeemSubscriptionCode).toHaveBeenCalledWith('APIC-TEST-CODE')
    })
  })
})
