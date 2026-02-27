import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Header } from './components/Header';
import { StatsCards } from './components/StatsCards';
import { WorkersList } from './components/WorkersList';
import { ChatPlayground } from './components/ChatPlayground';
import { InstancesList } from './components/InstancesList';
import { ProvisionModal } from './components/ProvisionModal';
import { CostDisplay } from './components/CostDisplay';
import { useWorkers, useModels, useStats, useInstances, useOfferings, useCosts } from './hooks/useApi';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
    },
  },
});

function Dashboard() {
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  
  const { data: workers, isLoading: workersLoading } = useWorkers();
  const { data: models } = useModels();
  const { data: stats, isLoading: statsLoading } = useStats();
  const { data: instances, isLoading: instancesLoading } = useInstances();
  const { data: offerings } = useOfferings();
  const { data: costs, isLoading: costsLoading } = useCosts();

  return (
    <div className="min-h-screen bg-gray-950">
      <Header />
      
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Stats Row */}
        <section className="mb-8">
          <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
            <StatsCards stats={stats} isLoading={statsLoading} />
            <CostDisplay costs={costs} isLoading={costsLoading} />
          </div>
        </section>

        {/* Main Content - 3 Column */}
        <div className="grid lg:grid-cols-3 gap-6">
          {/* GPU Instances */}
          <section>
            <InstancesList 
              instances={instances} 
              isLoading={instancesLoading}
              onProvision={() => setShowProvisionModal(true)}
            />
          </section>

          {/* Workers */}
          <section>
            <WorkersList workers={workers} isLoading={workersLoading} />
          </section>

          {/* Chat Playground */}
          <section>
            <ChatPlayground models={models} />
          </section>
        </div>
      </main>

      {/* Provision Modal */}
      <ProvisionModal
        isOpen={showProvisionModal}
        onClose={() => setShowProvisionModal(false)}
        offerings={offerings}
      />

      {/* Footer */}
      <footer className="border-t border-gray-800 mt-12 py-6">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <p className="text-center text-gray-500 text-sm">
            Infera v0.1.0 — Cost-efficient AI inference at scale
          </p>
        </div>
      </footer>
    </div>
  );
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Dashboard />
    </QueryClientProvider>
  );
}

export default App;
