import { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';

type PaletteItem = {
  path: string;
  label: string;
  section: string;
};

const ITEMS: PaletteItem[] = [
  { path: '/', label: 'Dashboard', section: 'Primary' },
  { path: '/models', label: 'Models', section: 'Primary' },
  { path: '/instances', label: 'Nodes', section: 'Primary' },
  { path: '/playground', label: 'Playground', section: 'Primary' },
  { path: '/logs', label: 'Logs', section: 'Workspace' },
  { path: '/api-keys', label: 'API Keys', section: 'Workspace' },
  { path: '/workspace', label: 'Settings', section: 'Workspace' },
  { path: '/docs', label: 'API Docs', section: 'Docs' },
  { path: '/getting-started', label: 'Getting Started', section: 'Docs' },
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const location = useLocation();

  // Close on route change
  useEffect(() => {
    setOpen(false);
  }, [location.pathname]);

  // Cmd+K / Ctrl+K listener
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setOpen(prev => {
          if (!prev) {
            setQuery('');
            setSelectedIndex(0);
          }
          return !prev;
        });
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []);

  // Focus input when opened
  useEffect(() => {
    if (open) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  const filtered = query.trim()
    ? ITEMS.filter(item =>
        item.label.toLowerCase().includes(query.toLowerCase()) ||
        item.path.toLowerCase().includes(query.toLowerCase()),
      )
    : ITEMS;

  // Reset selection when filtered list changes
  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  const handleSelect = useCallback((item: PaletteItem) => {
    navigate(item.path);
    setOpen(false);
  }, [navigate]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setOpen(false);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelectedIndex(i => Math.min(i + 1, filtered.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelectedIndex(i => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && filtered[selectedIndex]) {
      handleSelect(filtered[selectedIndex]);
    }
  };

  if (!open) return null;

  // Group by section for rendering
  let currentSection = '';

  return (
    <>
      <div
        className="cmd-palette-backdrop"
        onClick={() => setOpen(false)}
        aria-hidden="true"
      />
      <div
        className="cmd-palette"
        role="dialog"
        aria-label="Command palette"
        onKeyDown={handleKeyDown}
      >
        <div className="cmd-palette-input-row">
          <span className="cmd-palette-shortcut">⌘K</span>
          <input
            ref={inputRef}
            type="text"
            className="cmd-palette-input"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Navigate to..."
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        <div className="cmd-palette-list" role="listbox">
          {filtered.length === 0 ? (
            <div className="cmd-palette-empty">No matching pages</div>
          ) : (
            filtered.map((item, i) => {
              const showSection = item.section !== currentSection;
              currentSection = item.section;
              const isActive = item.path === location.pathname;
              return (
                <div key={item.path}>
                  {showSection && (
                    <div className="cmd-palette-section">{item.section}</div>
                  )}
                  <button
                    type="button"
                    role="option"
                    aria-selected={i === selectedIndex}
                    className={`cmd-palette-item ${i === selectedIndex ? 'selected' : ''} ${isActive ? 'current' : ''}`}
                    onClick={() => handleSelect(item)}
                    onMouseEnter={() => setSelectedIndex(i)}
                  >
                    <span>{item.label}</span>
                    <span className="cmd-palette-item-path mono">{item.path}</span>
                  </button>
                </div>
              );
            })
          )}
        </div>
        <div className="cmd-palette-footer">
          <span><kbd>↑↓</kbd> navigate</span>
          <span><kbd>↵</kbd> open</span>
          <span><kbd>esc</kbd> close</span>
        </div>
      </div>
    </>
  );
}
