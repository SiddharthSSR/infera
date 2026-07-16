import type {
  ChatCompletionChunk,
  ChatCompletionResponse,
  ChatCompletionUsage,
  ChatMessage,
  ChatMessageRole,
  ChatToolCall,
  ChatToolFunction,
} from '../types';

type JSONRecord = Record<string, unknown>;

export type ChatCompletionStreamEvent =
  | { type: 'ignore' }
  | { type: 'done' }
  | { type: 'chunk'; chunk: ChatCompletionChunk };

function expectRecord(value: unknown, label: string): JSONRecord {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`Invalid ${label}`);
  }
  return value as JSONRecord;
}

function expectString(record: JSONRecord, key: string, label: string): string {
  const value = record[key];
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalString(record: JSONRecord, key: string, label: string): string | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function expectNumber(record: JSONRecord, key: string, label: string): number {
  const value = record[key];
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalNumber(record: JSONRecord, key: string, label: string): number | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalFinishReason(record: JSONRecord, key: string, label: string): string | null | undefined {
  const value = record[key];
  if (value == null) {
    return value as null | undefined;
  }
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function parseUsage(value: unknown, label: string): ChatCompletionUsage {
  const record = expectRecord(value, label);
  return {
    prompt_tokens: expectNumber(record, 'prompt_tokens', label),
    completion_tokens: expectNumber(record, 'completion_tokens', label),
    total_tokens: expectNumber(record, 'total_tokens', label),
  };
}

function parseToolFunction(value: unknown, label: string): ChatToolFunction {
  const record = expectRecord(value, label);
  const parsed: ChatToolFunction = {};
  const name = optionalString(record, 'name', label);
  const args = optionalString(record, 'arguments', label);
  if (name !== undefined) {
    parsed.name = name;
  }
  if (args !== undefined) {
    parsed.arguments = args;
  }
  return parsed;
}

function parseToolCall(value: unknown, label: string): ChatToolCall {
  const record = expectRecord(value, label);
  const parsed: ChatToolCall = {};

  const index = optionalNumber(record, 'index', label);
  const id = optionalString(record, 'id', label);
  const type = optionalString(record, 'type', label);
  const toolFunction = record.function == null ? undefined : parseToolFunction(record.function, `${label}.function`);

  if (index !== undefined) {
    parsed.index = index;
  }
  if (id !== undefined) {
    parsed.id = id;
  }
  if (type !== undefined) {
    parsed.type = type;
  }
  if (toolFunction !== undefined) {
    parsed.function = toolFunction;
  }

  return parsed;
}

function parseToolCalls(value: unknown, label: string): ChatToolCall[] | undefined {
  if (value == null) {
    return undefined;
  }
  if (!Array.isArray(value)) {
    throw new Error(`Invalid ${label}`);
  }
  return value.map((item, index) => parseToolCall(item, `${label}[${index}]`));
}

function parseMessage(value: unknown, label: string): ChatMessage {
  const record = expectRecord(value, label);
  return {
    role: expectString(record, 'role', label) as ChatMessageRole,
    content: expectString(record, 'content', label),
    name: optionalString(record, 'name', label),
    tool_calls: parseToolCalls(record.tool_calls, `${label}.tool_calls`),
    tool_call_id: optionalString(record, 'tool_call_id', label),
  };
}

function parseDelta(value: unknown, label: string): ChatCompletionChunk['choices'][number]['delta'] {
  const record = expectRecord(value, label);
  const parsed: ChatCompletionChunk['choices'][number]['delta'] = {};
  const role = optionalString(record, 'role', label);
  const content = optionalString(record, 'content', label);
  const toolCalls = parseToolCalls(record.tool_calls, `${label}.tool_calls`);

  if (role !== undefined) {
    parsed.role = role as ChatMessageRole;
  }
  if (content !== undefined) {
    parsed.content = content;
  }
  if (toolCalls !== undefined) {
    parsed.tool_calls = toolCalls;
  }

  return parsed;
}

function parseChunkChoice(
  value: unknown,
  label: string,
): ChatCompletionChunk['choices'][number] {
  const record = expectRecord(value, label);
  return {
    index: expectNumber(record, 'index', label),
    delta: parseDelta(record.delta, `${label}.delta`),
    finish_reason: optionalFinishReason(record, 'finish_reason', label),
  };
}

export function parseChatCompletionResponse(value: unknown): ChatCompletionResponse {
  const record = expectRecord(value, 'chat completion response');
  const choicesValue = record.choices;
  if (!Array.isArray(choicesValue)) {
    throw new Error('Invalid chat completion response.choices');
  }

  return {
    id: expectString(record, 'id', 'chat completion response'),
    object: expectString(record, 'object', 'chat completion response'),
    created: expectNumber(record, 'created', 'chat completion response'),
    model: expectString(record, 'model', 'chat completion response'),
    choices: choicesValue.map((choice, index) => {
      const choiceRecord = expectRecord(choice, `chat completion response.choices[${index}]`);
      return {
        index: expectNumber(choiceRecord, 'index', `chat completion response.choices[${index}]`),
        message: parseMessage(choiceRecord.message, `chat completion response.choices[${index}].message`),
        finish_reason: optionalFinishReason(
          choiceRecord,
          'finish_reason',
          `chat completion response.choices[${index}]`,
        ) ?? null,
      };
    }),
    usage: parseUsage(record.usage, 'chat completion response.usage'),
  };
}

export function parseChatCompletionChunk(value: unknown): ChatCompletionChunk {
  const record = expectRecord(value, 'chat completion chunk');
  const choicesValue = record.choices;
  if (!Array.isArray(choicesValue)) {
    throw new Error('Invalid chat completion chunk.choices');
  }

  return {
    id: expectString(record, 'id', 'chat completion chunk'),
    object: expectString(record, 'object', 'chat completion chunk'),
    created: expectNumber(record, 'created', 'chat completion chunk'),
    model: expectString(record, 'model', 'chat completion chunk'),
    choices: choicesValue.map((choice, index) =>
      parseChunkChoice(choice, `chat completion chunk.choices[${index}]`),
    ),
    usage: record.usage == null ? undefined : parseUsage(record.usage, 'chat completion chunk.usage'),
  };
}

export function parseChatCompletionStreamEvent(line: string): ChatCompletionStreamEvent {
  if (!line.startsWith('data: ')) {
    return { type: 'ignore' };
  }

  const payload = line.slice(6).trim();
  if (payload.length === 0) {
    return { type: 'ignore' };
  }
  if (payload === '[DONE]') {
    return { type: 'done' };
  }

  return {
    type: 'chunk',
    chunk: parseChatCompletionChunk(JSON.parse(payload)),
  };
}
