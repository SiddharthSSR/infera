import { useMemo, useState } from 'react';
import { toast } from 'sonner';

const scalingDefaults = { minNodes: 1, maxNodes: 5, autoscaleTrigger: 80 };

export function useInstancesScalingState() {
  const [scalingMinNodes, setScalingMinNodes] = useState(scalingDefaults.minNodes);
  const [scalingMaxNodes, setScalingMaxNodes] = useState(scalingDefaults.maxNodes);
  const [scalingTrigger, setScalingTrigger] = useState(scalingDefaults.autoscaleTrigger);
  const [scalingSaved, setScalingSaved] = useState(scalingDefaults);

  const scalingDirty =
    scalingMinNodes !== scalingSaved.minNodes ||
    scalingMaxNodes !== scalingSaved.maxNodes ||
    scalingTrigger !== scalingSaved.autoscaleTrigger;

  const scalingErrors = useMemo(
    () => ({
      minNodes: scalingMinNodes >= scalingMaxNodes ? 'Min must be less than max nodes' : '',
      maxNodes: scalingMaxNodes <= scalingMinNodes ? 'Max must be greater than min nodes' : '',
      trigger: scalingTrigger < 0 || scalingTrigger > 100 ? 'Must be between 0 and 100' : '',
    }),
    [scalingMaxNodes, scalingMinNodes, scalingTrigger],
  );
  const scalingHasErrors = Object.values(scalingErrors).some(Boolean);

  const handleApplyScaling = () => {
    if (scalingHasErrors || !scalingDirty) return;
    setScalingSaved({ minNodes: scalingMinNodes, maxNodes: scalingMaxNodes, autoscaleTrigger: scalingTrigger });
    toast.success('Scaling configuration updated');
  };

  return {
    scaling: {
      minNodes: scalingMinNodes,
      maxNodes: scalingMaxNodes,
      trigger: scalingTrigger,
      errors: scalingErrors,
      dirty: scalingDirty,
      hasErrors: scalingHasErrors,
      onMinNodesChange: setScalingMinNodes,
      onMaxNodesChange: setScalingMaxNodes,
      onTriggerChange: setScalingTrigger,
      onApply: handleApplyScaling,
    },
  };
}
