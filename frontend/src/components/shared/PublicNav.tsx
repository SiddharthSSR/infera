import { useEffect, useState } from 'react';
import { Link, NavLink, useLocation } from 'react-router-dom';
import { cn } from '../../lib/utils';
import { LabelText } from './LabelText';

export interface PublicNavLink {
  path?: string;
  href?: string;
  label: string;
  external?: boolean;
}

export interface PublicNavProps {
  /** Subtitle shown beneath the INFERA.AI brand mark */
  title: string;
  /** Navigation links — defaults to the standard public page set */
  links?: PublicNavLink[];
  className?: string;
  style?: React.CSSProperties;
}

const defaultPublicLinks: PublicNavLink[] = [
  { href: '/#product', label: 'PRODUCT' },
  { href: '/#migration', label: 'OPENAI MIGRATION' },
  { path: '/docs', label: 'DOCS' },
  { href: 'https://github.com/SiddharthSSR/infera', label: 'GITHUB', external: true },
  { path: '/sign-in', label: 'SIGN IN' },
];

/**
 * Navigation bar for public (unauthenticated) pages.
 * Uses NavLink for route-based active state highlighting.
 */
export function PublicNav({
  title,
  links = defaultPublicLinks,
  className,
  style,
}: PublicNavProps) {
  const location = useLocation();
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    setMenuOpen(false);
  }, [location.pathname, location.hash]);

  return (
    <header className={cn('top-nav docs-header', className)} style={style}>
      <Link className="public-brand" to="/" aria-label="Infera home">
        <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
        <LabelText as="div" style={{ marginTop: '0.5rem' }}>{title}</LabelText>
      </Link>
      <button
        className="public-menu-button"
        type="button"
        aria-expanded={menuOpen}
        aria-controls="public-navigation"
        onClick={() => setMenuOpen((open) => !open)}
      >
        {menuOpen ? 'CLOSE' : 'MENU'}
      </button>
      <nav
        id="public-navigation"
        className={cn('nav-group public-nav-links', menuOpen && 'is-open')}
        aria-label="Primary navigation"
      >
        {links.map((link) => link.path ? (
          <NavLink
            key={link.path}
            to={link.path}
            end={link.path === '/'}
            className={({ isActive }) => cn('nav-link', isActive && 'active')}
          >
            {link.label}
          </NavLink>
        ) : (
          <a
            key={link.href}
            href={link.href}
            className="nav-link"
            target={link.external ? '_blank' : undefined}
            rel={link.external ? 'noreferrer' : undefined}
          >
            {link.label}
            {link.external ? <span className="sr-only"> (opens in a new tab)</span> : null}
          </a>
        ))}
      </nav>
    </header>
  );
}
