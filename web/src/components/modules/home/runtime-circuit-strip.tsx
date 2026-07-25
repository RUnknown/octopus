'use client';

import { useState } from 'react';
import { useClearRuntimeOverview, useRuntimeOverview } from '@/api/endpoints/runtime';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { Activity, Loader2, ShieldAlert, Trash2, TrendingDown } from 'lucide-react';

function stateLabel(state: string) {
    if (state === 'open') return '熔断中';
    if (state === 'half_open') return '半开试探';
    if (state === 'closed') return '已恢复';
    return state;
}

function stateClass(state: string) {
    if (state === 'open') return 'border-destructive/30 bg-destructive/10 text-destructive';
    if (state === 'half_open') return 'border-amber-500/30 bg-amber-500/10 text-amber-800 dark:text-amber-200';
    return 'border-border/70 bg-muted/40 text-muted-foreground';
}

function failRateClass(rate: number) {
    if (rate >= 50) return 'text-destructive';
    if (rate >= 20) return 'text-amber-700 dark:text-amber-300';
    return 'text-muted-foreground';
}

export function RuntimeCircuitStrip() {
    const { data, isLoading, error } = useRuntimeOverview();
    const clearRuntime = useClearRuntimeOverview();
    const [clearOpen, setClearOpen] = useState(false);
    if ((isLoading && !data) || error) return null;

    const circuits = data?.circuits ?? [];
    const health = data?.channel_health ?? [];
    const open = data?.open_circuits ?? 0;
    const halfOpen = data?.half_open_circuits ?? 0;
    const unhealthy = data?.unhealthy_count ?? 0;

    if (open === 0 && halfOpen === 0 && health.length === 0) {
        return (
            <div className="flex items-center gap-2 rounded-2xl border border-border/60 bg-card/60 px-3 py-2 text-xs text-muted-foreground">
                <Activity className="size-3.5" />
                运行态：当前无熔断，也无高失败率渠道
            </div>
        );
    }

    return (
        <div className="space-y-3 rounded-2xl border border-border/70 bg-card/70 p-3">
            <div className="flex flex-wrap items-center gap-2">
                <ShieldAlert className="size-4 text-amber-600" />
                <span className="text-sm font-medium text-foreground">运行态</span>
                {open > 0 ? <Badge variant="outline" className={cn('rounded-full', stateClass('open'))}>熔断 {open}</Badge> : null}
                {halfOpen > 0 ? <Badge variant="outline" className={cn('rounded-full', stateClass('half_open'))}>半开 {halfOpen}</Badge> : null}
                {unhealthy > 0 ? (
                    <Badge variant="outline" className="rounded-full border-amber-500/30 bg-amber-500/10 text-amber-800 dark:text-amber-200">
                        高失败 {unhealthy}
                    </Badge>
                ) : null}
                <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="ml-auto h-7 rounded-lg px-2 text-xs text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    onClick={() => setClearOpen(true)}
                    disabled={clearRuntime.isPending}
                >
                    {clearRuntime.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                    清空
                </Button>
            </div>

            {circuits.length > 0 ? (
                <div>
                    <div className="mb-1.5 text-[11px] font-medium text-muted-foreground">熔断器</div>
                    <div className="max-h-36 space-y-1.5 overflow-y-auto">
                        {circuits.slice(0, 12).map((circuit) => (
                            <div key={`${circuit.channel_id}-${circuit.channel_key_id}-${circuit.model_name}-${circuit.state}`} className="flex items-center justify-between gap-2 rounded-xl border border-border/50 bg-background/60 px-2.5 py-1.5 text-xs">
                                <div className="min-w-0">
                                    <div className="truncate font-medium text-foreground">
                                        {circuit.channel_name || `渠道 #${circuit.channel_id}`}
                                        <span className="ml-1.5 font-normal text-muted-foreground">· {circuit.model_name}</span>
                                    </div>
                                    <div className="text-[11px] text-muted-foreground">
                                        连续失败 {circuit.consecutive_failures} · 触发 {circuit.trip_count}
                                        {circuit.remaining_cooldown_ms > 0 ? ` · 剩余 ${Math.ceil(circuit.remaining_cooldown_ms / 1000)}s` : ''}
                                    </div>
                                </div>
                                <Badge variant="outline" className={cn('shrink-0 rounded-full', stateClass(circuit.state))}>{stateLabel(circuit.state)}</Badge>
                            </div>
                        ))}
                    </div>
                </div>
            ) : null}

            {health.length > 0 ? (
                <div>
                    <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
                        <TrendingDown className="size-3.5" />
                        渠道失败率（近 {data?.health_window || health[0]?.window || '1h'}）
                    </div>
                    <div className="max-h-36 space-y-1.5 overflow-y-auto">
                        {health.slice(0, 10).map((item) => (
                            <div key={item.channel_id} className="flex items-center justify-between gap-2 rounded-xl border border-border/50 bg-background/60 px-2.5 py-1.5 text-xs">
                                <div className="min-w-0">
                                    <div className="truncate font-medium text-foreground">
                                        {item.channel_name || `渠道 #${item.channel_id}`}
                                        {!item.enabled ? <span className="ml-1.5 text-[10px] font-normal text-muted-foreground">已停用</span> : null}
                                    </div>
                                    <div className="text-[11px] text-muted-foreground">成功 {item.request_success} · 失败 {item.request_failed} · 共 {item.total_requests}</div>
                                </div>
                                <span className={cn('shrink-0 text-sm font-semibold tabular-nums', failRateClass(item.fail_rate))}>{item.fail_rate.toFixed(0)}%</span>
                            </div>
                        ))}
                    </div>
                </div>
            ) : null}

            <AlertDialog open={clearOpen} onOpenChange={(openState) => {
                if (!clearRuntime.isPending) setClearOpen(openState);
            }}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>清空运行态？</AlertDialogTitle>
                        <AlertDialogDescription>
                            将清除当前熔断器状态和近 1 小时渠道失败率样本。请求日志、统计汇总和渠道配置不会受到影响，后续请求会重新累积运行态数据。
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={clearRuntime.isPending}>取消</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                            disabled={clearRuntime.isPending}
                            onClick={() => clearRuntime.mutate(undefined, {
                                onSuccess: () => {
                                    setClearOpen(false);
                                    toast.success('运行态已清空');
                                },
                                onError: (mutationError) => toast.error('清空运行态失败', { description: mutationError.message }),
                            })}
                        >
                            {clearRuntime.isPending ? '清空中...' : '确认清空'}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    );
}
