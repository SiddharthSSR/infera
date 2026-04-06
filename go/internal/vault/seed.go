package vault

import (
	"errors"
	"log/slog"
)

// SeedDefaultModels ensures well-known default models exist in the registry.
// This is idempotent and additive: missing defaults are inserted, existing ones are left unchanged.
func SeedDefaultModels(store *Store) error {
	models := []Model{
		{
			Name:         "Mistral 7B Instruct v0.3",
			Source:       "huggingface",
			SourceURI:    "mistralai/Mistral-7B-Instruct-v0.3",
			Parameters:   "7B",
			Quantization: "none",
			VRAMRequired: 16384,
			MaxContext:   32768,
			Family:       "mistral",
			Tags:         []string{"chat", "instruct", "general"},
			Status:       "available",
		},
			{
				Name:         "Mistral 7B Instruct v0.3 (AWQ)",
				Source:       "huggingface",
				SourceURI:    "solidrust/Mistral-7B-Instruct-v0.3-AWQ",
				Parameters:   "7B",
				Quantization: "awq",
				VRAMRequired: 6144,
				MaxContext:   32768,
				Family:       "mistral",
				Tags:         []string{"chat", "instruct", "quantized"},
				Status:       "available",
			},
		{
			Name:         "Llama 3.1 8B Instruct",
			Source:       "huggingface",
			SourceURI:    "meta-llama/Meta-Llama-3.1-8B-Instruct",
			Parameters:   "8B",
			Quantization: "none",
			VRAMRequired: 18432,
			MaxContext:   131072,
			Family:       "llama",
			Tags:         []string{"chat", "instruct", "general", "coding"},
			Status:       "available",
		},
		{
			Name:         "Llama 3.1 70B Instruct",
			Source:       "huggingface",
			SourceURI:    "meta-llama/Meta-Llama-3.1-70B-Instruct",
			Parameters:   "70B",
			Quantization: "none",
			VRAMRequired: 143360,
			MaxContext:   131072,
			Family:       "llama",
			Tags:         []string{"chat", "instruct", "general", "coding"},
			Status:       "available",
		},
		{
			Name:         "Phi-3 Mini 4K Instruct",
			Source:       "huggingface",
			SourceURI:    "microsoft/Phi-3-mini-4k-instruct",
			Parameters:   "3.8B",
			Quantization: "none",
			VRAMRequired: 8192,
			MaxContext:   4096,
			Family:       "phi",
			Tags:         []string{"chat", "instruct", "compact"},
			Status:       "available",
		},
			{
				Name:         "Qwen2.5 7B Instruct",
				Source:       "huggingface",
				SourceURI:    "Qwen/Qwen2.5-7B-Instruct",
			Parameters:   "7B",
			Quantization: "none",
			VRAMRequired: 16384,
			MaxContext:   131072,
			Family:       "qwen",
				Tags:         []string{"chat", "instruct", "multilingual"},
				Status:       "available",
			},
			{
				Name:         "Qwen2.5 7B Instruct (AWQ)",
				Source:       "huggingface",
				SourceURI:    "Qwen/Qwen2.5-7B-Instruct-AWQ",
				Parameters:   "7B",
				Quantization: "awq",
				VRAMRequired: 8192,
				MaxContext:   131072,
				Family:       "qwen",
				Tags:         []string{"chat", "instruct", "multilingual", "quantized"},
				Status:       "available",
			},
			{
				Name:         "Qwen2.5 7B Instruct (GPTQ Int4)",
				Source:       "huggingface",
				SourceURI:    "Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4",
				Parameters:   "7B",
				Quantization: "gptq-int4",
				VRAMRequired: 8192,
				MaxContext:   131072,
				Family:       "qwen",
				Tags:         []string{"chat", "instruct", "multilingual", "quantized"},
				Status:       "available",
			},
			{
				Name:         "Qwen2.5 7B Instruct (GPTQ Int8)",
				Source:       "huggingface",
				SourceURI:    "Qwen/Qwen2.5-7B-Instruct-GPTQ-Int8",
				Parameters:   "7B",
				Quantization: "gptq-int8",
				VRAMRequired: 12288,
				MaxContext:   131072,
				Family:       "qwen",
				Tags:         []string{"chat", "instruct", "multilingual", "quantized"},
				Status:       "available",
			},
			{
				Name:         "Qwen3 4B Thinking 2507",
				Source:       "huggingface",
				SourceURI:    "Qwen/Qwen3-4B-Thinking-2507",
			Parameters:   "4B",
			Quantization: "none",
			VRAMRequired: 12288,
			MaxContext:   262144,
			Family:       "qwen",
			Tags:         []string{"chat", "reasoning", "multilingual"},
			Status:       "available",
		},
		{
			Name:         "Kimi K2.5 Instruct",
			Source:       "huggingface",
			SourceURI:    "moonshotai/Kimi-K2.5-Instruct",
			Parameters:   "1T MoE",
			Quantization: "none",
			VRAMRequired: 675840,
			MaxContext:   131072,
			Family:       "kimi",
			Tags:         []string{"chat", "reasoning", "coding", "moe"},
			Status:       "available",
		},
		{
			Name:         "CodeLlama 13B Instruct",
			Source:       "huggingface",
			SourceURI:    "codellama/CodeLlama-13b-Instruct-hf",
			Parameters:   "13B",
			Quantization: "none",
			VRAMRequired: 28672,
			MaxContext:   16384,
			Family:       "llama",
			Tags:         []string{"coding", "instruct"},
			Status:       "available",
		},
		{
			Name:         "Gemma 2 9B Instruct",
			Source:       "huggingface",
			SourceURI:    "google/gemma-2-9b-it",
			Parameters:   "9B",
			Quantization: "none",
			VRAMRequired: 20480,
			MaxContext:   8192,
			Family:       "gemma",
			Tags:         []string{"chat", "instruct", "general"},
			Status:       "available",
		},
		{
			Name:         "Hermes 3 Llama 3.1 8B",
			Source:       "huggingface",
			SourceURI:    "NousResearch/Hermes-3-Llama-3.1-8B",
			Parameters:   "8B",
			Quantization: "none",
			VRAMRequired: 18432,
			MaxContext:   131072,
			Family:       "hermes",
			Tags:         []string{"chat", "instruct", "function-calling", "agentic"},
			Status:       "available",
		},
		{
			Name:         "Hermes 3 Llama 3.1 70B",
			Source:       "huggingface",
			SourceURI:    "NousResearch/Hermes-3-Llama-3.1-70B",
			Parameters:   "70B",
			Quantization: "none",
			VRAMRequired: 143360,
			MaxContext:   131072,
			Family:       "hermes",
			Tags:         []string{"chat", "instruct", "function-calling", "agentic"},
			Status:       "available",
		},
	}

	inserted := 0
	for i := range models {
		_, err := store.GetBySourceURI(models[i].SourceURI)
		if err == nil {
			continue
		}
		if err != nil && !errors.Is(err, ErrModelNotFound) {
			return err
		}
		if err := store.Create(&models[i]); err != nil {
			return err
		}
		inserted++
	}

	slog.Info(
		"vault seeded default models",
		slog.Int("inserted", inserted),
		slog.Int("defaults", len(models)),
	)
	return nil
}
