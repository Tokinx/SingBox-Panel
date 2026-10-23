import type { ReactNode } from 'react'

/** Page heading: large title, optional one-line meta row, actions on the right. */
export function PageHeader({ title, meta, actions }: { title: ReactNode; meta?: ReactNode; actions?: ReactNode }) {
  return <header className="ui-page-head">
    <div className="ui-page-title">
      <h1>{title}</h1>
      {meta && <div className="ui-page-meta">{meta}</div>}
    </div>
    {actions && <div className="ui-page-actions">{actions}</div>}
  </header>
}

/** Bordered strip of headline numbers. */
export function Stats({ items }: { items: { label: ReactNode; value: ReactNode; hint?: ReactNode }[] }) {
  return <div className="ui-stats" style={{ ['--ui-stats-cols' as string]: items.length }}>
    {items.map((item, index) => <div className="ui-stat" key={index}>
      <span>{item.label}</span>
      <b>{item.value}</b>
      {item.hint && <small>{item.hint}</small>}
    </div>)}
  </div>
}

export type PillItem = { key: string; label: ReactNode; count?: number }

/** Segmented pill tabs with an optional action slot on the right. */
export function Pills({ items, active, onChange, actions, label }: {
  items: PillItem[]
  active: string
  onChange: (key: string) => void
  actions?: ReactNode
  label?: string
}) {
  return <div className="ui-pills-row">
    <div className="ui-pills" role="tablist" aria-label={label}>
      {items.map((item) => <button key={item.key} type="button" role="tab" aria-selected={item.key === active}
        className={`ui-pill${item.key === active ? ' is-active' : ''}`} onClick={() => onChange(item.key)}>
        {item.label}{item.count !== undefined && <span className="ui-pill-count">{item.count}</span>}
      </button>)}
    </div>
    {actions && <div className="ui-pills-actions">{actions}</div>}
  </div>
}

/** Small coloured dot with a label. */
export function Status({ tone = 'off', children }: { tone?: 'ok' | 'off' | 'warn' | 'error'; children: ReactNode }) {
  return <span className={`ui-status is-${tone}`}><i />{children}</span>
}

/** Plain section with a small heading and optional actions; used inside pages. */
export function Section({ title, actions, children, className = '' }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return <section className={`ui-section ${className}`}>
    {(title || actions) && <header className="ui-section-head">{title && <h2>{title}</h2>}{actions && <div className="ui-section-actions">{actions}</div>}</header>}
    {children}
  </section>
}
