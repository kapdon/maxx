import { useState, useMemo, useEffect, useRef } from 'react';
import { ChevronDown, Search, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { useAvailableModels } from '@/hooks/queries';

type Provider = 'Claude' | 'Gemini' | 'OpenAI' | 'NVIDIA' | 'Antigravity' | 'Other';

interface Model {
  id: string;
  name: string;
  provider: string;
}

interface ModelInputProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  /** Filter to only show models from specific providers */
  providers?: Provider[];
}

// 简单的模糊匹配函数
function fuzzyMatch(text: string, pattern: string): boolean {
  const lowerText = text.toLowerCase();
  const lowerPattern = pattern.toLowerCase();

  // 先尝试普通包含匹配
  if (lowerText.includes(lowerPattern)) return true;

  // 模糊匹配：pattern 中的字符按顺序出现在 text 中
  let patternIdx = 0;
  for (let i = 0; i < lowerText.length && patternIdx < lowerPattern.length; i++) {
    if (lowerText[i] === lowerPattern[patternIdx]) {
      patternIdx++;
    }
  }
  return patternIdx === lowerPattern.length;
}

// 计算匹配分数（用于排序）
function matchScore(model: Model, pattern: string): number {
  const lowerPattern = pattern.toLowerCase();
  const lowerId = model.id.toLowerCase();
  const lowerName = model.name.toLowerCase();

  // 精确匹配得分最高
  if (lowerId === lowerPattern || lowerName === lowerPattern) return 100;

  // 前缀匹配次之
  if (lowerId.startsWith(lowerPattern) || lowerName.startsWith(lowerPattern)) return 80;

  // 包含匹配
  if (lowerId.includes(lowerPattern) || lowerName.includes(lowerPattern)) return 60;

  // 模糊匹配得分最低
  return 40;
}

function mergeModelOptions(models: Model[]): Model[] {
  const seen = new Set<string>();
  const merged: Model[] = [];

  for (const model of models) {
    const id = model.id.trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    merged.push({ ...model, id });
  }

  return merged;
}

function inferProvider(modelId: string): Model['provider'] {
  const id = modelId.toLowerCase();
  if (id.startsWith('claude-')) return 'Claude';
  if (id.startsWith('gemini-')) return 'Gemini';
  if (id.startsWith('gpt-') || /^o\d/.test(id) || id.includes('codex')) return 'OpenAI';
  if (id.includes('llama') || id.includes('mistral') || id.includes('qwen')) return 'NVIDIA';
  return 'Other';
}

export function ModelInput({
  value,
  onChange,
  placeholder,
  disabled = false,
  className,
  providers,
}: ModelInputProps) {
  const { t } = useTranslation();
  const actualPlaceholder = placeholder ?? t('modelInput.selectOrEnter');
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [focusedIndex, setFocusedIndex] = useState(-1);
  const focusedRef = useRef<HTMLButtonElement>(null);
  const { data: availableModelIds } = useAvailableModels();

  const availableModels = useMemo<Model[]>(() => {
    const models = (availableModelIds || []).map((id) => ({
      id,
      name: id,
      provider: inferProvider(id),
    }));

    if (!providers || providers.length === 0) return models;
    return models.filter((model) => providers.includes(model.provider as Provider));
  }, [availableModelIds, providers]);

  // Mapping choices are runtime/provider-driven. Static common models stay out of
  // the selectable list so an empty provider inventory produces no suggestions.
  const baseModels = useMemo(() => {
    return mergeModelOptions(availableModels);
  }, [availableModels]);

  // 过滤和排序模型（支持模糊匹配）
  const filteredModels = useMemo(() => {
    if (!search.trim()) return baseModels;

    return baseModels
      .filter(
        (model) =>
          fuzzyMatch(model.id, search) ||
          fuzzyMatch(model.name, search) ||
          fuzzyMatch(model.provider, search),
      )
      .sort((a, b) => matchScore(b, search) - matchScore(a, search));
  }, [search, baseModels]);

  // 重置 focusedIndex 当过滤结果变化时
  useEffect(() => {
    setFocusedIndex(-1);
  }, [filteredModels.length]);

  // 自动滚动到高亮项
  useEffect(() => {
    if (focusedIndex >= 0 && focusedRef.current) {
      focusedRef.current.scrollIntoView({ block: 'nearest' });
    }
  }, [focusedIndex]);

  // 按 provider 分组
  const groupedModels = useMemo(() => {
    return filteredModels.reduce(
      (acc, model) => {
        if (!acc[model.provider]) {
          acc[model.provider] = [];
        }
        acc[model.provider].push(model);
        return acc;
      },
      {} as Record<string, Model[]>,
    );
  }, [filteredModels]);

  const handleOpen = () => {
    if (!disabled) {
      setSearch(value); // 初始化搜索框为当前值
      setIsOpen(true);
    }
  };

  const handleSelect = (modelId: string) => {
    onChange(modelId);
    setIsOpen(false);
    setSearch('');
  };

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation();
    onChange('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      // 如果有高亮项，选择高亮项；否则使用搜索框内容
      if (focusedIndex >= 0 && focusedIndex < filteredModels.length) {
        handleSelect(filteredModels[focusedIndex].id);
      } else if (search.trim()) {
        handleSelect(search.trim());
      }
      return;
    }

    // Tab/Shift+Tab 切换高亮项
    if (e.key === 'Tab' && filteredModels.length > 0) {
      e.preventDefault();

      if (e.shiftKey) {
        // Shift+Tab: 上一个
        setFocusedIndex((prev) => (prev <= 0 ? filteredModels.length - 1 : prev - 1));
      } else {
        // Tab: 下一个
        setFocusedIndex((prev) => (prev >= filteredModels.length - 1 ? 0 : prev + 1));
      }
    }

    // 上下箭头也可以切换
    if (e.key === 'ArrowDown' && filteredModels.length > 0) {
      e.preventDefault();
      setFocusedIndex((prev) => (prev >= filteredModels.length - 1 ? 0 : prev + 1));
    }
    if (e.key === 'ArrowUp' && filteredModels.length > 0) {
      e.preventDefault();
      setFocusedIndex((prev) => (prev <= 0 ? filteredModels.length - 1 : prev - 1));
    }
  };

  return (
    <>
      {/* 触发按钮 */}
      <button
        type="button"
        onClick={handleOpen}
        disabled={disabled}
        className={cn(
          'w-full flex items-center justify-between gap-2 px-3 py-2',
          'bg-card border border-border rounded-md',
          'text-sm text-left',
          'hover:border-border-hover focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          'transition-colors',
          className,
        )}
      >
        <span
          className={cn('flex-1 truncate', value ? 'text-foreground' : 'text-muted-foreground')}
        >
          {value || actualPlaceholder}
        </span>
        <div className="flex items-center gap-1">
          {value && !disabled && (
            <span
              role="button"
              tabIndex={0}
              onClick={handleClear}
              onKeyDown={(e) => e.key === 'Enter' && handleClear(e as unknown as React.MouseEvent)}
              className="p-0.5 hover:bg-accent rounded text-muted-foreground hover:text-foreground"
            >
              <X size={14} />
            </span>
          )}
          <ChevronDown size={16} className="text-muted-foreground" />
        </div>
      </button>

      {/* Dialog */}
      <Dialog open={isOpen} onOpenChange={setIsOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('modelInput.selectModel')}</DialogTitle>
          </DialogHeader>

          {/* 搜索框 */}
          <div className="relative">
            <Search
              size={16}
              className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t('modelInput.searchOrEnter')}
              className="pl-9"
              autoFocus
            />
          </div>

          {/* 模型列表 */}
          <div className="h-80 overflow-y-auto -mx-6 px-6">
            {Object.keys(groupedModels).length > 0 ? (
              <div className="space-y-4">
                {Object.entries(groupedModels).map(([provider, models]) => (
                  <div key={provider}>
                    <div className="text-xs font-semibold text-muted-foreground mb-2 sticky top-0 bg-card py-1">
                      {provider}
                    </div>
                    <div className="space-y-1">
                      {models.map((model) => {
                        const modelIndex = filteredModels.findIndex((m) => m.id === model.id);
                        const isFocused = modelIndex === focusedIndex;
                        return (
                          <button
                            key={model.id}
                            ref={isFocused ? focusedRef : null}
                            type="button"
                            onClick={() => handleSelect(model.id)}
                            className={cn(
                              'w-full px-3 py-2 text-left text-sm rounded-md',
                              'hover:bg-accent transition-colors',
                              'flex flex-col gap-0.5',
                              value === model.id && 'bg-primary/10 ring-1 ring-primary/20',
                              isFocused && 'bg-accent ring-1 ring-primary/40',
                            )}
                          >
                            <span className="text-foreground font-medium">{model.name}</span>
                            <span className="text-xs text-muted-foreground font-mono">
                              {model.id}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                ))}
              </div>
            ) : search.trim() ? (
              <div className="py-4">
                <p className="text-sm text-text-secondary mb-3">
                  {t('modelInput.noMatchingModels')}
                </p>
                <button
                  type="button"
                  onClick={() => handleSelect(search.trim())}
                  className="w-full px-3 py-2 text-left text-sm bg-muted rounded-md hover:bg-accent transition-colors"
                >
                  <span className="text-text-primary">
                    {t('modelInput.useCustom')}
                    <span className="font-mono font-medium">{search.trim()}</span>
                  </span>
                </button>
              </div>
            ) : (
              <div className="h-full flex items-center justify-center text-sm text-text-muted">
                {t('modelInput.noModelsAvailable')}
              </div>
            )}
          </div>

          {/* 提示 */}
          <div className="text-xs text-text-muted pt-2 border-t border-border">
            {t('modelInput.pressToUse', { key: 'Enter' })}
            <kbd className="px-1.5 py-0.5 bg-surface-secondary rounded text-text-secondary font-mono">
              Enter
            </kbd>
            {t('modelInput.toUseCustomModel')}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
