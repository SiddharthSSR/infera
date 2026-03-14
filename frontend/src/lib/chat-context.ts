import { createContext, useContext } from 'react';
import type { ChatMessage } from '../types';

export interface Message extends ChatMessage {
  id: string;
  timestamp: Date;
}

export interface PlaygroundHistoryEntry {
  id: string;
  time: string;
  latencyMs: number;
  preview: string;
  promptTokens?: number;
  completionTokens?: number;
}

export interface ChatContextType {
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  history: PlaygroundHistoryEntry[];
  setHistory: React.Dispatch<React.SetStateAction<PlaygroundHistoryEntry[]>>;
  selectedModel: string;
  setSelectedModel: (model: string) => void;
  temperature: number;
  setTemperature: (temp: number) => void;
  maxTokens: number;
  setMaxTokens: (tokens: number) => void;
}

export const ChatContext = createContext<ChatContextType | null>(null);

export function useChat() {
  const context = useContext(ChatContext);
  if (!context) throw new Error('useChat must be used within ChatProvider');
  return context;
}
