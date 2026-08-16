'use client';

import { useTranslations } from 'next-intl';
import { Sparkles, Hash, Clock, SlidersHorizontal, Scale } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField } from './shared';

// min/max 与后端 model.Setting.Validate() 的边界保持一致，前端先行约束整数范围。
const AUTO_STRATEGY_FIELDS: { key: string; labelKey: string; icon: LucideIcon; min: number; max?: number }[] = [
    { key: SettingKey.AutoStrategyMinSamples, labelKey: 'minSamples', icon: Hash, min: 1 },
    { key: SettingKey.AutoStrategyTimeWindow, labelKey: 'timeWindow', icon: Clock, min: 1 },
    { key: SettingKey.AutoStrategySampleThreshold, labelKey: 'sampleThreshold', icon: SlidersHorizontal, min: 1 },
    { key: SettingKey.AutoStrategyLatencyWeight, labelKey: 'latencyWeight', icon: Scale, min: 0, max: 100 },
];

function NumberFieldRow({ settingKey, label, placeholder, tooltip, icon, min, max }: {
    settingKey: string;
    label: string;
    placeholder: string;
    tooltip?: React.ReactNode;
    icon?: LucideIcon;
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
            {AUTO_STRATEGY_FIELDS.map(({ key, labelKey, icon, min, max }) => (
                <NumberFieldRow
                    key={key}
                    settingKey={key}
                    label={t(`autoStrategy.${labelKey}.label`)}
                    placeholder={t(`autoStrategy.${labelKey}.placeholder`)}
                    tooltip={t(`autoStrategy.${labelKey}.description`)}
                    icon={icon}
                    min={min}
                    max={max}
                />
            ))}
        </SettingCard>
    );
}
