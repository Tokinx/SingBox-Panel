import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Table } from './table'

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('Table', () => {
  it('sets scope, aria-sort, nowrap cell classes, and summary cell styles', () => {
    render(
      <Table
        ariaLabel="测试列表"
        rowKey="id"
        dataSource={[
          { id: 1, tag: 'fwd-1', port: 30511 },
          { id: 2, tag: 'fwd-2', port: 10443 },
        ]}
        columns={[
          { title: '标签', dataIndex: 'tag', nowrap: true },
          { title: '端口', dataIndex: 'port', nowrap: true, sorter: (a, b) => a.port - b.port },
        ]}
        summary={() => (
          <Table.Summary>
            <Table.Summary.Row>
              <Table.Summary.Cell index={0}>合计</Table.Summary.Cell>
              <Table.Summary.Cell index={1} align="right" className="custom-summary">2</Table.Summary.Cell>
            </Table.Summary.Row>
          </Table.Summary>
        )}
      />,
    )

    const region = screen.getByRole('region', { name: '测试列表' })
    expect(region.getAttribute('tabindex')).toBeNull()

    const tagHeader = screen.getByRole('columnheader', { name: '标签' })
    const portHeader = screen.getByRole('columnheader', { name: '端口' })
    expect(tagHeader.getAttribute('scope')).toBe('col')
    expect(portHeader.getAttribute('scope')).toBe('col')
    expect(portHeader.getAttribute('aria-sort')).toBeNull()

    const sortButton = screen.getByRole('button', { name: '端口' })
    expect(sortButton.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true')

    fireEvent.click(sortButton)
    expect(portHeader.getAttribute('aria-sort')).toBe('ascending')
    expect(tagHeader.getAttribute('aria-sort')).toBeNull()
    const cellsAfterAsc = screen.getAllByRole('cell')
    expect(cellsAfterAsc[0].textContent).toBe('fwd-2')
    expect(cellsAfterAsc[0].classList.contains('ui-table-cell-nowrap')).toBe(true)

    fireEvent.click(sortButton)
    expect(portHeader.getAttribute('aria-sort')).toBe('descending')
    const cellsAfterDesc = screen.getAllByRole('cell')
    expect(cellsAfterDesc[0].textContent).toBe('fwd-1')

    fireEvent.click(sortButton)
    expect(portHeader.getAttribute('aria-sort')).toBeNull()

    const summaryCell = screen.getByText('2')
    expect(summaryCell.classList.contains('ui-table-cell')).toBe(true)
    expect(summaryCell.classList.contains('custom-summary')).toBe(true)
    expect((summaryCell as HTMLElement).style.textAlign).toBe('right')
  })

  it('adds tabindex=0 to overflowing scrollable containers for keyboard scrolling', () => {
    const originalClientWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    const originalScrollWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollWidth')
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 400 })
    Object.defineProperty(HTMLElement.prototype, 'scrollWidth', { configurable: true, get: () => 900 })
    try {
      render(
        <Table
          ariaLabel="宽表格"
          rowKey="id"
          dataSource={[{ id: 1, tag: 'fwd-1' }]}
          columns={[{ title: '标签', dataIndex: 'tag' }]}
        />,
      )
      const region = screen.getByRole('region', { name: '宽表格' })
      expect(region.getAttribute('tabindex')).toBe('0')
    } finally {
      if (originalClientWidth) Object.defineProperty(HTMLElement.prototype, 'clientWidth', originalClientWidth)
      else Reflect.deleteProperty(HTMLElement.prototype, 'clientWidth')
      if (originalScrollWidth) Object.defineProperty(HTMLElement.prototype, 'scrollWidth', originalScrollWidth)
      else Reflect.deleteProperty(HTMLElement.prototype, 'scrollWidth')
    }
  })
})
