import { useState, useRef, useEffect, useCallback } from 'react';
import {
  Send, Loader2, User, Bot, Settings2, Maximize2, Minimize2,
  Trash2, Copy, Check, Sparkles, ChevronDown, X
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import { toast } from 'sonner';
import { cn } from '../lib/utils';
import { streamChatCompletion } from '../lib/api';
import { useModels } from '../hooks/useApi';
import { useChat } from '../App';
import type { ChatMessage, Model } from '../types';

interface Message extends ChatMessage {
  id: string;
  timestamp: Date;
}

function generateUUID(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function ModelSelector({ models, selectedModel, onSelect, disabled }: {
  models: Model[] | undefined;
  selectedModel: string;
  onSelect: (model: string) => void;
  disabled?: boolean;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const selectedModelData = models?.find(m => m.id === selectedModel);
  const modelName = selectedModel?.split('/').pop() || 'Select Model';
  const hasVaultData = models?.some(m => m.family !== undefined);

  if (!models) {
    return (
      <div className="flex items-center gap-2 px-3 py-2 bg-input border border-border rounded-lg">
        <div className="h-4 w-24 bg-muted rounded animate-pulse" />
      </div>
    );
  }

  // Group by family if vault metadata is available
  const groupedModels = hasVaultData
    ? models.reduce((acc, model) => {
        const family = model.family || 'Other';
        if (!acc[family]) acc[family] = [];
        acc[family].push(model);
        return acc;
      }, {} as Record<string, Model[]>)
    : { '': models };

  return (
    <div className="relative">
      <button
        onClick={() => !disabled && setIsOpen(!isOpen)}
        disabled={disabled}
        className={cn(
          "flex items-center gap-2 px-3 py-2 bg-input border border-border rounded-lg text-sm",
          "hover:border-primary/50 transition-colors disabled:opacity-50"
        )}
      >
        <Sparkles className="w-4 h-4 text-primary" />
        {selectedModelData?.loaded !== undefined && (
          <span className={cn("w-2 h-2 rounded-full", selectedModelData.loaded ? "bg-emerald-500" : "bg-gray-400")} />
        )}
        <span className="text-foreground font-medium">{modelName}</span>
        {selectedModelData?.family && selectedModelData?.parameters && (
          <span className="text-xs text-muted-foreground">{selectedModelData.family} · {selectedModelData.parameters}</span>
        )}
        <ChevronDown className={cn("w-4 h-4 text-muted-foreground transition-transform", isOpen && "rotate-180")} />
      </button>

      {isOpen && models && models.length > 0 && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute top-full left-0 mt-2 w-96 bg-popover border border-border rounded-lg shadow-xl z-50 overflow-hidden animate-scale-in">
            <div className="p-3 border-b border-border">
              <p className="text-xs text-muted-foreground">Select a model</p>
            </div>
            <div className="max-h-80 overflow-y-auto scrollbar-thin">
              {Object.entries(groupedModels).map(([family, familyModels]) => (
                <div key={family}>
                  {family && hasVaultData && (
                    <div className="px-3 py-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wide bg-muted/50 border-b border-border">
                      {family}
                    </div>
                  )}
                  {familyModels.map(model => {
                    const isSelected = model.id === selectedModel;
                    const isLoaded = model.loaded !== undefined ? model.loaded : true;
                    const isDisabled = model.loaded !== undefined && !model.loaded;
                    return (
                      <button
                        key={model.id}
                        onClick={() => { if (!isDisabled) { onSelect(model.id); setIsOpen(false); } }}
                        disabled={isDisabled}
                        className={cn(
                          "w-full p-3 text-left transition-colors",
                          isDisabled ? "opacity-50 cursor-not-allowed" : "hover:bg-accent",
                          isSelected && "bg-accent border-l-2 border-primary"
                        )}
                      >
                        <div className="flex items-center justify-between mb-0.5">
                          <div className="flex items-center gap-2">
                            <span className={cn("w-2 h-2 rounded-full flex-shrink-0", isLoaded ? "bg-emerald-500" : "bg-gray-400")} />
                            <span className="font-medium text-foreground text-sm">{model.id.split('/').pop()}</span>
                          </div>
                          <div className="flex items-center gap-2">
                            {isDisabled && <span className="text-xs text-muted-foreground">Not loaded</span>}
                            {isSelected && <Check className="w-4 h-4 text-primary" />}
                          </div>
                        </div>
                        <div className="ml-4 flex items-center gap-2">
                          <span className="text-xs text-muted-foreground truncate">{model.id}</span>
                          {model.family && model.parameters && (
                            <span className="flex-shrink-0 text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                              {model.family} · {model.parameters}
                            </span>
                          )}
                        </div>
                      </button>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function SettingsPanel({ temperature, maxTokens, onTemperatureChange, onMaxTokensChange, onClose }: {
  temperature: number;
  maxTokens: number;
  onTemperatureChange: (v: number) => void;
  onMaxTokensChange: (v: number) => void;
  onClose: () => void;
}) {
  return (
    <div className="absolute top-full right-0 mt-2 w-72 bg-popover border border-border rounded-lg shadow-xl z-50 animate-scale-in">
      <div className="flex items-center justify-between p-3 border-b border-border">
        <span className="font-medium text-foreground text-sm">Generation Settings</span>
        <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="p-4 space-y-4">
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-sm text-muted-foreground">Temperature</label>
            <span className="text-sm font-mono text-primary">{temperature.toFixed(1)}</span>
          </div>
          <input type="range" min="0" max="2" step="0.1" value={temperature} onChange={(e) => onTemperatureChange(parseFloat(e.target.value))} />
          <div className="flex justify-between text-xs text-muted-foreground mt-1">
            <span>Precise</span><span>Creative</span>
          </div>
        </div>
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-sm text-muted-foreground">Max Tokens</label>
            <span className="text-sm font-mono text-primary">{maxTokens}</span>
          </div>
          <input type="range" min="64" max="4096" step="64" value={maxTokens} onChange={(e) => onMaxTokensChange(parseInt(e.target.value))} />
        </div>
      </div>
    </div>
  );
}

function ChatMessageBubble({ message, onCopy }: { message: Message; onCopy: (text: string) => void }) {
  const [copied, setCopied] = useState(false);
  const isUser = message.role === 'user';

  const handleCopy = () => {
    onCopy(message.content);
    setCopied(true);
    toast.success('Copied to clipboard');
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className={cn("flex gap-3 group animate-fade-in", isUser && "flex-row-reverse")}>
      <div className={cn(
        "w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0",
        isUser ? "bg-primary" : "bg-muted border border-border"
      )}>
        {isUser ? <User className="w-4 h-4 text-primary-foreground" /> : <Bot className="w-4 h-4 text-primary" />}
      </div>

      <div className={cn("relative max-w-[80%]", isUser ? "items-end" : "items-start")}>
        <div className={cn(
          "rounded-2xl px-4 py-3",
          isUser ? "bg-primary text-primary-foreground rounded-br-md" : "bg-card border border-border text-card-foreground rounded-bl-md"
        )}>
          {isUser ? (
            <p className="text-sm whitespace-pre-wrap leading-relaxed">
              {message.content}
            </p>
          ) : message.content ? (
            <div className="prose-chat text-sm leading-relaxed">
              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
                {message.content}
              </ReactMarkdown>
            </div>
          ) : (
            <span className="typing-indicator"><span></span><span></span><span></span></span>
          )}
        </div>

        {message.content && (
          <div className={cn("flex items-center gap-1 mt-1 opacity-0 group-hover:opacity-100 transition-opacity", isUser ? "justify-end" : "justify-start")}>
            <button onClick={handleCopy} className="p-1 text-muted-foreground hover:text-foreground transition-colors">
              {copied ? <Check className="w-3 h-3 text-success" /> : <Copy className="w-3 h-3" />}
            </button>
            <span className="text-xs text-muted-foreground">
              {message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

export function Playground() {
  const { data: models } = useModels();

  const {
    messages, setMessages,
    selectedModel, setSelectedModel,
    temperature, setTemperature,
    maxTokens, setMaxTokens
  } = useChat();

  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (models && models.length > 0 && !selectedModel) setSelectedModel(models[0].id);
  }, [models, selectedModel, setSelectedModel]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  useEffect(() => { inputRef.current?.focus(); }, []);

  // Auto-resize textarea
  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 128) + 'px';
  }, []);

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!input.trim() || isLoading || !selectedModel) return;

    const userMessage: Message = { id: generateUUID(), role: 'user', content: input.trim(), timestamp: new Date() };
    setMessages(prev => [...prev, userMessage]);
    setInput('');
    setIsLoading(true);

    // Reset textarea height
    if (inputRef.current) inputRef.current.style.height = 'auto';

    const assistantMessage: Message = { id: generateUUID(), role: 'assistant', content: '', timestamp: new Date() };
    setMessages(prev => [...prev, assistantMessage]);

    try {
      const stream = streamChatCompletion({
        model: selectedModel,
        messages: [...messages, userMessage].map(({ role, content }) => ({ role, content })),
        temperature, max_tokens: maxTokens, stream: true,
      });

      for await (const chunk of stream) {
        setMessages(prev => {
          const updated = [...prev];
          const lastMessage = updated[updated.length - 1];
          if (lastMessage.role === 'assistant') lastMessage.content += chunk;
          return updated;
        });
      }
    } catch (error) {
      setMessages(prev => {
        const updated = [...prev];
        const lastMessage = updated[updated.length - 1];
        if (lastMessage.role === 'assistant') {
          lastMessage.content = `Error: ${error instanceof Error ? error.message : 'Request failed'}`;
        }
        return updated;
      });
    } finally {
      setIsLoading(false);
      inputRef.current?.focus();
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSubmit(); }
  };

  const containerClass = isFullscreen
    ? "fixed inset-0 z-50 bg-background flex flex-col"
    : "bg-card border border-border rounded-xl flex flex-col h-[calc(100vh-12rem)]";

  return (
    <div className={containerClass}>
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border">
        <div className="flex items-center gap-3">
          <ModelSelector models={models} selectedModel={selectedModel} onSelect={setSelectedModel} disabled={isLoading} />
        </div>

        <div className="flex items-center gap-2">
          {messages.length > 0 && (
            <button onClick={() => { setMessages([]); inputRef.current?.focus(); }} className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-lg transition-colors">
              <Trash2 className="w-4 h-4" /><span className="hidden sm:inline">Clear</span>
            </button>
          )}

          <div className="relative">
            <button onClick={() => setShowSettings(!showSettings)} className={cn("p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors", showSettings && "bg-accent")}>
              <Settings2 className="w-4 h-4" />
            </button>
            {showSettings && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowSettings(false)} />
                <SettingsPanel temperature={temperature} maxTokens={maxTokens} onTemperatureChange={setTemperature} onMaxTokensChange={setMaxTokens} onClose={() => setShowSettings(false)} />
              </>
            )}
          </div>

          <button onClick={() => setIsFullscreen(!isFullscreen)} className="p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
            {isFullscreen ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4 scrollbar-thin">
        {messages.length === 0 ? (
          <div className="h-full flex items-center justify-center">
            <div className="text-center max-w-md">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-primary/20 to-accent/20 flex items-center justify-center mx-auto mb-4">
                <Sparkles className="w-8 h-8 text-primary" />
              </div>
              <h3 className="text-lg font-semibold text-foreground mb-2">Start a conversation</h3>
              <p className="text-muted-foreground text-sm mb-4">
                {models && models.length > 0 ? `Chat with ${selectedModel.split('/').pop()}` : 'No models available'}
              </p>
              {models && models.length > 0 && (
                <div className="flex flex-wrap justify-center gap-2">
                  {['Explain quantum computing', 'Write a poem about AI', 'Help me code'].map(prompt => (
                    <button key={prompt} onClick={() => setInput(prompt)} className="px-3 py-1.5 bg-muted hover:bg-accent border border-border rounded-lg text-sm text-muted-foreground transition-colors">
                      {prompt}
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          messages.map(message => <ChatMessageBubble key={message.id} message={message} onCopy={(text) => navigator.clipboard.writeText(text)} />)
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Input */}
      <div className="p-4 border-t border-border">
        <form onSubmit={handleSubmit} className="flex gap-3">
          <div className="flex-1 relative">
            <textarea
              ref={inputRef}
              value={input}
              onChange={handleInputChange}
              onKeyDown={handleKeyDown}
              placeholder={selectedModel ? "Type a message..." : "Select a model first"}
              className="w-full bg-input border border-border rounded-lg px-4 py-3 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-primary resize-none min-h-[52px] max-h-32"
              rows={1}
              disabled={isLoading || !selectedModel}
            />
          </div>

          <button
            type="submit"
            disabled={isLoading || !input.trim() || !selectedModel}
            className="flex items-center justify-center px-4 h-[52px] rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {isLoading ? <Loader2 className="w-5 h-5 animate-spin" /> : <Send className="w-5 h-5" />}
          </button>
        </form>
      </div>
    </div>
  );
}
