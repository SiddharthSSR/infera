import { useEffect, useId, useRef, useState, type CSSProperties, type ReactNode } from 'react';

type CodeLanguage = 'shell' | 'python' | 'typescript' | 'text';

interface CodeExampleProps {
  code: string;
  language?: CodeLanguage;
  className?: string;
  style?: CSSProperties;
}

const keywordSets: Record<Exclude<CodeLanguage, 'text'>, Set<string>> = {
  shell: new Set(['curl', 'export']),
  python: new Set(['from', 'import', 'print']),
  typescript: new Set(['import', 'from', 'const', 'await', 'for', 'of']),
};

const tokenPattern = /(https?:\/\/\S+|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`|#.*$|\/\/.*$|--?[A-Za-z0-9_-]+|\b\d+(?:\.\d+)?\b|\b[A-Za-z_][A-Za-z0-9_]*\b)/g;

function classifyToken(token: string, language: CodeLanguage): string | null {
  if (language === 'text') return null;
  if (token.startsWith('#') || token.startsWith('//')) return 'comment';
  if (token.startsWith('"') || token.startsWith("'") || token.startsWith('`')) return 'string';
  if (token.startsWith('http://') || token.startsWith('https://')) return 'url';
  if (token.startsWith('-')) return 'flag';
  if (/^\d/.test(token)) return 'number';
  if (/^[A-Z0-9_]+$/.test(token) && token.length > 1) return 'variable';
  if (keywordSets[language].has(token)) return 'keyword';
  if (language === 'shell' && token === 'Bearer') return 'keyword';
  if (language !== 'shell' && /^[A-Z][A-Za-z0-9_]+$/.test(token)) return 'type';
  return null;
}

function renderLine(line: string, language: CodeLanguage): ReactNode[] {
  const segments: ReactNode[] = [];
  let lastIndex = 0;

  for (const match of line.matchAll(tokenPattern)) {
    const [token] = match;
    const offset = match.index ?? 0;
    if (offset > lastIndex) {
      segments.push(line.slice(lastIndex, offset));
    }

    const tokenClass = classifyToken(token, language);
    if (tokenClass) {
      segments.push(
        <span key={`${offset}-${token}`} className={`code-token-${tokenClass}`}>
          {token}
        </span>,
      );
    } else {
      segments.push(token);
    }

    lastIndex = offset + token.length;
  }

  if (lastIndex < line.length) {
    segments.push(line.slice(lastIndex));
  }

  return segments;
}

export function CodeExample({ code, language = 'text', className = '', style }: CodeExampleProps) {
  const [copyState, setCopyState] = useState<'idle' | 'success' | 'error'>('idle');
  const copyStatusId = useId();
  const resetTimer = useRef<number>();
  const classes = ['code-block', 'docs-code-block', className].filter(Boolean).join(' ');
  const lines = code.split('\n');

  useEffect(() => () => window.clearTimeout(resetTimer.current), []);

  const handleCopy = async () => {
    window.clearTimeout(resetTimer.current);

    try {
      await navigator.clipboard.writeText(code);
      setCopyState('success');
    } catch {
      setCopyState('error');
    }

    resetTimer.current = window.setTimeout(() => setCopyState('idle'), 3000);
  };

  return (
    <div className="code-block-wrapper">
      <pre className={classes} style={style}>
        {lines.map((line, index) => (
          <span key={`${language}-${index}-${line}`} className="code-line">
            {renderLine(line, language)}
            {index < lines.length - 1 ? '\n' : ''}
          </span>
        ))}
      </pre>
      <button
        type="button"
        className={`code-copy-btn${copyState === 'success' ? ' copied' : ''}${copyState === 'error' ? ' copy-failed' : ''}`}
        data-copy-state={copyState}
        aria-describedby={copyStatusId}
        onClick={() => void handleCopy()}
      >
        {copyState === 'success' ? 'COPIED' : copyState === 'error' ? 'TRY AGAIN' : 'COPY'}
      </button>
      <span id={copyStatusId} className="sr-only" role="status" aria-live="polite">
        {copyState === 'success'
          ? 'Copied to clipboard.'
          : copyState === 'error'
            ? 'Copy failed. Select the code to copy it manually.'
            : ''}
      </span>
    </div>
  );
}
