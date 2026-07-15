import type { ChatCompletionRequest, ChatCompletionResponse } from '../types';
import { API_BASE, authFetch, readResponseError } from './apiCore';
import { parseChatCompletionResponse, parseChatCompletionStreamEvent } from './chatCompletion';

export interface StreamChatCompletionOptions {
  onUsage?: (usage: ChatCompletionResponse['usage']) => void;
}

export async function sendChatCompletion(request: ChatCompletionRequest): Promise<ChatCompletionResponse> {
  const response = await authFetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Request failed'));
  }

  return parseChatCompletionResponse(await response.json());
}

export async function* streamChatCompletion(
  request: ChatCompletionRequest,
  options?: StreamChatCompletionOptions,
): AsyncGenerator<string, void, unknown> {
  const response = await authFetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ ...request, stream: true }),
  });

  if (!response.ok) {
    const message = await readResponseError(response, 'Request failed');
    if (message.toLowerCase().includes('ngrok')) {
      throw new Error('Please visit the ngrok URL directly in your browser first to bypass the interstitial page');
    }
    throw new Error(message);
  }

  const reader = response.body?.getReader();
  if (!reader) throw new Error('No response body');

  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    for (const line of lines) {
      try {
        const event = parseChatCompletionStreamEvent(line);
        if (event.type === 'done') return;
        if (event.type !== 'chunk') continue;

        const usage = event.chunk.usage;
        if (usage?.prompt_tokens != null && usage?.completion_tokens != null && usage?.total_tokens != null) {
          options?.onUsage?.(usage);
        }

        const content = event.chunk.choices[0]?.delta?.content;
        if (content) yield content;
      } catch {
        // Ignore parse errors for individual chunks
      }
    }
  }
}
