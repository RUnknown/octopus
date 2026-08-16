'use client';

import { useEffect, useMemo, useState } from 'react';
import { ImageIcon, LoaderCircle, Sparkles } from 'lucide-react';
import { useTranslations } from 'next-intl';

import {
    type Channel,
    type ImageGenerationTestImage,
    useTestImageChannel,
} from '@/api/endpoints/channel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

const IMAGE_PROMPT_STORAGE_KEY = 'octopus:image-test-prompt';

function splitModels(channel: Channel) {
    return Array.from(new Set(
        `${channel.model},${channel.custom_model}`
            .split(',')
            .map((model) => model.trim())
            .filter(Boolean),
    ));
}

function imageSource(image: ImageGenerationTestImage, outputFormat: string) {
    if (image.url) return image.url;
    if (!image.b64_json) return '';
    const format = outputFormat === 'jpeg' ? 'jpeg' : outputFormat === 'webp' ? 'webp' : 'png';
    return `data:image/${format};base64,${image.b64_json}`;
}

export function ImageTestPanel({ channel }: { channel: Channel }) {
    const t = useTranslations('channel.detail.imageTest');
    const models = useMemo(() => splitModels(channel), [channel]);
    const enabledKeys = useMemo(() => channel.keys.filter((key) => key.enabled && key.channel_key), [channel.keys]);
    const [model, setModel] = useState(models[0] ?? '');
    const [keyID, setKeyID] = useState(enabledKeys[0]?.id ? String(enabledKeys[0].id) : '');
    const [prompt, setPrompt] = useState('');
    const [size, setSize] = useState('1024x1024');
    const [quality, setQuality] = useState('auto');
    const [background, setBackground] = useState('auto');
    const [outputFormat, setOutputFormat] = useState('png');
    const testImage = useTestImageChannel();

    useEffect(() => {
        const timer = window.setTimeout(() => {
            try {
                setPrompt(window.localStorage.getItem(IMAGE_PROMPT_STORAGE_KEY) ?? '');
            } catch {
                // Private browsing or storage policy may disable localStorage.
            }
        }, 0);
        return () => window.clearTimeout(timer);
    }, []);

    const updatePrompt = (value: string) => {
        setPrompt(value);
        try {
            window.localStorage.setItem(IMAGE_PROMPT_STORAGE_KEY, value);
        } catch {
            // Keep the in-memory prompt usable when persistence is unavailable.
        }
    };

    const runTest = () => {
        testImage.mutate({
            channel_id: channel.id,
            key_id: keyID ? Number(keyID) : undefined,
            model,
            prompt,
            size,
            quality,
            background,
            output_format: outputFormat,
        });
    };

    const images = testImage.data?.body?.data ?? [];

    return (
        <section className="mt-4 max-h-[58vh] space-y-4 overflow-y-auto rounded-2xl border bg-card p-3 sm:p-4">
            <div>
                <h4 className="flex items-center gap-2 text-sm font-semibold">
                    <Sparkles className="size-4 text-primary" />
                    {t('title')}
                </h4>
                <p className="mt-1 text-xs text-muted-foreground">{t('description')}</p>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('model')}</span>
                    <Select value={model} onValueChange={setModel}>
                        <SelectTrigger className="w-full rounded-xl">
                            <SelectValue placeholder={t('modelPlaceholder')} />
                        </SelectTrigger>
                        <SelectContent>
                            {models.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
                        </SelectContent>
                    </Select>
                </label>

                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('key')}</span>
                    <Select value={keyID} onValueChange={setKeyID}>
                        <SelectTrigger className="w-full rounded-xl">
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
            </div>

            <label className="block space-y-1 text-xs text-muted-foreground">
                <span>{t('prompt')}</span>
                <textarea
                    value={prompt}
                    onChange={(event) => updatePrompt(event.target.value)}
                    placeholder={t('promptPlaceholder')}
                    rows={4}
                    className="border-input bg-transparent placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 w-full resize-y rounded-xl border px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-[3px]"
                />
            </label>

            <div className="grid gap-3 sm:grid-cols-2">
                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('size')}</span>
                    <Input value={size} onChange={(event) => setSize(event.target.value)} className="rounded-xl" />
                </label>
                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('quality')}</span>
                    <Input value={quality} onChange={(event) => setQuality(event.target.value)} className="rounded-xl" />
                </label>
                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('background')}</span>
                    <Input value={background} onChange={(event) => setBackground(event.target.value)} className="rounded-xl" />
                </label>
                <label className="space-y-1 text-xs text-muted-foreground">
                    <span>{t('outputFormat')}</span>
                    <Select value={outputFormat} onValueChange={setOutputFormat}>
                        <SelectTrigger className="w-full rounded-xl">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="png">PNG</SelectItem>
                            <SelectItem value="jpeg">JPEG</SelectItem>
                            <SelectItem value="webp">WebP</SelectItem>
                        </SelectContent>
                    </Select>
                </label>
            </div>

            <Button
                type="button"
                onClick={runTest}
                disabled={testImage.isPending || !model || !prompt.trim() || !keyID}
                className="w-full rounded-xl"
            >
                {testImage.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <ImageIcon className="size-4" />}
                {testImage.isPending ? t('generating') : t('generate')}
            </Button>

            {testImage.error ? (
                <p className="break-all rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
                    {testImage.error.message}
                </p>
            ) : null}

            {testImage.data ? (
                <div className="space-y-3">
                    <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span>{t('status', { status: testImage.data.status_code })}</span>
                        <span>{t('duration', { duration: testImage.data.duration_ms })}</span>
                        <span>{t('usedKey', { key: testImage.data.key_id })}</span>
                    </div>
                    {images.length > 0 ? (
                        <div className="grid gap-3 sm:grid-cols-2">
                            {images.map((image, index) => {
                                const source = imageSource(image, outputFormat);
                                return (
                                    <figure key={`${index}-${source.slice(0, 32)}`} className="overflow-hidden rounded-2xl border bg-background">
                                        {source ? (
                                            <div
                                                role="img"
                                                aria-label={image.revised_prompt || t('imageAlt', { index: index + 1 })}
                                                className="aspect-square w-full bg-contain bg-center bg-no-repeat"
                                                style={{ backgroundImage: `url(${JSON.stringify(source)})` }}
                                            />
                                        ) : (
                                            <div className="flex aspect-square items-center justify-center text-xs text-muted-foreground">
                                                {t('missingImage')}
                                            </div>
                                        )}
                                        {image.revised_prompt ? (
                                            <figcaption className="border-t p-2 text-xs text-muted-foreground">{image.revised_prompt}</figcaption>
                                        ) : null}
                                    </figure>
                                );
                            })}
                        </div>
                    ) : (
                        <p className="text-xs text-muted-foreground">{t('noImages')}</p>
                    )}
                </div>
            ) : null}
        </section>
    );
}
