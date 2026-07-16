/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { sendChatCompletion, streamChatCompletion } from './api';
import {
  parseChatCompletionResponse,
  parseChatCompletionStreamEvent,
} from './chatCompletion';
import type { ChatCompletionRequest } from '../types';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/openai_chat');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

function buildSSEFixture(...fixtureNames: string[]): string {
  const payload = fixtureNames
    .map((name) => `data: ${JSON.stringify(loadJSONFixture(name))}\n\n`)
    .join('');
  return `${payload}data: [DONE]\n\n`;
}

function createReadableBody(text: string) {
  const bytes = new TextEncoder().encode(text);
  let sent = false;

  return {
    getReader() {
      return {
        async read() {
          if (sent) {
            return { done: true, value: undefined };
          }
          sent = true;
          return { done: false, value: bytes };
        },
      };
    },
  };
}

describe('chat completion contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared unary response fixture with tool calls', () => {
    const payload = loadJSONFixture('chat_completion_response_tool_calls.json');

    const parsed = parseChatCompletionResponse(payload);

    expect(parsed.id).toBe('chatcmpl-req-openai-fixture');
    expect(parsed.choices[0]?.finish_reason).toBe('tool_calls');
    expect(parsed.choices[0]?.message.tool_calls?.[0]?.function?.name).toBe('web_search');
    expect(parsed.usage.total_tokens).toBe(6);
  });

  it('parses the shared stream tool-call delta fixture', () => {
    const payload = loadJSONFixture('chat_completion_stream_tool_calls_chunk.json');

    const parsed = parseChatCompletionStreamEvent(`data: ${JSON.stringify(payload)}`);

    expect(parsed.type).toBe('chunk');
    if (parsed.type !== 'chunk') {
      return;
    }
    expect(parsed.chunk.choices[0]?.delta.tool_calls?.[0]?.index).toBe(0);
    expect(parsed.chunk.choices[0]?.delta.tool_calls?.[0]?.function?.arguments).toBe(
      '{"query":"Go scheduler"}',
    );
  });

  it('sendChatCompletion uses the shared unary fixture shape', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => loadJSONFixture('chat_completion_response_tool_calls.json'),
    });

    const response = await sendChatCompletion({
      model: 'model-1',
      messages: [{ role: 'user', content: 'search go scheduler' }],
    });

    expect(mockFetch).toHaveBeenCalledWith(
      '/v1/chat/completions',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    expect(response.choices[0]?.message.tool_calls?.[0]?.id).toBe('call_1');
  });

  it('sendChatCompletion forwards the shared request fixture exactly', async () => {
    const request = loadJSONFixture('chat_completion_request_tool_calls.json') as ChatCompletionRequest;

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => loadJSONFixture('chat_completion_response_tool_calls.json'),
    });

    await sendChatCompletion(request);

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual(request);
  });

  it('streamChatCompletion consumes the shared SSE fixture and stops on DONE', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      body: createReadableBody(
        buildSSEFixture(
          'chat_completion_stream_initial_chunk.json',
          'chat_completion_stream_tool_calls_chunk.json',
          'chat_completion_stream_final_chunk.json',
        ),
      ),
    });

    const received: string[] = [];
    for await (const chunk of streamChatCompletion({
      model: 'model-1',
      messages: [{ role: 'user', content: 'search go scheduler' }],
    })) {
      received.push(chunk);
    }

    expect(received).toEqual([]);
  });

  it('streamChatCompletion preserves the shared request fixture and only flips stream=true', async () => {
    const request = loadJSONFixture('chat_completion_request_tool_calls.json') as ChatCompletionRequest;

    mockFetch.mockResolvedValueOnce({
      ok: true,
      body: createReadableBody('data: [DONE]\n\n'),
    });

    for await (const _ of streamChatCompletion(request)) {
      // Exhaust the generator to ensure the request is issued.
    }

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({
      ...request,
      stream: true,
    });
  });

  it('sendChatCompletion surfaces the shared invalid-request error fixture', async () => {
    const payload = loadJSONFixture('chat_completion_error_invalid_request.json');

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      headers: { get: () => 'application/json' },
      json: async () => payload,
    });

    await expect(
      sendChatCompletion({
        model: 'model-1',
        messages: [{ role: 'user', content: 'say hello' }],
      }),
    ).rejects.toThrow('Request failed (400 Bad Request): Invalid JSON: stop must be a string or array of strings');
  });

  it('streamChatCompletion surfaces the shared pre-commit inference error fixture', async () => {
    const payload = loadJSONFixture('chat_completion_error_inference_error.json');

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      headers: { get: () => 'application/json' },
      json: async () => payload,
    });

    await expect(
      (async () => {
        for await (const _ of streamChatCompletion({
          model: 'model-1',
          messages: [{ role: 'user', content: 'say hello' }],
        })) {
          // The generator should throw before yielding.
        }
      })(),
    ).rejects.toThrow('Request failed (500 Internal Server Error): worker error (status 503): upstream unavailable');
  });
});
