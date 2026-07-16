import { NavLink } from 'react-router-dom';
import { cn } from '../../lib/utils';
import { LabelText } from './LabelText';

export interface PublicNavLink {
  path: string;
  label: string;
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
  { path: '/docs', label: 'API DOCS' },
  { path: '/getting-started', label: 'GETTING STARTED' },
  { path: '/accept-invite', label: 'ACCEPT INVITE' },
  { path: '/', label: 'LOGIN' },
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
  return (
    <header className={cn('top-nav docs-header', className)} style={style}>
      <div>
        <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
        <LabelText as="div" style={{ marginTop: '0.5rem' }}>{title}</LabelText>
      </div>
      <div className="nav-group" style={{ gap: '1rem' }}>
        {links.map((link) => (
          <NavLink
            key={link.path}
            to={link.path}
            end={link.path === '/'}
            className={({ isActive }) => cn('nav-link', isActive && 'active')}
          >
            {link.label}
          </NavLink>
        ))}
      </div>
    </header>
  );
}
