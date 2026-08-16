'use client';

import { useTranslations } from 'next-intl';
import { Sparkles, Hash, Clock, SlidersHorizontal, Scale } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField } from './shared';

// min/max 与后端 model.Setting.Validate() 的边界保持一致，前端先行约束整数范围。
const AUTO_STRATEGY_FIELDS: { key: string; labelKey: string; min: number; max?: number }[] = [
    { key: SettingKey.AutoStrategyMinSamples, labelKey: 'minSamples', min: 1 },
    { key: SettingKey.AutoStrategyTimeWindow, labelKey: 'timeWindow', min: 1 },
    { key: SettingKey.AutoStrategySampleThreshold, labelKey: 'sampleThreshold', min: 1 },
    { key: SettingKey.AutoStrategyLatencyWeight, labelKey: 'latencyWeight', min: 0, max: 100 },
];

function NumberFieldRow({ settingKey, label, placeholder, tooltip, icon, min, max }: {
    settingKey: string;
    label: string;
    placeholder: string;
    tooltip?: React.ReactNode;
    icon?: React.ComponentType<{ className?: string }>;
    min?: number;
    max?: number;
}) {
    const field = useSettingField(settingKey);
    return (
        <SettingRow icon={icon} label={label} tooltip={tooltip}>
            <Input
                type="number"
                step={1}
                min={min}
                max={max}
                value={field.value}
                onChange={(e) => field.setValue(e.target.value)}
                onBlur={field.save}
                placeholder={placeholder}
                className="w-48 rounded-xl"
            />
        </SettingRow>
    );
}

export function SettingAutoStrategy() {
    const t = useTranslations('setting');

    return (
        <SettingCard icon={Sparkles} title={t('autoStrategy.title')} tooltip={t('autoStrategy.description')}>
            <SettingSection title={t('autoStrategy.section')} tooltip={t('autoStrategy.hint')} />
            <NumberFieldRow
                settingKey={SettingKey.AutoStrategyMinSamples}
                label={t('autoStrategy.minSamples.label')}
                placeholder={t('autoStrategy.minSamples.placeholder')}
                tooltip={t('autoStrategy.minSamples.description')}
                icon={Hash}
                min={1}
            />
            <NumberFieldRow
                settingKey={SettingKey.AutoStrategyTimeWindow}
                label={t('autoStrategy.timeWindow.label')}
                placeholder={t('autoStrategy.timeWindow.placeholder')}
                tooltip={t('autoStrategy.timeWindow.description')}
                icon={Clock}
                min={1}
            />
            <NumberFieldRow
                settingKey={SettingKey.AutoStrategySampleThreshold}
                label={t('autoStrategy.sampleThreshold.label')}
                placeholder={t('autoStrategy.sampleThreshold.placeholder')}
                tooltip={t('autoStrategy.sampleThreshold.description')}
                icon={SlidersHorizontal}
                min={1}
            />
            <NumberFieldRow
                settingKey={SettingKey.AutoStrategyLatencyWeight}
                label={t('autoStrategy.latencyWeight.label')}
                placeholder={t('autoStrategy.latencyWeight.placeholder')}
                tooltip={t('autoStrategy.latencyWeight.description')}
                icon={Scale}
                min={0}
                max={100}
            />
        </SettingCard>
    );
}
