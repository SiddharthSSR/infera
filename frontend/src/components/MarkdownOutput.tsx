import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';

export function MarkdownOutput({ content }: { content: string }) {
  return (
    <div className="markdown-output">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          pre({ children, ...props }) {
            return (
              <pre
                {...props}
                style={{
                  background: '#F4F2EE',
                  border: '1px solid var(--border-color)',
                  padding: '1.25rem',
                  overflow: 'auto',
                  fontSize: '0.85rem',
                  lineHeight: 1.6,
                  marginBottom: '1rem',
                }}
              >
                {children}
              </pre>
            );
          },
          code({ className, children, ...props }) {
            const isInline = !className;
            if (isInline) {
              return (
                <code
                  {...props}
                  style={{
                    background: '#F4F2EE',
                    padding: '0.15rem 0.4rem',
                    fontSize: '0.88em',
                    fontFamily: 'var(--font-mono)',
                    border: '1px solid var(--border-color)',
                  }}
                >
                  {children}
                </code>
              );
            }
            return (
              <code className={className} {...props} style={{ fontFamily: 'var(--font-mono)' }}>
                {children}
              </code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
