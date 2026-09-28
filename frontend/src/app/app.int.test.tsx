import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderRoute } from '@/test/render'

import { App } from './App'

describe('FE-INT-001 application shell', () => {
  it('FE-INT-001 redirects the index to test cases and navigates between sections', async () => {
    const { user, router } = renderRoute('/')
    expect(await screen.findByText('Login works')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/test-cases')
    await user.click(within(screen.getByRole('banner')).getByRole('link', { name: 'Test Runs' }))
    expect(await screen.findByText('github:9876:1')).toBeInTheDocument()
  })

  it('FE-INT-001 shows a not-found page for unknown routes', async () => {
    const { user, router } = renderRoute('/nowhere')
    expect(screen.getByText('Page not found')).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Go to test cases' }))
    expect(router.state.location.pathname).toBe('/test-cases')
  })

  it('FE-INT-013 boots the app with its providers and browser router', async () => {
    render(<App />)
    expect(await screen.findByText('Logout works')).toBeInTheDocument()
    expect(screen.getByText('Provenly')).toBeInTheDocument()
  })
})
