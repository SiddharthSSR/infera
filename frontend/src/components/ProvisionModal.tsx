import { useState } from 'react';
import { X, Cpu, DollarSign, Loader2, Box } from 'lucide-react';
import type { GPUOffering, ProvisionRequest, ProviderType, GPUType } from '../types';
import { useProvisionInstance } from '../hooks/useApi';

interface ProvisionModalProps {
  isOpen: boolean;
  onClose: () => void;
  offerings: GPUOffering[] | undefined;
}

const GPU_LABELS: Record<GPUType, string> = {
  RTX_4090: 'RTX 4090 (24GB)',
  RTX_4080: 'RTX 4080 (16GB)',
  A100_40GB: 'A100 40GB',
  A100_80GB: 'A100 80GB',
  H100: 'H100 (80GB)',
  L40S: 'L40S (48GB)',
};

const PROVIDER_LABELS: Record<ProviderType, string> = {
  runpod: 'RunPod',
  vastai: 'Vast.ai',
  lambda: 'Lambda',
  mock: 'Mock (Testing)',
};

// Available models with their requirements
const AVAILABLE_MODELS = [
  { id: 'mistralai/Mistral-7B-Instruct-v0.2', name: 'Mistral 7B Instruct', vram: 16, gated: false },
  { id: 'meta-llama/Llama-3-8B-Instruct', name: 'Llama 3 8B Instruct', vram: 18, gated: true },
  { id: 'microsoft/phi-2', name: 'Phi-2 (2.7B)', vram: 6, gated: false },
  { id: 'google/gemma-7b-it', name: 'Gemma 7B Instruct', vram: 16, gated: true },
];

// GPU VRAM in GB
const GPU_VRAM: Record<GPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

export function ProvisionModal({ isOpen, onClose, offerings }: ProvisionModalProps) {
  const [name, setName] = useState('infera-worker');
  const [provider, setProvider] = useState<ProviderType>('mock');
  const [gpuType, setGpuType] = useState<GPUType>('RTX_4090');
  const [gpuCount, setGpuCount] = useState(1);
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModel, setSelectedModel] = useState('mistralai/Mistral-7B-Instruct-v0.2');
  const [error, setError] = useState<string | null>(null);

  const provisionMutation = useProvisionInstance();

  // Get unique providers from offerings (cast to ProviderType[])
  const availableProviders: ProviderType[] = [...new Set(offerings?.map(o => o.provider) || ['mock' as ProviderType])];
  
  // Get offerings for selected provider
  const providerOfferings = offerings?.filter(o => o.provider === provider) || [];
  
  // Get unique GPU types for selected provider (cast to GPUType[])
  const availableGPUs: GPUType[] = [...new Set(providerOfferings.map(o => o.gpu_type))];

  // Get selected offering
  const selectedOffering = providerOfferings.find(
    o => o.gpu_type === gpuType && o.gpu_count === gpuCount
  );

  const estimatedCost = selectedOffering 
    ? (spotInstance ? selectedOffering.spot_price : selectedOffering.cost_per_hour) || selectedOffering.cost_per_hour
    : 0;

  // Filter models that fit in selected GPU
  const gpuVram = GPU_VRAM[gpuType] || 24;
  const compatibleModels = AVAILABLE_MODELS.filter(m => m.vram <= gpuVram * gpuCount);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    const request: ProvisionRequest = {
      name,
      provider,
      gpu_type: gpuType,
      gpu_count: gpuCount,
      spot_instance: spotInstance,
      max_cost_hour: estimatedCost * 1.5,
      models: provider !== 'mock' ? [selectedModel] : undefined,
    };

    try {
      await provisionMutation.mutateAsync(request);
      onClose();
      // Reset form
      setName('infera-worker');
      setGpuCount(1);
      setSpotInstance(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Provisioning failed');
    }
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div 
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onClose}
      />
      
      {/* Modal */}
      <div className="relative bg-gray-900 border border-gray-800 rounded-xl w-full max-w-lg mx-4 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-800">
          <h2 className="text-lg font-semibold text-white">Provision GPU Instance</h2>
          <button 
            onClick={onClose}
            className="p-1 text-gray-400 hover:text-white transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="p-4 space-y-4">
          {/* Name */}
          <div>
            <label className="block text-sm text-gray-400 mb-1.5">Instance Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="input w-full"
              placeholder="infera-worker"
            />
          </div>

          {/* Provider */}
          <div>
            <label className="block text-sm text-gray-400 mb-1.5">Provider</label>
            <select
              value={provider}
              onChange={(e) => {
                setProvider(e.target.value as ProviderType);
                // Reset GPU type when provider changes
                const newOfferings = offerings?.filter(o => o.provider === e.target.value);
                if (newOfferings && newOfferings.length > 0) {
                  setGpuType(newOfferings[0].gpu_type);
                }
              }}
              className="input w-full"
            >
              {availableProviders.map(p => (
                <option key={p} value={p}>{PROVIDER_LABELS[p] || p}</option>
              ))}
            </select>
          </div>

          {/* GPU Type */}
          <div>
            <label className="block text-sm text-gray-400 mb-1.5">GPU Type</label>
            <div className="grid grid-cols-2 gap-2">
              {(availableGPUs.length > 0 ? availableGPUs : ['RTX_4090', 'A100_40GB', 'A100_80GB', 'H100'] as GPUType[]).map(gpu => {
                const offering = providerOfferings.find(o => o.gpu_type === gpu);
                return (
                  <button
                    key={gpu}
                    type="button"
                    onClick={() => setGpuType(gpu)}
                    className={`p-3 rounded-lg border text-left transition-colors ${
                      gpuType === gpu
                        ? 'border-infera-500 bg-infera-500/10 text-white'
                        : 'border-gray-700 bg-gray-800/50 text-gray-300 hover:border-gray-600'
                    }`}
                  >
                    <div className="flex items-center gap-2 mb-1">
                      <Cpu className="w-4 h-4" />
                      <span className="font-medium text-sm">{GPU_LABELS[gpu] || gpu}</span>
                    </div>
                    {offering && (
                      <div className="text-xs text-gray-400">
                        ${offering.cost_per_hour.toFixed(2)}/hr
                        {offering.spot_price && (
                          <span className="text-amber-400 ml-1">
                            (spot: ${offering.spot_price.toFixed(2)})
                          </span>
                        )}
                      </div>
                    )}
                  </button>
                );
              })}
            </div>
          </div>

          {/* GPU Count */}
          <div>
            <label className="block text-sm text-gray-400 mb-1.5">GPU Count</label>
            <select
              value={gpuCount}
              onChange={(e) => setGpuCount(parseInt(e.target.value))}
              className="input w-full"
            >
              {[1, 2, 4, 8].map(count => (
                <option key={count} value={count}>{count} GPU{count > 1 ? 's' : ''}</option>
              ))}
            </select>
          </div>

          {/* Model Selection - Only show for non-mock providers */}
          {provider !== 'mock' && (
            <div>
              <label className="block text-sm text-gray-400 mb-1.5">
                <div className="flex items-center gap-2">
                  <Box className="w-4 h-4" />
                  Model to Load
                </div>
              </label>
              <select
                value={selectedModel}
                onChange={(e) => setSelectedModel(e.target.value)}
                className="input w-full"
              >
                {compatibleModels.map(model => (
                  <option key={model.id} value={model.id}>
                    {model.name} ({model.vram}GB VRAM)
                    {model.gated ? ' 🔒' : ''}
                  </option>
                ))}
              </select>
              {AVAILABLE_MODELS.find(m => m.id === selectedModel)?.gated && (
                <p className="text-xs text-amber-400 mt-1">
                  ⚠️ This model requires HuggingFace authentication. Set HF_TOKEN env var.
                </p>
              )}
              <p className="text-xs text-gray-500 mt-1">
                Model will be downloaded and loaded on startup (~5-15 min)
              </p>
            </div>
          )}

          {/* Spot Instance */}
          <div className="flex items-center gap-3">
            <input
              type="checkbox"
              id="spotInstance"
              checked={spotInstance}
              onChange={(e) => setSpotInstance(e.target.checked)}
              className="w-4 h-4 rounded border-gray-600 bg-gray-800 text-infera-500 focus:ring-infera-500"
            />
            <label htmlFor="spotInstance" className="text-sm text-gray-300">
              Use spot instance
              <span className="text-gray-500 ml-1">(cheaper but can be interrupted)</span>
            </label>
          </div>

          {/* Cost Estimate */}
          <div className="p-3 bg-gray-800/50 rounded-lg">
            <div className="flex items-center justify-between">
              <span className="text-gray-400">Estimated Cost</span>
              <div className="flex items-center gap-1">
                <DollarSign className="w-4 h-4 text-emerald-400" />
                <span className="text-xl font-bold text-white">
                  {(estimatedCost * gpuCount).toFixed(2)}
                </span>
                <span className="text-gray-400">/hr</span>
              </div>
            </div>
            <div className="text-xs text-gray-500 mt-1">
              ~${((estimatedCost * gpuCount) * 24).toFixed(2)}/day • 
              ~${((estimatedCost * gpuCount) * 720).toFixed(2)}/month
            </div>
          </div>

          {/* Error */}
          {error && (
            <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-sm text-red-400">
              {error}
            </div>
          )}

          {/* Actions */}
          <div className="flex gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="btn-secondary flex-1"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={provisionMutation.isPending}
              className="btn-primary flex-1 flex items-center justify-center gap-2"
            >
              {provisionMutation.isPending ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Provisioning...
                </>
              ) : (
                'Provision Instance'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}