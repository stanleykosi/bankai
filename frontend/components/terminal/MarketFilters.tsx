import React from "react";
import { XCircle } from "lucide-react";

export type SortOption = "all" | "volume" | "liquidity" | "created";

export interface FilterOption {
  value: string;
  label: string;
  count: number;
}

interface MarketFiltersProps {
  categories: FilterOption[];
  tags: FilterOption[];
  selectedCategory?: string;
  selectedTag?: string;
  sort: SortOption;
  onChange: (filters: { category?: string; tag?: string; sort?: SortOption }) => void;
  onReset: () => void;
}

const sortOptions: { value: SortOption; label: string; description: string }[] = [
  { value: "all", label: "All Active Markets", description: "Default: newest first" },
  { value: "volume", label: "24h Volume", description: "Most traded in last 24h" },
  { value: "liquidity", label: "Deep Liquidity", description: "Highest capital depth" },
  { value: "created", label: "Newest Listings", description: "Latest markets first" },
];

export const MarketFilters: React.FC<MarketFiltersProps> = ({
  categories,
  tags,
  selectedCategory,
  selectedTag,
  sort,
  onChange,
  onReset,
}) => {
  return (
    <div className="rounded-lg border border-border bg-card/60 p-4 shadow-lg shadow-black/20 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-semibold text-primary tracking-wide uppercase">Market Controls</p>
          <p className="text-xs text-muted-foreground">Tune the radar by category, tag, and momentum.</p>
        </div>
        <button
          onClick={onReset}
          className="inline-flex items-center gap-1 rounded-full border border-border px-3 py-1 text-xs font-semibold text-muted-foreground hover:text-foreground hover:border-foreground transition-colors"
        >
          <XCircle className="h-3.5 w-3.5" />
          Reset
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-xs uppercase tracking-wide text-muted-foreground/80">Category</label>
          <select
            className="rounded-md border border-border bg-background/70 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
            value={selectedCategory || ""}
            onChange={(e) => onChange({ category: e.target.value || undefined })}
          >
            <option value="">All categories</option>
            {categories.map((cat) => (
              <option key={cat.value} value={cat.value}>
                {cat.label} ({cat.count})
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <label className="text-xs uppercase tracking-wide text-muted-foreground/80">Tag</label>
          <select
            className="rounded-md border border-border bg-background/70 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
            value={selectedTag || ""}
            onChange={(e) => onChange({ tag: e.target.value || undefined })}
          >
            <option value="">All tags</option>
            {tags.map((tag) => (
              <option key={tag.value} value={tag.value}>
                {tag.label} ({tag.count})
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <label className="text-xs uppercase tracking-wide text-muted-foreground/80">Sort</label>
          <select
            className="rounded-md border border-border bg-background/70 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
            value={sort}
            onChange={(e) => onChange({ sort: e.target.value as SortOption })}
          >
            {sortOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label} — {option.description}
              </option>
            ))}
          </select>
        </div>
      </div>

      {(selectedCategory || selectedTag) && (
        <div className="flex flex-wrap gap-2 text-xs">
          {selectedCategory && (
            <span className="inline-flex items-center gap-1 rounded-full bg-primary/10 text-primary px-2 py-1">
              Category: {selectedCategory}
              <button onClick={() => onChange({ category: undefined })}>
                <XCircle className="h-3 w-3" />
              </button>
            </span>
          )}
          {selectedTag && (
            <span className="inline-flex items-center gap-1 rounded-full bg-primary/10 text-primary px-2 py-1">
              Tag: {selectedTag}
              <button onClick={() => onChange({ tag: undefined })}>
                <XCircle className="h-3 w-3" />
              </button>
            </span>
          )}
        </div>
      )}
    </div>
  );
};

