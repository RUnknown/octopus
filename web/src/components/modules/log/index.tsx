'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useActiveRelayRequests, useLogs, useLogSiteActionTargets, type LogKeywordMode, type LogKeywordScope, type ActiveRelayRequest } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTargets } from './Item';
import { Activity, Clock, Loader2, RefreshCw } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore } from '@/components/modules/toolbar';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { useLogUIStore } from './ui-store';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';

type LogFilters = {
    keyword: string;
    keywordMode: LogKeywordMode;
    keywordScope: LogKeywordScope;
    channelIds: number[];
    startTime?: number;
    endTime?: number;
};

const LOG_PAGE_SIZE = 10;

function useDebouncedValue<T>(value: T, delay = 200) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const handle = setTimeout(() => setDebounced(value), delay);
        return () => clearTimeout(handle);
    }, [value, delay]);
    return debounced;
}

function filtersActive(filters: LogFilters) {
    return (
        !!filters.keyword.trim() ||
        filters.channelIds.length > 0 ||
        !!filters.startTime ||
        !!filters.endTime
    );
}

function formatDuration(ms: number) {
    if (ms < 1000) return `${ms}ms`;
    const seconds = ms / 1000;
    if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`;
    return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
}

function ActiveRequestRow({ request }: { request: ActiveRelayRequest }) {
    const t = useTranslations('log.active');
    return (
        <div className="rounded-lg border border-border/70 bg-muted/20 p-3 text-sm">
            <div className="flex items-start gap-3">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                    <Activity className="size-4" />
                </div>
                <div className="min-w-0 flex-1 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-medium text-foreground">{request.request_model}</span>
                        {request.actual_model ? <Badge variant="secondary" className="h-5 text-[11px]">{request.actual_model}</Badge> : null}
                        {request.streaming ? <Badge variant="outline" className="h-5 text-[11px]">{t('streaming')}</Badge> : null}
                        {request.used_ws ? <Badge variant="outline" className="h-5 text-[11px]">{t('ws')}</Badge> : null}
                    </div>
                    <div className="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2">
                        <span>{t('stage')}: <span className="font-medium text-foreground">{request.stage}</span></span>
                        <span>{t('attempt')}: <span className="font-mono text-foreground">{request.attempt ?? '-'}</span></span>
                        <span>{t('elapsed')}: <span className="font-mono text-foreground">{formatDuration(request.elapsed_ms)}</span></span>
                        <span>{t('stageElapsed')}: <span className="font-mono text-foreground">{formatDuration(request.stage_elapsed_ms)}</span></span>
                        <span>{t('channel')}: <span className="text-foreground">{request.channel_name ?? request.channel_id ?? '-'}</span></span>
                        <span>ID: <span className="font-mono text-foreground">{request.id}</span></span>
                    </div>
                    {request.last_message ? (
                        <div className="rounded-md bg-background/70 px-2 py-1.5 text-xs text-muted-foreground wrap-break-word">
                            {t('lastMessage')}: <span className="text-foreground">{request.last_message}</span>
                        </div>
                    ) : null}
                </div>
            </div>
        </div>
    );
}

function ActiveRequestsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
    const t = useTranslations('log.active');
    const activeQuery = useActiveRelayRequests(open);
    const requests = activeQuery.data ?? [];

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[calc(100vh-2rem)] w-[min(720px,calc(100vw-2rem))] max-w-none flex-col overflow-hidden p-0">
                <DialogHeader className="border-b border-border px-5 py-4 pr-12">
                    <div className="flex items-center gap-2">
                        <DialogTitle>{t('title')}</DialogTitle>
                        {activeQuery.isFetching ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
                    </div>
                    <DialogDescription>{t('description')}</DialogDescription>
                </DialogHeader>
                <div className="flex items-center justify-between border-b border-border px-5 py-2 text-xs text-muted-foreground">
                    <span>{t('count', { count: requests.length })}</span>
                    <Button type="button" variant="ghost" size="sm" onClick={() => void activeQuery.refetch()} disabled={activeQuery.isFetching}>
                        <RefreshCw className="size-4" />
                        {t('refresh')}
                    </Button>
                </div>
                <div className="min-h-0 flex-1 overflow-auto p-5">
                    {activeQuery.isLoading ? (
                        <div className="flex h-32 items-center justify-center text-muted-foreground">
                            <Loader2 className="size-5 animate-spin" />
                        </div>
                    ) : requests.length === 0 ? (
                        <div className="flex h-32 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
                            <Clock className="size-5" />
                            <span>{t('empty')}</span>
                        </div>
                    ) : (
                        <div className="space-y-2.5">
                            {requests.map((request) => <ActiveRequestRow key={request.id} request={request} />)}
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志（无过滤时）
 * - 过滤模式使用 cursor 分页，滚动加载更多
 */
export function Log() {
    const t = useTranslations('log');
    const [activeOpen, setActiveOpen] = useState(false);
    const pageKey = 'log' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const refreshRequestId = useLogUIStore((s) => s.refreshRequestId);
    const setRefreshing = useLogUIStore((s) => s.setRefreshing);
    const lastHandledRefreshRequestIdRef = useRef(refreshRequestId);
    const logDateRange = useToolbarViewOptionsStore((s) => s.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const logKeywordMode = useToolbarViewOptionsStore((s) => s.logKeywordMode);
    const logKeywordScope = useToolbarViewOptionsStore((s) => s.logKeywordScope);
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        keywordMode: logKeywordMode,
        keywordScope: logKeywordScope,
        channelIds: logChannelIds,
        startTime: logDateRange.start,
        endTime: logDateRange.end,
    }), [logDateRange.end, logDateRange.start, logChannelIds, searchTerm, logKeywordMode, logKeywordScope]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const filterMode = filtersActive(debouncedFilters);
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        keyword_mode: debouncedFilters.keyword.trim() ? debouncedFilters.keywordMode : undefined,
        keyword_scope: debouncedFilters.keyword.trim() ? debouncedFilters.keywordScope : undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
        start_time: debouncedFilters.startTime,
        end_time: debouncedFilters.endTime,
    }), [debouncedFilters]);
    const liveLogsQuery = useLogs({ pageSize: LOG_PAGE_SIZE, filters: logFilters, mode: filterMode ? 'paged' : 'stream' });
    const logs = liveLogsQuery.logs;
    const hasMore = liveLogsQuery.hasMore;
    const isLoading = liveLogsQuery.isLoading;
    const isLoadingMore = liveLogsQuery.isLoadingMore;
    const loadMore = liveLogsQuery.loadMore;
    const warning = liveLogsQuery.warning;

    const logIDs = useMemo(() => logs.map((log) => log.id), [logs]);
    const siteActionTargetsQuery = useLogSiteActionTargets(logIDs, logs.length > 0);
    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();
        const data = siteActionTargetsQuery.data ?? {};
        for (const [id, targets] of Object.entries(data)) {
            next.set(Number(id), targets);
        }
        return next;
    }, [siteActionTargetsQuery.data]);

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && logs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const refreshIdRef = useRef(0);
    const handleRefresh = useCallback(async () => {
        refreshIdRef.current += 1;
        const myId = refreshIdRef.current;
        setRefreshing(true);
        const startedAt = Date.now();
        try {
            await liveLogsQuery.refetch();
        } finally {
            const elapsed = Date.now() - startedAt;
            const remaining = Math.max(0, 500 - elapsed);
            setTimeout(() => {
                if (refreshIdRef.current === myId) setRefreshing(false);
            }, remaining);
        }
    }, [liveLogsQuery, setRefreshing]);

    useEffect(() => {
        if (refreshRequestId === lastHandledRefreshRequestIdRef.current) return;
        lastHandledRefreshRequestIdRef.current = refreshRequestId;
        void handleRefresh();
    }, [handleRefresh, refreshRequestId]);

    const footer = useMemo(() => {
        if (hasMore) {
            return (
                <div className="flex justify-center py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            );
        }
        if (logs.length > 0) {
            return (
                <div className="flex justify-center py-4">
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, logs.length, t]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <div className="flex shrink-0 items-center justify-end">
                <Button type="button" variant="outline" size="sm" onClick={() => setActiveOpen(true)}>
                    <Activity className="size-4" />
                    {t('active.open')}
                </Button>
            </div>
            {warning ? (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                    {warning}
                </div>
            ) : null}
            <ActiveRequestsDialog open={activeOpen} onOpenChange={setActiveOpen} />
            <div className="relative min-h-0 flex-1">
                <VirtualizedGrid
                    items={logs}
                    layout="list"
                    columns={{ default: 1 }}
                    estimateItemHeight={80}
                    overscan={8}
                    getItemKey={(log) => `log-${log.id}`}
                    renderItem={(log) => <LogCard log={log} siteTargets={siteActionTargets.get(log.id) ?? null} />}
                    footer={footer}
                    onReachEnd={handleReachEnd}
                    reachEndEnabled={canLoadMore}
                    reachEndOffset={2}
                />
            </div>
        </div>
    );
}
