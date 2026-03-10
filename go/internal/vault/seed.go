package vault

import "log/slog"

// SeedDefaultModels populates the registry with well-known models if it's empty.
// This is idempotent — it skips seeding if any models already exist.
func SeedDefaultModels(store *Store) error {
	count, err := store.Count()
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Info("vault seed skipped, models already exist", slog.Int("count", count))
		return nil
	}

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
			SourceURI:    "TheBloke/Mistral-7B-Instruct-v0.3-AWQ",
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
	}

	for i := range models {
		if err := store.Create(&models[i]); err != nil {
			return err
		}
	}

	slog.Info("vault seeded default models", slog.Int("count", len(models)))
	return nil
}
