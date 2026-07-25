import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export type RuntimeCircuit = {
    channel_id: number;
    channel_name: string;
    channel_key_id: number;
    model_name: string;
    state: 'open' | 'half_open' | 'closed' | string;
    consecutive_failures: number;
    trip_count: number;
    remaining_cooldown_ms: number;
};

export type RuntimeChannelHealth = {
    channel_id: number;
    channel_name: string;
    request_success: number;
    request_failed: number;
    total_requests: number;
    fail_rate: number;
    enabled: boolean;
    window?: string;
};

export type RuntimeOverview = {
    open_circuits: number;
    half_open_circuits: number;
    circuits: RuntimeCircuit[];
    channel_health?: RuntimeChannelHealth[];
    unhealthy_count?: number;
    health_window?: string;
};

export function useRuntimeOverview(enabled = true) {
    return useQuery({
        queryKey: ['runtime', 'overview'],
        queryFn: async () => apiClient.get<RuntimeOverview>('/api/v1/runtime/overview'),
        enabled,
        refetchInterval: 15000,
    });
}

export function useClearRuntimeOverview() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () => apiClient.delete<null>('/api/v1/runtime/clear'),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ['runtime', 'overview'] });
        },
    });
}
