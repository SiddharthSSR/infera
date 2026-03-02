import { useState } from 'react';
import { Key, Globe, Database, Shield, ExternalLink, Copy, Check, RefreshCw } from 'lucide-react';
import { useTheme } from '../App';

export function SettingsPage() {
  const { theme, toggleTheme } = useTheme();
  const [apiKey, setApiKey] = useState('sk-infera-xxxxxxxxxxxx');
  const [copied, setCopied] = useState(false);
  const [gatewayUrl] = useState(window.location.origin);

  const handleCopyKey = () => {
    navigator.clipboard.writeText(apiKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleRegenerateKey = () => {
    setApiKey('sk-infera-' + Math.random().toString(36).slice(2, 14));
  };

  return (
    <div className="max-w-3xl space-y-6 animate-fade-in">
      {/* API Configuration */}
      <div className="bg-surface-900/80 backdrop-blur-sm border border-surface-800 rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-infera-500/10 flex items-center justify-center">
            <Key className="w-5 h-5 text-infera-400" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-white">API Configuration</h2>
            <p className="text-sm text-surface-400">Manage your API access</p>
          </div>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-surface-300 mb-2">API Key</label>
            <div className="flex gap-2">
              <div className="flex-1 relative">
                <input type="password" value={apiKey} readOnly className="w-full bg-surface-900 border border-surface-700 rounded-xl px-4 py-2.5 pr-20 font-mono text-surface-100" />
                <button onClick={handleCopyKey} className="absolute right-2 top-1/2 -translate-y-1/2 p-2 rounded-lg text-surface-400 hover:text-surface-100 hover:bg-surface-800 transition-colors">
                  {copied ? <Check className="w-4 h-4 text-accent-green" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
              <button onClick={handleRegenerateKey} className="inline-flex items-center gap-2 px-4 py-2.5 rounded-xl bg-surface-800 text-surface-100 border border-surface-700 hover:bg-surface-700 transition-colors">
                <RefreshCw className="w-4 h-4" />Regenerate
              </button>
            </div>
            <p className="text-xs text-surface-500 mt-2">Use this key to authenticate API requests</p>
          </div>

          <div>
            <label className="block text-sm font-medium text-surface-300 mb-2">Gateway URL</label>
            <input type="text" value={gatewayUrl} readOnly className="w-full bg-surface-900 border border-surface-700 rounded-xl px-4 py-2.5 font-mono text-surface-100" />
            <p className="text-xs text-surface-500 mt-2">OpenAI-compatible endpoint: {gatewayUrl}/v1/chat/completions</p>
          </div>
        </div>
      </div>

      {/* Appearance */}
      <div className="bg-surface-900/80 backdrop-blur-sm border border-surface-800 rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-accent-purple/10 flex items-center justify-center">
            <Globe className="w-5 h-5 text-accent-purple" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-white">Appearance</h2>
            <p className="text-sm text-surface-400">Customize the interface</p>
          </div>
        </div>

        <div className="flex items-center justify-between p-4 bg-surface-900 rounded-xl border border-surface-800">
          <div>
            <div className="font-medium text-white">Theme</div>
            <p className="text-sm text-surface-400">Switch between dark and light mode</p>
          </div>
          <button onClick={toggleTheme} className={`px-4 py-2 rounded-xl font-medium transition-colors ${theme === 'dark' ? 'bg-surface-800 text-white' : 'bg-white text-surface-900 border border-surface-200'}`}>
            {theme === 'dark' ? '🌙 Dark' : '☀️ Light'}
          </button>
        </div>
      </div>

      {/* Usage Example */}
      <div className="bg-surface-900/80 backdrop-blur-sm border border-surface-800 rounded-2xl p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center">
            <Database className="w-5 h-5 text-accent-green" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-white">Quick Start</h2>
            <p className="text-sm text-surface-400">Example API usage</p>
          </div>
        </div>

        <div className="bg-surface-950 border border-surface-800 rounded-xl p-4 font-mono text-sm text-surface-200 overflow-x-auto">
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
      <div className="bg-surface-900/80 backdrop-blur-sm border border-surface-800 rounded-2xl p-6">
        <h2 className="text-lg font-semibold text-white mb-4">Resources</h2>
        <div className="grid sm:grid-cols-2 gap-3">
          <a href="/api/health" target="_blank" className="flex items-center gap-3 p-3 bg-surface-900 rounded-xl hover:bg-surface-800 transition-colors">
            <Shield className="w-5 h-5 text-surface-400" />
            <span className="text-surface-200">Health Check</span>
            <ExternalLink className="w-4 h-4 text-surface-500 ml-auto" />
          </a>
          <a href="https://github.com/infera/infera" target="_blank" className="flex items-center gap-3 p-3 bg-surface-900 rounded-xl hover:bg-surface-800 transition-colors">
            <Globe className="w-5 h-5 text-surface-400" />
            <span className="text-surface-200">Documentation</span>
            <ExternalLink className="w-4 h-4 text-surface-500 ml-auto" />
          </a>
        </div>
      </div>
    </div>
  );
}