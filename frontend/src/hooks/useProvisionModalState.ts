import { useEffect, useState } from 'react';
import type { NavigateFunction, SetURLSearchParams } from 'react-router-dom';

import type { DeploymentAttemptRecord } from '../lib/deploymentHistory';
import type { ProvisionDraft } from '../lib/instanceProvisioning';

export function useProvisionModalState({
  searchParams,
  setSearchParams,
  navigate,
  onProvisionedSuccess,
}: {
  searchParams: URLSearchParams;
  setSearchParams: SetURLSearchParams;
  navigate: NavigateFunction;
  onProvisionedSuccess: () => void;
}) {
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  const [provisionModalReturnTo, setProvisionModalReturnTo] = useState<string | null>(null);
  const [preselectedModel, setPreselectedModel] = useState<string | null>(null);
  const [provisionDraft, setProvisionDraft] = useState<ProvisionDraft | null>(null);

  useEffect(() => {
    if (searchParams.get('provision') === 'true') {
      const model = searchParams.get('model');
      const from = searchParams.get('from');
      if (model) setPreselectedModel(model);
      setProvisionDraft(null);
      setProvisionModalReturnTo(from ? `/${from}` : null);
      setShowProvisionModal(true);
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const openFreshProvisionModal = () => {
    setProvisionDraft(null);
    setShowProvisionModal(true);
  };

  const openRetryModal = (attempt: DeploymentAttemptRecord) => {
    setProvisionDraft(attempt.request);
    setPreselectedModel(attempt.request.models?.length === 1 ? attempt.request.models[0] : null);
    setShowProvisionModal(true);
  };

  const closeProvisionModal = () => {
    setShowProvisionModal(false);
    setPreselectedModel(null);
    setProvisionDraft(null);
    if (provisionModalReturnTo) {
      navigate(provisionModalReturnTo);
      setProvisionModalReturnTo(null);
    }
  };

  const handleProvisioned = () => {
    onProvisionedSuccess();
    setProvisionModalReturnTo(null);
  };

  const handleProvisionFailed = (request: ProvisionDraft) => {
    setProvisionDraft(request);
  };

  const openWorkspaceFromModal = () => {
    setShowProvisionModal(false);
    navigate('/workspace');
  };

  return {
    showProvisionModal,
    preselectedModel,
    provisionDraft,
    openFreshProvisionModal,
    openRetryModal,
    closeProvisionModal,
    handleProvisioned,
    handleProvisionFailed,
    openWorkspaceFromModal,
  };
}
