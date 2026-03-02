import { useState } from 'react';
import { Key, Globe, Database, Shield, ExternalLink, Copy, Check, RefreshCw } from 'lucide-react';
import { cn } from '../lib/utils';
import { useTheme } from '../App';
import { toast } from 'sonner';

export function SettingsPage() {
  const { theme, toggleTheme } = useTheme();
  const [apiKey, setApiKey] = useState('sk-infera-xxxxxxxxxxxx');
  const [copied, setCopied] = useState(false);
  const [gatewayUrl] = useState(window.location.origin);

  const handleCopyKey = () => {
    navigator.clipboard.writeText(apiKey);
    setCopied(true);
    toast.success('Copied to clipboard');
    setTimeout(() => setCopied(false), 2000);
  };

  const handleRegenerateKey = () => {
    setApiKey('sk-infera-' + Math.random().toString(36).slice(2, 14));
    toast.success('API key regenerated');
  };

  return (
    <div className="max-w-3xl space-y-6 animate-fade-in">
      {/* API Configuration */}
      <div className="bg-card backdrop-blur-sm border border-border rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center">
            <Key className="w-5 h-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground">API Configuration</h2>
            <p className="text-sm text-muted-foreground">Manage your API access</p>
          </div>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-foreground mb-2">API Key</label>
            <div className="flex gap-2">
              <div className="flex-1 relative">
                <input type="password" value={apiKey} readOnly className="w-full bg-input border border-border rounded-xl px-4 py-2.5 pr-20 font-mono text-foreground" />
                <button onClick={handleCopyKey} className="absolute right-2 top-1/2 -translate-y-1/2 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted transition-colors">
                  {copied ? <Check className="w-4 h-4 text-success" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
              <button onClick={handleRegenerateKey} className="inline-flex items-center gap-2 px-4 py-2.5 rounded-xl bg-secondary text-secondary-foreground border border-border hover:bg-accent transition-colors">
                <RefreshCw className="w-4 h-4" />Regenerate
              </button>
            </div>
            <p className="text-xs text-muted-foreground mt-2">Use this key to authenticate API requests</p>
          </div>

          <div>
            <label className="block text-sm font-medium text-foreground mb-2">Gateway URL</label>
            <input type="text" value={gatewayUrl} readOnly className="w-full bg-input border border-border rounded-xl px-4 py-2.5 font-mono text-foreground" />
            <p className="text-xs text-muted-foreground mt-2">OpenAI-compatible endpoint: {gatewayUrl}/v1/chat/completions</p>
          </div>
        </div>
      </div>

      {/* Appearance */}
      <div className="bg-card backdrop-blur-sm border border-border rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center">
            <Globe className="w-5 h-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground">Appearance</h2>
            <p className="text-sm text-muted-foreground">Customize the interface</p>
          </div>
        </div>

        <div className="flex items-center justify-between p-4 bg-muted/50 rounded-xl border border-border">
          <div>
            <div className="font-medium text-foreground">Theme</div>
            <p className="text-sm text-muted-foreground">Switch between dark and light mode</p>
          </div>
          <button onClick={toggleTheme} className={cn(
            "px-4 py-2 rounded-xl font-medium transition-colors",
            theme === 'dark'
              ? "bg-secondary text-secondary-foreground"
              : "bg-card text-foreground border border-border"
          )}>
            {theme === 'dark' ? 'Dark' : 'Light'}
          </button>
        </div>
      </div>

      {/* Usage Example */}
      <div className="bg-card backdrop-blur-sm border border-border rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-success/10 flex items-center justify-center">
            <Database className="w-5 h-5 text-success" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground">Quick Start</h2>
            <p className="text-sm text-muted-foreground">Example API usage</p>
          </div>
        </div>

        <div className="bg-background border border-border rounded-xl p-4 font-mono text-sm text-foreground overflow-x-auto">
          <pre>{`curl ${gatewayUrl}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${apiKey}" \\
  -d '{
    "model": "mistralai/Mistral-7B-Instruct-v0.2",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'`}</pre>
        </div>
      </div>

      {/* Links */}
      <div className="bg-card backdrop-blur-sm border border-border rounded-2xl p-6">
        <h2 className="text-lg font-semibold text-foreground mb-4">Resources</h2>
        <div className="grid sm:grid-cols-2 gap-3">
          <a href="/api/health" target="_blank" className="flex items-center gap-3 p-3 bg-muted/50 rounded-xl hover:bg-muted transition-colors">
            <Shield className="w-5 h-5 text-muted-foreground" />
            <span className="text-foreground">Health Check</span>
            <ExternalLink className="w-4 h-4 text-muted-foreground ml-auto" />
          </a>
          <a href="https://github.com/infera/infera" target="_blank" className="flex items-center gap-3 p-3 bg-muted/50 rounded-xl hover:bg-muted transition-colors">
            <Globe className="w-5 h-5 text-muted-foreground" />
            <span className="text-foreground">Documentation</span>
            <ExternalLink className="w-4 h-4 text-muted-foreground ml-auto" />
          </a>
        </div>
      </div>
    </div>
  );
}
