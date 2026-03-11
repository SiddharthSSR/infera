import { Component, type ErrorInfo, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';

interface Props {
  children: ReactNode;
}

interface ErrorBoundaryProps extends Props {
  navigate: (to: string) => void;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

class ErrorBoundaryInner extends Component<ErrorBoundaryProps, State> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('Unhandled error in ErrorBoundary', error, info);
  }

  handleReset = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      return (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '60vh',
          padding: '2rem',
          textAlign: 'center',
          fontFamily: 'var(--font-main)',
        }}>
          <div style={{
            fontSize: '3rem',
            fontWeight: 700,
            color: 'var(--text-primary)',
            marginBottom: '1rem',
            letterSpacing: '-0.03em',
          }}>
            Something went wrong
          </div>
          <div style={{
            fontSize: '0.95rem',
            color: 'var(--text-secondary)',
            maxWidth: 480,
            marginBottom: '2rem',
            lineHeight: 1.6,
          }}>
            An unexpected error occurred. This has been logged.
            You can try refreshing the page or going back to the dashboard.
          </div>
          <div style={{
            fontFamily: 'var(--font-mono)',
            fontSize: '0.8rem',
            color: 'var(--text-secondary)',
            background: 'var(--bg-accent)',
            padding: '1rem 1.5rem',
            borderRadius: 4,
            marginBottom: '2rem',
            maxWidth: 600,
            overflow: 'auto',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}>
            {this.state.error?.message || 'Unknown error'}
          </div>
          <div style={{ display: 'flex', gap: '1rem' }}>
            <button className="action-btn" onClick={this.handleReset}>
              TRY AGAIN
            </button>
            <button className="action-btn" onClick={() => this.props.navigate('/')}>
              GO TO DASHBOARD
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}

export function ErrorBoundary({ children }: Props) {
  const navigate = useNavigate();
  return <ErrorBoundaryInner navigate={navigate}>{children}</ErrorBoundaryInner>;
}
