/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Header } from './Header'
import { StatsCards } from './StatsCards'
import { CostDisplay } from './CostDisplay'
import type { Stats, CostSummary } from '../types'

describe('Header', () => {
  it('renders the logo and title', () => {
    render(<Header />)
    
    expect(screen.getByText('Infera')).toBeInTheDocument()
    expect(screen.getByText('AI Inference Platform')).toBeInTheDocument()
  })

  it('renders navigation links', () => {
    render(<Header />)

    expect(screen.getByText('GitHub')).toBeInTheDocument()
    expect(screen.getByText('API')).toBeInTheDocument()
  })
})

describe('StatsCards', () => {
  const mockStats: Stats = {
    workers: { total: 5, healthy: 4 },
    models: { available: 3 },
    requests: { per_second: 100.5, queue_depth: 10 },
    latency: { avg_ms: 150.75 },
    memory: { used_bytes: 1000000000, total_bytes: 2000000000 },
    uptime_seconds: 3600,
  }

  it('renders loading state', () => {
    render(<StatsCards stats={undefined} isLoading={true} />)
    
    // Should show loading skeletons
    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('renders stats when loaded', () => {
    render(<StatsCards stats={mockStats} isLoading={false} />)
    
    expect(screen.getByText('Workers')).toBeInTheDocument()
    expect(screen.getByText('4/5')).toBeInTheDocument()
    expect(screen.getByText('Models')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('Requests')).toBeInTheDocument()
    expect(screen.getByText('100.5/s')).toBeInTheDocument()
    expect(screen.getByText('Latency')).toBeInTheDocument()
    expect(screen.getByText('151ms')).toBeInTheDocument()
  })

  it('shows queue depth', () => {
    render(<StatsCards stats={mockStats} isLoading={false} />)
    
    expect(screen.getByText('10 queued')).toBeInTheDocument()
  })
})

describe('CostDisplay', () => {
  const mockCosts: CostSummary = {
    current_hourly: 5.50,
    today_total: 45.00,
    month_total: 350.00,
    projected_month: 500.00,
    by_provider: { runpod: 3.50, vastai: 2.00 },
    by_gpu: { RTX_4090: 1.50, A100_80GB: 4.00 },
  }

  it('renders loading state', () => {
    render(<CostDisplay costs={undefined} isLoading={true} />)
    
    const skeleton = document.querySelector('.animate-pulse')
    expect(skeleton).toBeInTheDocument()
  })

  it('renders cost summary when loaded', () => {
    render(<CostDisplay costs={mockCosts} isLoading={false} />)
    
    expect(screen.getByText('Cost')).toBeInTheDocument()
    expect(screen.getByText('$5.50')).toBeInTheDocument()
    expect(screen.getByText('/hr')).toBeInTheDocument()
  })

  it('shows today and month totals', () => {
    render(<CostDisplay costs={mockCosts} isLoading={false} />)
    
    expect(screen.getByText('Today: $45.00')).toBeInTheDocument()
    expect(screen.getByText('Month: $350.00')).toBeInTheDocument()
  })

  it('shows projected monthly cost', () => {
    render(<CostDisplay costs={mockCosts} isLoading={false} />)
    
    expect(screen.getByText('Projected: $500.00/mo')).toBeInTheDocument()
  })

  it('handles zero costs', () => {
    const zeroCosts: CostSummary = {
      current_hourly: 0,
      today_total: 0,
      month_total: 0,
      projected_month: 0,
      by_provider: {},
      by_gpu: {},
    }
    
    render(<CostDisplay costs={zeroCosts} isLoading={false} />)
    
    expect(screen.getByText('$0.00')).toBeInTheDocument()
  })
})
