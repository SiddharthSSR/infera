import { useEffect, useState } from 'react';
import { Link, NavLink, useLocation } from 'react-router-dom';
import {
  designPartnerRequestEndpoint,
  getPublicAcquisitionTarget,
} from '../../lib/designPartnerRequest';
import { cn } from '../../lib/utils';
import { publicAnalytics } from '../../lib/publicAnalytics';
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
  /** Optional build-time intake endpoint override, primarily for deterministic rendering and tests. */
  intakeEndpoint?: string;
  className?: string;
  style?: React.CSSProperties;
}

function getDefaultPublicLinks(intakeEndpoint?: string): PublicNavLink[] {
  const acquisition = getPublicAcquisitionTarget(intakeEndpoint);

  return [
    { href: '/#product', label: 'PRODUCT' },
    { href: '/#migration', label: 'OPENAI MIGRATION' },
    { path: '/evaluation', label: 'EVALUATE' },
    { path: '/docs', label: 'DOCS' },
    { path: '/trust', label: 'TRUST' },
    { href: 'https://github.com/SiddharthSSR/infera', label: 'GITHUB', external: true },
    { path: '/sign-in', label: 'SIGN IN' },
    acquisition.path === '/request-access'
      ? { path: acquisition.path, label: 'REQUEST ACCESS' }
      : { path: '/getting-started', label: 'RUN QUICKSTART' },
  ];
}

function trackPublicNavigation(link: PublicNavLink) {
  if (link.path === '/docs') {
    publicAnalytics.track('public_resource_opened', { resource: 'api_docs', source: 'public_navigation' });
  } else if (link.path === '/sign-in') {
    publicAnalytics.track('public_sign_in_intent', { source: 'public_navigation' });
  } else if (link.path === '/request-access') {
    publicAnalytics.track('public_primary_cta_clicked', { action: 'request_design_partner_access', placement: 'public_navigation' });
  } else if (link.path === '/getting-started') {
    publicAnalytics.track('public_resource_opened', { resource: 'quickstart', source: 'public_navigation' });
  } else if (link.path === '/evaluation') {
    publicAnalytics.track('public_resource_opened', { resource: 'evaluation', source: 'public_navigation' });
  } else if (link.href === '/#product') {
    publicAnalytics.track('public_product_explored', { product: 'model_catalog', source: 'public_navigation' });
  } else if (link.href === '/#migration') {
    publicAnalytics.track('public_product_explored', { product: 'openai_compatibility', source: 'public_navigation' });
  }
}

/**
 * Navigation bar for public (unauthenticated) pages.
 * Uses NavLink for route-based active state highlighting.
 */
export function PublicNav({
  title,
  links,
  intakeEndpoint = designPartnerRequestEndpoint,
  className,
  style,
}: PublicNavProps) {
  const location = useLocation();
  const [menuOpen, setMenuOpen] = useState(false);
  const publicLinks = links ?? getDefaultPublicLinks(intakeEndpoint);

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
        {publicLinks.map((link) => link.path ? (
          <NavLink
            key={link.path}
            to={link.path}
            end={link.path === '/'}
            className={({ isActive }) => cn('nav-link', isActive && 'active')}
            onClick={() => trackPublicNavigation(link)}
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
            onClick={() => trackPublicNavigation(link)}
          >
            {link.label}
            {link.external ? <span className="sr-only"> (opens in a new tab)</span> : null}
          </a>
        ))}
      </nav>
    </header>
  );
}
