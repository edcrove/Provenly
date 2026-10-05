import { render, screen, waitFor, within } from '@testing-library/react'
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
    expect(await screen.findByText('Page not found')).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Go to test cases' }))
    expect(router.state.location.pathname).toBe('/test-cases')
  })

  it.each([
    ['/test-cases', 'Test Cases', 'Test Cases', []],
    ['/test-cases/new', 'New test case', 'New test case', []],
    [
      '/test-cases/153',
      'TC-153 · Login works',
      'TC-153 · Login works',
      ['Current definition', 'Execution history'],
    ],
    ['/test-runs', 'Test Runs', 'Test Runs', []],
    [
      '/test-runs/7',
      'Test run #7',
      'Test run #7',
      ['Run metadata', 'Summary', 'TC-ID diagnostics', 'Results'],
    ],
    ['/nowhere', 'Page not found', 'Page not found', []],
  ])('FE-INT-017 %s has one h1, section h2s and its own tab title', async (path, h1, title, sections) => {
    renderRoute(path)
    const heading = await screen.findByRole('heading', { level: 1 })
    expect(heading).toHaveTextContent(h1)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    for (const name of sections)
      expect(await screen.findByRole('heading', { level: 2, name })).toBeInTheDocument()
    await waitFor(() => expect(document.title).toBe(`${title} · Provenly`))
  })

  it('FE-INT-013 boots the app with its providers and browser router', async () => {
    render(<App />)
    expect(await screen.findByText('Logout works')).toBeInTheDocument()
    expect(screen.getByText('Provenly')).toBeInTheDocument()
  })
})
