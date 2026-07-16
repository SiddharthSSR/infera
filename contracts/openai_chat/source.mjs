export const contractLiterals = {
  completionObject: 'chat.completion',
  completionChunkObject: 'chat.completion.chunk',
  toolTypeFunction: 'function',
  errorTypeInferenceError: 'inference_error',
  errorTypeInvalidRequest: 'invalid_request',
};

const webSearchToolDefinition = {
  type: contractLiterals.toolTypeFunction,
  function: {
    name: 'web_search',
    description: 'Search the web for recent results.',
    parameters: {
      type: 'object',
      properties: {
        query: {
          type: 'string',
        },
      },
      required: ['query'],
    },
  },
};

const webSearchToolChoice = {
  type: contractLiterals.toolTypeFunction,
  function: {
    name: 'web_search',
  },
};

const assistantToolCall = {
  id: 'call_1',
  type: contractLiterals.toolTypeFunction,
  function: {
    name: 'web_search',
    arguments: '{"query":"Go scheduler trade-offs"}',
  },
};

const responseToolCall = {
  id: 'call_1',
  type: contractLiterals.toolTypeFunction,
  function: {
    name: 'web_search',
    arguments: '{"query":"Go scheduler"}',
  },
};

export const generatedTypes = `export type ChatMessageRole = 'system' | 'user' | 'assistant' | 'tool';

export interface ChatToolFunction {
  name?: string;
  arguments?: string;
}

export interface ChatToolCall {
  index?: number;
  id?: string;
  type?: string;
  function?: ChatToolFunction;
}

export interface ChatToolDefinition {
  type: string;
  function: {
    name: string;
    description?: string;
    parameters?: unknown;
  };
}

export interface ChatToolChoiceObject {
  type: string;
  function?: {
    name: string;
  };
}

export type ChatToolChoice =
  | 'none'
  | 'auto'
  | ChatToolChoiceObject
  | Record<string, unknown>;

export interface ChatMessage {
  role: ChatMessageRole;
  content: string;
  name?: string;
  tool_calls?: ChatToolCall[];
  tool_call_id?: string;
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  temperature?: number;
  top_p?: number;
  max_tokens?: number;
  stop?: string | string[];
  stream?: boolean;
  seed?: number;
  presence_penalty?: number;
  frequency_penalty?: number;
  tools?: ChatToolDefinition[];
  tool_choice?: ChatToolChoice;
}

export interface ChatCompletionUsage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatCompletionChoice {
  index: number;
  message: ChatMessage;
  finish_reason: string | null;
}

export interface ChatCompletionResponse {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: ChatCompletionChoice[];
  usage: ChatCompletionUsage;
}

export interface ChatCompletionDelta {
  role?: ChatMessageRole;
  content?: string;
  tool_calls?: ChatToolCall[];
}

export interface ChatCompletionChunkChoice {
  index: number;
  delta: ChatCompletionDelta;
  finish_reason?: string | null;
}

export interface ChatCompletionChunk {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: ChatCompletionChunkChoice[];
  usage?: ChatCompletionUsage;
}

export interface ChatCompletionError {
  type?: string;
  message: string;
}

export interface ChatCompletionErrorResponse {
  error: ChatCompletionError;
}
`;

export const fixtures = {
  'chat_completion_error_inference_error.json': {
    error: {
      type: contractLiterals.errorTypeInferenceError,
      message: 'worker error (status 503): upstream unavailable',
    },
  },
  'chat_completion_error_invalid_request.json': {
    error: {
      type: contractLiterals.errorTypeInvalidRequest,
      message: 'Invalid JSON: stop must be a string or array of strings',
    },
  },
  'chat_completion_request_tool_calls.json': {
    model: 'model-1',
    messages: [
      {
        role: 'user',
        content: 'Search for Go scheduler trade-offs',
      },
      {
        role: 'assistant',
        content: '',
        tool_calls: [assistantToolCall],
      },
      {
        role: 'tool',
        content: '{"results":["example"]}',
        tool_call_id: 'call_1',
      },
    ],
    temperature: 0.25,
    top_p: 0.9,
    max_tokens: 128,
    stop: ['<END>', '</tool>'],
    stream: false,
    seed: 42,
    presence_penalty: 0.4,
    frequency_penalty: 0.3,
    tools: [webSearchToolDefinition],
    tool_choice: webSearchToolChoice,
  },
  'chat_completion_response_tool_calls.json': {
    id: 'chatcmpl-req-openai-fixture',
    object: contractLiterals.completionObject,
    created: 0,
    model: 'model-1',
    choices: [
      {
        index: 0,
        message: {
          role: 'assistant',
          content: '',
          tool_calls: [responseToolCall],
        },
        finish_reason: 'tool_calls',
      },
    ],
    usage: {
      prompt_tokens: 5,
      completion_tokens: 1,
      total_tokens: 6,
    },
  },
  'chat_completion_stream_final_chunk.json': {
    id: 'chatcmpl-req-openai-fixture',
    object: contractLiterals.completionChunkObject,
    created: 0,
    model: 'model-1',
    choices: [
      {
        index: 1,
        delta: {},
        finish_reason: 'tool_calls',
      },
    ],
  },
  'chat_completion_stream_initial_chunk.json': {
    id: 'chatcmpl-req-openai-fixture',
    object: contractLiterals.completionChunkObject,
    created: 0,
    model: 'model-1',
    choices: [
      {
        index: 0,
        delta: {
          role: 'assistant',
        },
        finish_reason: null,
      },
    ],
  },
  'chat_completion_stream_tool_calls_chunk.json': {
    id: 'chatcmpl-req-openai-fixture',
    object: contractLiterals.completionChunkObject,
    created: 0,
    model: 'model-1',
    choices: [
      {
        index: 0,
        delta: {
          tool_calls: [
            {
              index: 0,
              ...responseToolCall,
            },
          ],
        },
        finish_reason: null,
      },
    ],
  },
};
