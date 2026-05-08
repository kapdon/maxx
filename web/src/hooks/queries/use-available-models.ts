/**
 * Available Models React Query Hook
 * Fetches the concrete model IDs advertised by the runtime /v1/models endpoint.
 */

import { useQuery } from '@tanstack/react-query';
import { getTransport } from '@/lib/transport';

export const availableModelKeys = {
  all: ['availableModels'] as const,
  list: () => [...availableModelKeys.all, 'list'] as const,
};

export function useAvailableModels() {
  return useQuery({
    queryKey: availableModelKeys.list(),
    queryFn: () => getTransport().getAvailableModels(),
    staleTime: 5 * 60 * 1000,
  });
}
