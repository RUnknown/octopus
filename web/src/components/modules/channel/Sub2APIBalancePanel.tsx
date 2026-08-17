'use client';

import { useMemo, useState } from 'react';
import { CircleDollarSign, LoaderCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { type Channel, useTestSub2APIBalance } from '@/api/endpoints/channel';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

function formatBalance(value: number, unit: string) {
    return new Intl.NumberFormat(undefined, {
        minimumFractionDigits: 0,
        maximumFractionDigits: 6,
    }).format(value) + ` ${unit}`;
}

export function Sub2APIBalancePanel({ channel }: { channel: Channel }) {
    const t = useTranslations('channel.detail.sub2apiBalance');
    const enabledKeys = useMemo(() => channel.keys.filter((key) => key.enabled && key.channel_key), [channel.keys]);
    const [keyID, setKeyID] = useState(enabledKeys[0]?.id ? String(enabledKeys[0].id) : '');
    const testBalance = useTestSub2APIBalance();
    const result = testBalance.data;

    const runTest = () => {
        testBalance.mutate({
            channel_id: channel.id,
            key_id: keyID ? Number(keyID) : undefined,
        });
    };

    return (
        <section className="mt-4 min-w-0 space-y-4 rounded-2xl border bg-card p-3 sm:p-4">
            <div>
                <h4 className="flex items-center gap-2 text-sm font-semibold">
                    <CircleDollarSign className="size-4 text-primary" />
                    {t('title')}
                </h4>
                <p className="mt-1 text-xs text-muted-foreground">{t('description')}</p>
            </div>

            <label className="block min-w-0 space-y-1 text-xs text-muted-foreground">
                <span>{t('key')}</span>
                <Select value={keyID} onValueChange={setKeyID}>
                    <SelectTrigger className="w-full min-w-0 rounded-xl">
                        <SelectValue placeholder={t('keyPlaceholder')} />
                    </SelectTrigger>
                    <SelectContent>
                        {enabledKeys.map((key) => (
                            <SelectItem key={key.id} value={String(key.id)}>
                                {key.remark || `Key #${key.id}`}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </label>

            <Button
                type="button"
                onClick={runTest}
                disabled={testBalance.isPending || !keyID}
                className="w-full rounded-xl"
            >
                {testBalance.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <CircleDollarSign className="size-4" />}
                {testBalance.isPending ? t('querying') : t('query')}
            </Button>

            {testBalance.error ? (
                <p className="break-all rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
                    {testBalance.error.message}
                </p>
            ) : null}

            {result ? (
                <div className="min-w-0 space-y-3 rounded-xl border bg-background p-3">
                    <div>
                        <p className="text-xs text-muted-foreground">{t('remaining')}</p>
                        <p className="break-all text-2xl font-bold text-primary">
                            {formatBalance(result.remaining, result.unit)}
                        </p>
                    </div>
                    <dl className="grid min-w-0 gap-2 text-xs sm:grid-cols-2">
                        {result.plan_name ? (
                            <div className="min-w-0">
                                <dt className="text-muted-foreground">{t('plan')}</dt>
                                <dd className="break-all font-medium">{result.plan_name}</dd>
                            </div>
                        ) : null}
                        {result.mode ? (
                            <div className="min-w-0">
                                <dt className="text-muted-foreground">{t('mode')}</dt>
                                <dd className="break-all font-medium">{result.mode}</dd>
                            </div>
                        ) : null}
                        {result.limit !== undefined ? (
                            <div>
                                <dt className="text-muted-foreground">{t('limit')}</dt>
                                <dd className="font-medium">{formatBalance(result.limit, result.unit)}</dd>
                            </div>
                        ) : null}
                        {result.used !== undefined ? (
                            <div>
                                <dt className="text-muted-foreground">{t('used')}</dt>
                                <dd className="font-medium">{formatBalance(result.used, result.unit)}</dd>
                            </div>
                        ) : null}
                    </dl>
                    <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                        <span>{t('status', { status: result.status_code })}</span>
                        <span>{t('duration', { duration: result.duration_ms })}</span>
                        <span>{t('usedKey', { key: result.key_id })}</span>
                        {result.is_valid !== undefined ? (
                            <span>{result.is_valid ? t('valid') : t('invalid')}</span>
                        ) : null}
                    </div>
                </div>
            ) : null}
        </section>
    );
}
